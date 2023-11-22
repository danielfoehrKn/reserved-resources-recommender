package memory

import (
	"fmt"
	"math"
	"os"

	linuxproc "github.com/c9s/goprocinfo/linux"
	"github.com/containerd/cgroups"
	cgroupstatsv1 "github.com/containerd/cgroups/stats/v1"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/memory/util"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/types"
	"github.com/dustin/go-humanize"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
)


// TODO: make work with cgroupsv2 where the location of the memory.stat file changed from  (unified controller hierarchy)
// cat /sys/fs/cgroup/memory/kubepods/memory.stat  -> cat /sys/fs/cgroup/kubepods/memory.stat
var (
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

	metricTargetReservedMemoryBytesMachineType = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_target_reserved_memory_bytes_machine_type",
		Help: "The target kubelet reserved memory calculated based on the machine type",
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

	metricContainerdServiceWorkingSetMemory = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_containerd_service_memory_working_set_bytes",
		Help: "The working set memory of the containerd cgroup in bytes",
	})

	metricContainerdServiceWorkingSetMemoryPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_containerd_service_memory_working_set_percent",
		Help: "The working set memory of the containerd cgroup in percent of the total memory",
	})

	metricDockerServiceWorkingSetMemory = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_docker_service_memory_working_set_bytes",
		Help: "The working set memory of the docker cgroup in bytes",
	})

	metricDockerServiceWorkingSetMemoryPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_docker_service_memory_working_set_percent",
		Help: "The working set memory of the docker cgroup in percent of the total memory",
	})

	metricKubeletServiceWorkingSetMemory = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_kubelet_service_memory_working_set_bytes",
		Help: "The working set memory of the kubelet cgroup in bytes",
	})

	metricKubeletServiceWorkingSetMemoryPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_kubelet_service_memory_working_set_percent",
		Help: "The working set memory of the kubelet cgroup in percent of the total memory",
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
func RecommendReservedMemory(log *logrus.Logger, minimumReservedMemory, memorySafetyMarginAbsolute resource.Quantity, cgroupRoot string, containerdMemoryCgroupName string, kubeletMemoryCgroupName string) (resource.Quantity, error) {
	memTotal, memAvailable, err := ParseProcMemInfo()
	if err != nil {
		log.Fatalf("fatal error during reconciliation: %v", err)
	}

	kubepodsWorkingSetBytes, err := getMemoryWorkingSet(cgroupRoot, types.DefaultkubepodsCgroupName)
	if err != nil {
		return resource.Quantity{}, err
	}

	systemSliceWorkingSetBytes, err := getMemoryWorkingSet(cgroupRoot, types.SystemSliceCgroupName)
	if err != nil {
		return resource.Quantity{}, err
	}

	containerdSliceWorkingSetBytes, dockerSliceWorkingSetBytes, err := getContainerRuntimeWorkingSetBytes(cgroupRoot, containerdMemoryCgroupName)
	if err != nil {
		return resource.Quantity{}, err
	}

	kubeletSliceWorkingSetBytes, err := getMemoryWorkingSet(cgroupRoot, kubeletMemoryCgroupName)
	if err != nil {
		return resource.Quantity{}, err
	}

	kubepodsLimitInBytes, err := getMemoryLimitInBytes(cgroupRoot, types.DefaultkubepodsCgroupName)
	if err != nil {
		return resource.Quantity{}, err
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
	metricContainerdServiceWorkingSetMemory.Set(float64(containerdSliceWorkingSetBytes.Value()))
	metricContainerdServiceWorkingSetMemoryPercent.Set(math.Round(float64(containerdSliceWorkingSetBytes.Value()) / float64(memTotal.Value()) * 100))
	metricDockerServiceWorkingSetMemory.Set(float64(dockerSliceWorkingSetBytes.Value()))
	metricDockerServiceWorkingSetMemoryPercent.Set(math.Round(float64(dockerSliceWorkingSetBytes.Value()) / float64(memTotal.Value()) * 100))
	metricKubeletServiceWorkingSetMemory.Set(float64(kubeletSliceWorkingSetBytes.Value()))
	metricKubeletServiceWorkingSetMemoryPercent.Set(math.Round(float64(kubeletSliceWorkingSetBytes.Value()) / float64(memTotal.Value()) * 100))
	metricCurrentReservedMemoryBytes.Set(float64(currentReservedMemory.Value()))
	metricCurrentReservedMemoryPercent.Set(math.Round(float64(currentReservedMemory.Value()) / float64(memTotal.Value()) * 100))

	// in case the target reserved memory is negative, that means that the kubepods cgroup memory working set
	// was larger than the OS thinks is even used overall --> cgroupv1 accounting is most likely off
	// in this case, we rather choose to not report a target reserved memory via metrics.
	// If desired, the systemSliceWorkingSetBytes can be used as recommended reservation knowing that this will most
	// likely over-reserve memory
	if targetReservedMemory.Value() < 0 {
		metricTargetReservedMemoryBytes.Set(-1)
		metricTargetReservedMemoryPercent.Set(0)
		return resource.Quantity{}, fmt.Errorf("no memory recommendation can be provided. Memory accounting seems to be off. You can use the working set of system.slice instead, though this will most likely over-reserve memory.")
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

	targetReservedMachineType, err := util.CalculateReservationBasedOnCapacity(memTotal)
	if err != nil {
		return resource.Quantity{}, err
	}
	metricTargetReservedMemoryBytesMachineType.Set(float64(targetReservedMachineType.Value()))

	logRecommendation(
		humanize.IBytes(uint64(memAvailable.Value())),
		int64(math.Round(float64(memAvailable.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(currentlyUsedMemory.Value())),
		int64(math.Round(float64(currentlyUsedMemory.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(kubepodsWorkingSetBytes.Value())),
		int64(math.Round(float64(kubepodsWorkingSetBytes.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(systemSliceWorkingSetBytes.Value())),
		int64(math.Round(float64(systemSliceWorkingSetBytes.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(containerdSliceWorkingSetBytes.Value())),
		int64(math.Round(float64(containerdSliceWorkingSetBytes.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(dockerSliceWorkingSetBytes.Value())),
		int64(math.Round(float64(dockerSliceWorkingSetBytes.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(kubeletSliceWorkingSetBytes.Value())),
		int64(math.Round(float64(kubeletSliceWorkingSetBytes.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(currentReservedMemory.Value())),
		int64(math.Round(float64(currentReservedMemory.Value())/float64(memTotal.Value())*100)),
		humanize.IBytes(uint64(targetReservedMemory.Value())),
		targetReservedMemory.String(),
		int64(math.Round(float64(targetReservedMemory.Value())/float64(memTotal.Value())*100)),
	)

	if targetReservedMemory.Value() < minimumReservedMemory.Value() {
		targetReservedMemory = minimumReservedMemory
	}

	// calculate the desired kubepods memory limit for direct enforcement on the kubepods cgroup
	targetKubepodsLimitInBytes := memTotal
	targetKubepodsLimitInBytes.Sub(targetReservedMemory)

	return targetKubepodsLimitInBytes, nil
}

func getContainerRuntimeWorkingSetBytes(cgroupRoot string, containerdMemoryCgroupName string) (resource.Quantity, resource.Quantity, error) {
	var (
		containerdSliceWorkingSetBytes resource.Quantity
		dockerSliceWorkingSetBytes resource.Quantity
		err error
	)

	containerdSliceWorkingSetBytes, err = getMemoryWorkingSet(cgroupRoot, containerdMemoryCgroupName)
	if err != nil {
		// this can be the cae if the node uses only docker as the container runtime
		containerdSliceWorkingSetBytes = resource.Quantity{}
	}

	dockerSliceWorkingSetBytes, err = getMemoryWorkingSet(cgroupRoot, fmt.Sprintf("%s/%s", types.SystemSliceCgroupName, types.DefaultDockerCgroupName))
	if err != nil {
		dockerSliceWorkingSetBytes = resource.Quantity{}
	}

	return containerdSliceWorkingSetBytes, dockerSliceWorkingSetBytes, nil
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
	containerdServiceWorkingSet string,
	containerdServiceWorkingSetPercentTotal int64,
	dockerServiceWorkingSet string,
	dockerServiceWorkingSetPercentTotal int64,
	kubeletServiceWorkingSet string,
	kubeletServiceWorkingSetPercentTotal int64,
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
		{" - Containerd.slice working set", fmt.Sprintf("%s (%d%%)", containerdServiceWorkingSet, containerdServiceWorkingSetPercentTotal)},
		{" - Docker.slice working set", fmt.Sprintf("%s (%d%%)", dockerServiceWorkingSet, dockerServiceWorkingSetPercentTotal)},
		{" - Kubelet.slice working set", fmt.Sprintf("%s (%d%%)", kubeletServiceWorkingSet, kubeletServiceWorkingSetPercentTotal)},
		{"Current reservation (kube+system reserved)", fmt.Sprintf("%s (%d%%)", currentReservedMemory, currentReservedMemoryPercentTotal)},
	})

	t.AppendSeparator()
	t.AppendRow(table.Row{"RECOMMENDATION", fmt.Sprintf("%s (%s, %d%%)", targetReservedMemory, targetReservedMemoryPrecise, targetReservedMemoryPercentTotal)})
	t.Render()
}

// getMemoryWorkingSet reads the given unit's memory cgroup and calculates
// the working set bytes
func getMemoryWorkingSet(cgroupRoot, unit string) (resource.Quantity, error) {
	memoryController := cgroups.NewMemory(cgroupRoot)

	stats := &cgroupstatsv1.Metrics{}
	if err := memoryController.Stat(unit, stats); err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to read memory stats for kubepods cgroup: %v", err)
	}

	// https://www.kernel.org/doc/Documentation/cgroup-v1/memory.txt
	// TODO: For efficiency, as other kernel components, memory cgroup uses some optimization
	// to avoid unnecessary cacheline false sharing. usage_in_bytes is affected by the
	// method and doesn't show 'exact' value of memory (and swap) usage, it's a fuzz
	// value for efficient access. (Of course, when necessary, it's synchronized.)
	// If you want to know more exact memory usage, you should use RSS+CACHE(+SWAP)
	// value in memory.stat(see 5.2).

	memoryWorkingSetBytes := stats.Memory.Usage.Usage - stats.Memory.TotalInactiveFile
	return resource.ParseQuantity(fmt.Sprintf("%d", memoryWorkingSetBytes))
}

// getMemoryWorkingSet reads the given unit's memory cgroup to return the memory limit in bytes
func getMemoryLimitInBytes(cgroupRoot, unit string) (resource.Quantity, error) {
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
