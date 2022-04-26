package memory

import (
	"fmt"
	"math"
	"os"

	linuxproc "github.com/c9s/goprocinfo/linux"
	"github.com/containerd/cgroups"
	cgroupstatsv1 "github.com/containerd/cgroups/stats/v1"
	"github.com/dustin/go-humanize"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// PoC: assumes kubepods memory controller mounted at /sys/fs/cgroups/memory/kubepods
	// Should be based on cgroup driver (systemdDbus: kubepods.slice, cgroups: kubepods) and from kubelet config
	cgroupRoot = "/sys/fs/cgroup"
	kubepodsMemoryCgroupName = "kubepods"
	systemSliceMemoryCgroupName = "system.slice"
)


var (
	// defaultMinThresholdPercent is the default minimum percentage of OS memory available, that triggeres an update & restart of the kubelet
	// 0.9 means that it should reconcile if less than 90% of OS memory is available
	defaultMinThresholdPercent     = "0.9"

	metricCurrentReservedMemoryBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_reserved_memory_bytes",
		Help: "The kubelet reserved memory in bytes as configured in the kubelet configuration file",
	})

	metricCurrentReservedMemoryPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_reserved_memory_percent",
		Help: "The kubelet reserved memory in percent calculated as (current reserved memory / MemTotal)",
	})

	metricTargetReservedMemoryBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_target_reserved_memory_bytes",
		Help: "The target kubelet reserved memory calculated as MemTotal - MemAvailable - memory working set kubepods cgroup",
	})

	metricTargetReservedMemoryPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_target_reserved_memory_percent",
		Help: "The target kubelet reserved memory in percent calculated as (target reserved memory / MemTotal)",
	})

	metricKubepodsWorkingSetMemory = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_kubepods_memory_working_set_bytes",
		Help: "The working set memory of the kubepods cgroup in bytes",
	})

	metricKubepodsWorkingSetMemoryPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_kubepods_memory_working_set_percent",
		Help: "The working set memory of the kubepods cgroup in percent of the total memory",
	})

	metricSystemSliceWorkingSetMemory = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_system_slice_memory_working_set_bytes",
		Help: "The working set memory of the system slice cgroup in bytes",
	})

	metricSystemSliceWorkingSetMemoryPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_system_slice_memory_working_set_percent",
		Help: "The working set memory of the system slice cgroup in percent of the total memory",
	})

	metricMemTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_memory_MemTotal",
		Help: "The MemTotal from /proc/meminfo",
	})

	metricMemAvailable = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_memory_MemAvailable",
		Help: "The MemAvailable from /proc/meminfo",
	})

	metricMemAvailablePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_memory_MemAvailable_percent",
		Help: "The MemAvailable in percent of the total memory",
	})

	metricMemUsed = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_memory_used",
		Help: "The not reclaimable memory calculated from /proc/meminfo MemTotal - MemAvailable. (unlike measurement from root memory cgroup)",
	})

	metricMemUsedPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_memory_used_percent",
		Help: "The not reclaimable memory in percent calculated from /proc/meminfo MemTotal - MemAvailable. (unlike measurement from root memory cgroup)",
	})
)

// RecommendReservedMemory recommends a memory reservation for non-pod processes.
// The recommendation can be split across kube- and system-reserved and hard-eviction.
func RecommendReservedMemory(
	log *logrus.Logger,
	memorySafetyMarginAbsolute resource.Quantity) error {

	memTotal, memAvailable, err := ParseProcMemInfo()
	if err != nil {
		log.Fatalf("fatal error during reconciliation: %v", err)
	}

	kubepodsWorkingSetBytes, err := getMemoryWorkingSet(kubepodsMemoryCgroupName)
	if err != nil {
		return err
	}

	systemSliceWorkingSetBytes, err := getMemoryWorkingSet(systemSliceMemoryCgroupName)
	if err != nil {
		return err
	}

	kubepodsLimitInBytes, err := getMemoryLimitInBytes(kubepodsMemoryCgroupName)
	if err != nil {
		return err
	}

	// Calculate the reserved memory based on the memory limit on the kubepods cgroup
	// Memory limit on the kubepods cgroup = Capacity - kube-reserved - system-reserved - hard eviction
	// To know how the reservation is distributed amongst (kube-reserved,system-reserved,hard eviction),
	// we would have to read the kubelet configuration
	currentReservedMemory := memTotal
	currentReservedMemory.Sub(kubepodsLimitInBytes)

	// Calculation: target reserved memory =
	// MemTotal
	// - MemAvailable
	// - working_set_bytes of kubepods cgroup
	// + safety margin
	targetReservedMemory := memTotal
	targetReservedMemory.Sub(memAvailable)
	currentlyUsedMemory := targetReservedMemory
	targetReservedMemory.Sub(kubepodsWorkingSetBytes)
	targetReservedMemory.Add(memorySafetyMarginAbsolute)

	log.Debugf("Available memory from /proc/mem: %q (%d percent)", memAvailable.String(), int64(math.Round(float64(memAvailable.Value())/float64(memTotal.Value())*100)))
	log.Debugf("Used memory: %q (%d percent)", currentlyUsedMemory.String(), int64(math.Round(float64(currentlyUsedMemory.Value())/float64(memTotal.Value())*100)))
	log.Debugf("Kubepods working set memory: %q (%d percent)", kubepodsWorkingSetBytes.String(), int64(math.Round(float64(kubepodsWorkingSetBytes.Value())/float64(memTotal.Value())*100)))
	log.Debugf("System.slice working set memory: %q (%d percent)", systemSliceWorkingSetBytes.String(), int64(math.Round(float64(systemSliceWorkingSetBytes.Value())/float64(memTotal.Value())*100)))

	// record prometheus metrics
	metricMemAvailable.Set(float64(memAvailable.Value()))
	metricMemAvailablePercent.Set(math.Round(float64(memAvailable.Value()) / float64(memTotal.Value()) * 100))
	metricMemUsed.Set(float64(currentlyUsedMemory.Value()))
	metricMemUsedPercent.Set(math.Round(float64(currentlyUsedMemory.Value()) / float64(memTotal.Value()) * 100))
	metricKubepodsWorkingSetMemory.Set(float64(kubepodsWorkingSetBytes.Value()))
	metricKubepodsWorkingSetMemoryPercent.Set(math.Round(float64(kubepodsWorkingSetBytes.Value()) / float64(memTotal.Value()) * 100))
	metricSystemSliceWorkingSetMemory.Set(float64(systemSliceWorkingSetBytes.Value()))
	metricSystemSliceWorkingSetMemoryPercent.Set(math.Round(float64(systemSliceWorkingSetBytes.Value()) / float64(memTotal.Value()) * 100))
	metricCurrentReservedMemoryBytes.Set(float64(currentReservedMemory.Value()))
	metricCurrentReservedMemoryPercent.Set(math.Round(float64(currentReservedMemory.Value()) / float64(memTotal.Value()) * 100))

	// in case the target reserved memory is negative, that means that the kubepods cgroup memory working set
	// was larger than the OS thinks is even used overall --> cgroupv1 accounting is most likely off
	// in this case, we rather choose to not report a target reserved memory via metrics.
	// If desired, the systemSliceWorkingSetBytes can be used knowing that this will most
	// likely over-reserve memory
	if targetReservedMemory.Value() < 0 {
		log.Infof("No memory recommendation can be provided. Memory accounting seems to be off. You can use the working set of system.slice instead, though this will most likely over-reserve memory.")
		metricTargetReservedMemoryBytes.Set(-1)
		metricTargetReservedMemoryPercent.Set(0)
		return nil
	}

	log.Debugf("Recommended memory reservation: %q (%s, %d percent). Currenlty reserved (kube-reserved + system-reserved): %q (%d percent)",
		humanize.IBytes(uint64(targetReservedMemory.Value())),
		targetReservedMemory.String(),
		int64(math.Round(float64(targetReservedMemory.Value())/float64(memTotal.Value())*100)),
		currentReservedMemory.String(),
		int64(math.Round(float64(currentReservedMemory.Value())/float64(memTotal.Value())*100)),
		)

	// record prometheus metrics
	metricTargetReservedMemoryBytes.Set(float64(targetReservedMemory.Value()))
	metricTargetReservedMemoryPercent.Set(math.Round(float64(targetReservedMemory.Value()) / float64(memTotal.Value()) * 100))

	logRecommendation(
		humanize.IBytes(uint64(memAvailable.Value())),
		int64(math.Round(float64(memAvailable.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(currentlyUsedMemory.Value())),
		int64(math.Round(float64(currentlyUsedMemory.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(kubepodsWorkingSetBytes.Value())),
		int64(math.Round(float64(kubepodsWorkingSetBytes.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(systemSliceWorkingSetBytes.Value())),
		int64(math.Round(float64(systemSliceWorkingSetBytes.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(currentReservedMemory.Value())),
		int64(math.Round(float64(currentReservedMemory.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(targetReservedMemory.Value())),
		targetReservedMemory.String(),
		int64(math.Round(float64(targetReservedMemory.Value())/float64(memTotal.Value())*100)),
		)

	return nil
}

func logRecommendation(
	availableMemoryProcMem string,
	availableMemoryProcMemPercentTotal int64,
	usedMemoryProcMem string,
	usedMemoryProcMemPercentTotal int64,
	kubepodsWorkingSet string,
	kubepodsWorkingSetPercentTotal int64,
	systemSliceWorkingSet string,
	systemSliceWorkingSetPercentTotal int64,
	currentReservedMemory string,
	currentReservedMemoryPercentTotal int64,
	targetReservedMemory string,
	targetReservedMemoryPrecise string,
	targetReservedMemoryPercentTotal int64) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Memory Metric", "Value"})

	t.AppendRows([]table.Row{
		{"Available (/proc/mem)", fmt.Sprintf("%s (%d%%)", availableMemoryProcMem, availableMemoryProcMemPercentTotal)},
		{"Used (Capacity - Available)", fmt.Sprintf("%s (%d%%)", usedMemoryProcMem, usedMemoryProcMemPercentTotal)},
		{"Kubepods working set", fmt.Sprintf("%s (%d%%)", kubepodsWorkingSet, kubepodsWorkingSetPercentTotal)},
		{"System.slice working set", fmt.Sprintf("%s (%d%%)", systemSliceWorkingSet, systemSliceWorkingSetPercentTotal)},
		{"Current reservation (kube+system reserved)", fmt.Sprintf("%s (%d%%)", currentReservedMemory, currentReservedMemoryPercentTotal)},
	})

	t.AppendSeparator()
	t.AppendRow(table.Row{"RECOMMENDATION", fmt.Sprintf("%s (%s, %d%%)", targetReservedMemory, targetReservedMemoryPrecise, targetReservedMemoryPercentTotal)})
	t.Render()
}

// getMemoryWorkingSet reads the given unit's memory cgroup and calculates
// the working set bytes
func getMemoryWorkingSet(unit string) (resource.Quantity, error) {
	memoryController := cgroups.NewMemory(cgroupRoot)

	stats := &cgroupstatsv1.Metrics{}
	if err := memoryController.Stat(unit, stats); err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to read memory stats for kubepods cgroup: %v", err)
	}

	memoryWorkingSetBytes := stats.Memory.Usage.Usage - stats.Memory.TotalInactiveFile
	return resource.ParseQuantity(fmt.Sprintf("%d", memoryWorkingSetBytes))
}

// getMemoryWorkingSet reads the given unit's memory cgroup to return the memory limit in bytes
func getMemoryLimitInBytes(unit string) (resource.Quantity, error) {
	memoryController := cgroups.NewMemory(cgroupRoot)

	stats := &cgroupstatsv1.Metrics{}
	if err := memoryController.Stat(unit, stats); err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to read memory stats for kubepods cgroup: %v", err)
	}

	out := fmt.Sprintf("%d", stats.Memory.Usage.Limit)
	fmt.Sprintf("memory limit for unit %s is %s", unit, out)
	return resource.ParseQuantity(out)
}

// ParseProcMemInfo parses /proc/meminfo and returns MemTotal, MemAvailable or an error
func ParseProcMemInfo() (resource.Quantity, resource.Quantity, error) {
	// meminfo values are given in kiB/kibibytes (1024 bytes) (even though given as "kb")
	meminfo, err := linuxproc.ReadMemInfo("/proc/meminfo")
	if err != nil {
		return resource.Quantity{}, resource.Quantity{}, fmt.Errorf("failed to read file: /proc/meminfo: %v", err)
	}

	// for sake of simplicity, expect that "MemAvailable" field is available
	// alternatively, the available memory (before swapping) could also be calculated from other values in /proc/meminfo.
	// see here: https://unix.stackexchange.com/questions/261247/how-can-i-get-the-amount-of-available-memory-portably-across-distributions
	if meminfo.MemAvailable == 0 {
		return resource.Quantity{}, resource.Quantity{}, fmt.Errorf("MemAvailable field in /proc/meminfo is not set. Please make sure that your Linux kernel includes this commit: https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/commit/?id=34e431b0a")
	}

	memAvailable, err := resource.ParseQuantity(fmt.Sprintf("%dKi", meminfo.MemAvailable))
	if err != nil {
		return resource.Quantity{}, resource.Quantity{}, fmt.Errorf("failed to parse MemAvailable field in /proc/meminf (%q) as resource quantity: %v", meminfo.MemAvailable, err)
	}

	memTotal, err := resource.ParseQuantity(fmt.Sprintf("%dKi", meminfo.MemTotal))
	if err != nil {
		return resource.Quantity{}, resource.Quantity{}, fmt.Errorf("failed to parse MemTotal field in /proc/meminf (%q) as resource quantity: %v", meminfo.MemTotal, err)
	}

	return memTotal, memAvailable, nil
}

