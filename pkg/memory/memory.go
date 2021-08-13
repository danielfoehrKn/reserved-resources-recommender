package memory

import (
	"fmt"
	"math"
	"strconv"

	linuxproc "github.com/c9s/goprocinfo/linux"
	"github.com/containerd/cgroups"
	cgroupstatsv1 "github.com/containerd/cgroups/stats/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	kubeletv1beta1 "k8s.io/kubelet/config/v1beta1"
)

const (
	memorySafetyMarginValue        = "200Mi"
	// PoC: assumes kubepods memory controller mounted at /sys/fs/cgroups/memory/kubepods
	// Should be based on cgroup driver (systemdDbus: kubepods.slice, cgroups: kubepods) and from kubelet config
	cgroupRoot = "/sys/fs/cgroup"
	kubepodsMemoryCgroupName = "kubepods"
	systemSliceMemoryCgroupName = "system.slice"
)


var (
	// memorySafetyMargin is the additional amount of memory added to the kube-reserved memory compared to what is
	// actually reserved by other processes + kernel
	// this is to make sure the cgroup limit hits before the OS OOM
	memorySafetyMargin = resource.MustParse(memorySafetyMarginValue)
	// defaultMinThresholdPercent is the default minimum percentage of OS memory available, that triggeres an update & restart of the kubelet
	// 0.9 means that it should reconcile if less than 90% of OS memory is available
	defaultMinThresholdPercent     = "0.9"
	// defaultMinDeltaAbsolute is the default minimum absolut difference between kube-reserved memory and the actual
	// available memory which makes an update of the reserved memory necessary
	// if < 100MI difference in actual vs. desired, do not update / restart the kubelet
	defaultMinDeltaAbsolute        = "100Mi"

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

// ReconcileKubeReservedMemory reconciles the current kube-reserved memory settings against
// the actual (target) memory consumption of system.slice
func ReconcileKubeReservedMemory(
	log *logrus.Logger,
	kubeletConfig *kubeletv1beta1.KubeletConfiguration,
	minimumDelta,
	minimumThreshold resource.Quantity) (*resource.Quantity, bool, string, error) {

	memTotal, memAvailable, err := ParseProcMemInfo()
	if err != nil {
		log.Fatalf("fatal error during reconciliation: %v", err)
	}

	kubepodsWorkingSetBytes, err := getMemoryWorkingSet(kubepodsMemoryCgroupName)
	if err != nil {
		return nil, false, "", err
	}

	systemSliceWorkingSetBytes, err := getMemoryWorkingSet(systemSliceMemoryCgroupName)
	if err != nil {
		return nil, false, "", err
	}

	kubeReservedMemory, err := getKubeReservedMemory(*kubeletConfig)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to parse kube-reserved memory as resource quantity: %v", err)
	}

	systemReservedMemory, err := getSystemReservedMemory(*kubeletConfig)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to parse system-reserved memory as resource quantity: %v", err)
	}

	// total reserved memory of the kubelet is system + kube-reserved
	currentReservedMemory := kubeReservedMemory
	currentReservedMemory.Add(systemReservedMemory)

	// Calculation: target reserved memory =
	// MemTotal
	// - MemAvailable
	// - working_set_bytes of kubepods cgroup
	// + safety margin
	targetReservedMemory := memTotal
	targetReservedMemory.Sub(memAvailable)
	currentlyUsedMemory := targetReservedMemory
	targetReservedMemory.Sub(kubepodsWorkingSetBytes)
	targetReservedMemory.Add(memorySafetyMargin)

	log.Infof("Available memory from /proc/mem: %q (%d percent)", memAvailable.String(), int64(math.Round(float64(memAvailable.Value())/float64(memTotal.Value())*100)))
	log.Infof("Used memory: %q (%d percent)", currentlyUsedMemory.String(), int64(math.Round(float64(currentlyUsedMemory.Value())/float64(memTotal.Value())*100)))
	log.Infof("Kubepods working set memory: %q (%d percent)", kubepodsWorkingSetBytes.String(), int64(math.Round(float64(kubepodsWorkingSetBytes.Value())/float64(memTotal.Value())*100)))
	log.Infof("System.slice working set memory: %q (%d percent)", systemSliceWorkingSetBytes.String(), int64(math.Round(float64(systemSliceWorkingSetBytes.Value())/float64(memTotal.Value())*100)))

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
	// likely over reserve memory
	if targetReservedMemory.Value() < 0 {
		log.Infof("No memory recommendation can be provided. Memory accounting seems to be off. You can use the working set of system.slice instead, though this will most likely over-reserve memory.")
		metricTargetReservedMemoryBytes.Set(-1)
		metricTargetReservedMemoryPercent.Set(0)
		return nil, false, "", nil
	}

	// difference old reserved settings - target reserved
	diffOldMinusNewReserved := currentReservedMemory
	diffOldMinusNewReserved.Sub(targetReservedMemory)

	action := "INCREASE"
	if diffOldMinusNewReserved.Value() > 0 {
		action = "DECREASE"
	}

	log.Infof("RECOMMENDATION: %s reserved memory from %q (%d percent, kube: %q, system: %q) to %q (%d percent)",
		action,
		currentReservedMemory.String(),
		int64(math.Round(float64(currentReservedMemory.Value())/float64(memTotal.Value())*100)),
		kubeReservedMemory.String(),
		systemReservedMemory.String(),
		targetReservedMemory.String(),
		int64(math.Round(float64(targetReservedMemory.Value())/float64(memTotal.Value())*100)),
		)

	// record prometheus metrics
	metricTargetReservedMemoryBytes.Set(float64(targetReservedMemory.Value()))
	metricTargetReservedMemoryPercent.Set(math.Round(float64(targetReservedMemory.Value()) / float64(memTotal.Value()) * 100))

	// kube-reserved = reserved memory - system-reserved
	// because we only manipulate kube-reserved in this PoC
	targetKubeReserved := targetReservedMemory // includes safety margin
	targetKubeReserved.Sub(systemReservedMemory)

	shouldBeUpdated, reason := shouldUpdateKubeReserved(memAvailable, minimumThreshold, minimumDelta, diffOldMinusNewReserved)

	return &targetKubeReserved, shouldBeUpdated, reason, nil
}

// shouldUpdateKubeReserved checks if the kube-reserved values should be updated
// If should not  be updated, returns false as the first, and a reason as the second argument
func shouldUpdateKubeReserved(memAvailable, minimumThreshold, minimumDelta, diffOldMinusNewReserved resource.Quantity) (bool, string) {
	if memAvailable.Value() > minimumThreshold.Value() {
		return false, fmt.Sprintf("Available memory of %s does not fall below threshold of %s. Do nothing.", memAvailable.String(), minimumThreshold.String())
	}

	// only if the desired change is > threshold, we consider it significant enough to update the kube reserved
	// and restart the kubelet
	if math.Abs(float64(diffOldMinusNewReserved.Value())) < float64(minimumDelta.Value()) {
		return false, fmt.Sprintf("SKIPPING: Delta of target reserved memory and current reserved memory is %q (minimum delta: %q).", diffOldMinusNewReserved.String(), minimumDelta.String())
	}

	return true, ""
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

// getCurrentKubeReservedMemory takes the kubelet configuration and
// returns the the kube reserved memory or the the kubelet default if not set
func getKubeReservedMemory(config kubeletv1beta1.KubeletConfiguration) (resource.Quantity, error) {
	mem, ok := config.KubeReserved[string(corev1.ResourceMemory)]
	if !ok {
		// currently not set in config. Defaulted by kubelet to 100Mi
		return resource.MustParse("100Mi"), nil
	}

	// parse memory (can be BinarySI or DecimalSI)
	return resource.ParseQuantity(mem)
}

// getSystemReservedMemory takes the kubelet configuration and
// returns the the system reserved memory or nil if not set
func getSystemReservedMemory(config kubeletv1beta1.KubeletConfiguration) (resource.Quantity, error) {
	mem, ok := config.SystemReserved[string(corev1.ResourceMemory)]
	if !ok {
		// there is no default system-reserved memory
		return resource.Quantity{}, nil
	}

	// parse memory (can be BinarySI or DecimalSI)
	return resource.ParseQuantity(mem)
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


// GetMemoryMinimumAbsoluteThreshold uses the given minThresholdPercent (originated from an environment variable) or
// uses the defaultMinThresholdPercent to calculate the defaultMinDeltaAbsolute value.
// Returns the minimum difference (e.g 200 Mi) between kube-reserved memory and the actual
// available memory which makes an update of the reserved memory necessary.
func GetMemoryMinimumAbsoluteThreshold(memTotal uint64, minThresholdPercent string) (resource.Quantity, error) {
	if len(minThresholdPercent) == 0 {
		minThresholdPercent = defaultMinThresholdPercent
	}

	minThreshold, err := strconv.ParseFloat(minThresholdPercent, 64)
	if err != nil {
		return resource.Quantity{}, err
	}

	if minThreshold < 0 || minThreshold >= 1 {
		return resource.Quantity{}, fmt.Errorf("MIN_THRESHOLD_PERCENT has to be in range 0 - 1")
	}

	value := int64(float64(memTotal) * minThreshold)

	return resource.ParseQuantity(fmt.Sprintf("%d", value))
}

// GetMemoryMinimumAbsoluteDelta parses the given minDeltaAbsolute (originated from an environment variable)
// or returns the defaultMinDeltaAbsolute
func GetMemoryMinimumAbsoluteDelta(minDeltaAbsolute string) (resource.Quantity, error) {
	if len(minDeltaAbsolute) == 0 {
		minDeltaAbsolute = defaultMinDeltaAbsolute
	}

	return resource.ParseQuantity(minDeltaAbsolute)
}
