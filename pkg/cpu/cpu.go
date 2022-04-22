package cpu

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"time"

	linuxproc "github.com/c9s/goprocinfo/linux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// TODO: possibly use --kube-reserved-cgroup  read from kubelet configuration instead
	// Should be based on cgroup driver (systemdDbus: kubepods.slice, cgroups: kubepods) and from kubelet config
	cgroupKubepods      = "kubepods"
	cgroupSystemSlice   = "system.slice"
	cgroupStatCPUShares = "cpu.shares"
	cgroupStatCPUUsage  = "cpuacct.usage"
)

var (
	metricSystemSliceMinGuaranteedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_system_slice_min_guaranteed_cpu",
		Help: "The minimum guaranteed CPU time of the system.slice cgroup based on the cpu.shares (1024)",
	})

	metricCores = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_num_cpu_cores",
		Help: "The number of CPU cores of this node",
	})

	metricOverallCPUUsagePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cpu_usage_percent",
		Help: "The overall CPU usage based on /proc/stats",
	})

	metricKubepodsCurrentCPUConsumptionPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_kubepods_cpu_percent",
		Help: "The CPU consumption of the kubepods cgroup in percent",
	})

	metricSystemSliceCurrentCPUConsumptionPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_system_slice_cpu_percent",
		Help: "The CPU consumption of the system.slice cgroup in percent",
	})

	metricSystemSliceFreeCPUTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_system_slice_free_cpu_time",
		Help: "The freely absolute available CPU time for the system.slice cgroup in percent (100 = 1 core)",
	})

	metricKubepodsFreeCPUTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_kubepods_free_cpu_time",
		Help: "The freely absolute available CPU time for the kubepods cgroup in percent (100 = 1 core)",
	})

	metricTargetReservedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_target_reserved_cpu",
		Help: "The target kubelet reserved CPU",
	})

	metricCurrentReservedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_current_reserved_cpu",
		Help: "The current kubelet reserved CPU",
	})
)

// ReconcileKubeReservedCPU reconciles the current kube-reserved CPU settings against
// the actual (target) CPU consumption of system.slice
func ReconcileKubeReservedCPU(log *logrus.Logger, reconciliationPeriod time.Duration, cgroupsHierarchyRoot string) (*resource.Quantity, error) {
	numCPU := int64(runtime.NumCPU())
	cgroupsHierarchyCPU := fmt.Sprintf("%s/cpu", cgroupsHierarchyRoot)

	systemSliceCPUShares, err := getCPUStat(cgroupsHierarchyCPU, cgroupSystemSlice, cgroupStatCPUShares)
	if err != nil {
		return nil, err
	}

	kubepodsCPUShares, err := getCPUStat(cgroupsHierarchyCPU, cgroupKubepods, cgroupStatCPUShares)
	if err != nil {
		return nil, err
	}

	// System.slice's relative CPU time for ALL cores = (Active cgroup CPU shares) / (sum of all possible CPU shares of the cgroup SIBLINGS)
	// System.slice's relative CPU time for ONE core = Total CPU time for all cores * number of cores
	systemSliceGuaranteedCPUTimePercent := ((float64(systemSliceCPUShares) / (float64(systemSliceCPUShares) + float64(kubepodsCPUShares))) * float64(numCPU)) * 100
	kubepodsGuaranteedCPUTimePercent := ((float64(kubepodsCPUShares) / (float64(systemSliceCPUShares) + float64(kubepodsCPUShares))) * float64(numCPU)) * 100

	log.Infof("Guaranteed CPU time on this %d core machine: system.slice:  %.2f percent | kubepods:  %.2f percent CPU time. \n", numCPU, systemSliceGuaranteedCPUTimePercent, kubepodsGuaranteedCPUTimePercent)

	// measure overall CPU usage using `/proc/stats` and cpu usage of cgroups using the cgroupfs
	// For Linux, to determine the overall CPU usage, use the info exposed by the kernel in `/proc/stats` instead of
	// cgroups on system.slice or the root cgroup hierarchy.
	//  => Similar to why we read /proc/meminfo for memory and then calculate the CPU that is left based on that.
	// Reason:
	// - cgroup accounting in system.slice fails to account for processes outside system.slice  (like when starting process from user shell, will end up in user.slice)
	//   - Example: cat /dev/zero > /dev/null   --> will not be account for in system.slice because it is in users.slice (check `ps -o cgroup <pid>`)
	// - cpu accounting information in cgroups v1 was not designed to be absolutely precise and can be way off.
	// Please refer to the following URL for more information: https://www.idnt.net/en-US/kb/941772
	overallCPUNonIdleTime, systemSliceCPUTime, kubepodsCPUTime, err := measureAverageCPUUsage(log, cgroupsHierarchyCPU, reconciliationPeriod)
	if err != nil {
		return nil, fmt.Errorf("failed to measure relative CPU time: %w", err)
	}

	// Calculation:
	//   CPU usage without kubepods = total CPU Usage  - cpu usage kubepods (can be inaccurate)
	// After, use then use below formula to calculate kubepodsTargetCPUShares (now I know systemSliceCPUTime which is more precise now)
	overallCPUUsageWithoutKubepods := overallCPUNonIdleTime - kubepodsCPUTime

	// for metrics and logging
	overallCPUNonIdleTimePercent := overallCPUNonIdleTime * 100
	kubepodsCPUTimePercent := kubepodsCPUTime * 100
	systemSliceCPUTimePercent := systemSliceCPUTime * 100


	log.Infof("CPU usage: CPU usage overall: %.2f | Total without kubepods: %.2f percent | system.slice via cgroupfs: %.2f percent | kubepods via cgroupfs: %.2f percent", overallCPUNonIdleTimePercent, overallCPUUsageWithoutKubepods * 100, systemSliceCPUTimePercent, kubepodsCPUTimePercent)

	// Uses the same formula from above (just resolved to the target kubepodsCPUShares and not using percent (not multiplied by 100)).
	// We know the:
		// systemSliceCPUShares -> from cgroupfs (sibling of kubepods)
		// overallCPUUsageWithoutKubepods (like systemSliceCPUTime in above formula, only precisely measure via /proc/stats and as if it would be the total CPU usage)

	// Caveat: in this formula, system.slice is the only cgroup sibling of kubepods that is considered to consume any CPU shares (measured via /proc/stats - kubepods consumption)
	// this can lead to inaccurate target cpu shares for kubepods if  there are other cgroups that consume much CPU time
	//   - the ratio between the kubepods cpu shares and the other cgroups will be off (because we calculate as if there are only 2 cgroups)
	// Hierarchically, we assume:
	// L0: root
	// L1 - system.slice(usually 1024 shares) , kubepods (to be calculated)
	kubepodsTargetCPUShares := int64(((float64(systemSliceCPUShares) * float64(numCPU)) / overallCPUUsageWithoutKubepods) - float64(systemSliceCPUShares))
	log.Infof("CPU shares: kubepods current: %d | kubepods target: %d | system.slice current: %d", kubepodsCPUShares, kubepodsTargetCPUShares, systemSliceCPUShares)

	// totalCPUShares set by the kubelet based on the amount of cores (not a Linux requirement)
	totalCPUShares := numCPU * 1024

	var targetKubeReservedCPU int64
	if kubepodsTargetCPUShares < totalCPUShares {
		targetKubeReservedCPU = totalCPUShares - kubepodsTargetCPUShares
	} else {
		// kubepodsTargetCPUShares can be > totalCPUShares
		// However, Kubernetes decided that the maximum CPU shares it  sets for the
		// kubepods cgroup = amount of cpus * 1024
		// Hence, if we want to give more than the K8s possible total amount of shares to the kubepods cgroup
		// (system.slice does not use a lot of CPU), then we could set the kube/system-reserved for CPU to 0
		// We set a low default amount of 80m instead, as over reserving CPU resources for system.slice
		// is not problematic (kubepods can exceed "fair share" of CPU time in case system.slice does not need it).

		// Please also note: Kubernetes STATICALLY SETS (or: does not change) the systems.slice cpu.shares to 1024
		// this is a problem, as cpu.shares work as a ratio against its siblings
		// See: https://github.com/kubernetes/kubernetes/issues/72881#issuecomment-821224980
		// targetKubeReservedCPU = defaultMinimumReservedCPU
		targetKubeReservedCPU = 0
		log.Debugf("defaulting reserved CPU to minimum")
	}

	// no need to read the kubelet configuration to get the current reserved CPU
	// it can be deduced by looking at the kubepods cpu.shares
	currentKubeReservedCPU := totalCPUShares - kubepodsCPUShares

	if currentKubeReservedCPU == targetKubeReservedCPU {
		log.Infof("CPU RECOMMENDATION: Reserved CPU of %dm fits recommendation", currentKubeReservedCPU)
	} else if currentKubeReservedCPU - targetKubeReservedCPU > 0 {
		action := "DECREASE"
		log.Infof("CPU RECOMMENDATION: %s reserved CPU from %dm to %dm", action, currentKubeReservedCPU, targetKubeReservedCPU)
	} else {
		action := "INCREASE"
		log.Infof("CPU RECOMMENDATION: %s reserved CPU from %dm to %dm", action, currentKubeReservedCPU, targetKubeReservedCPU)
	}

	// record prometheus metrics
	metricCores.Set(float64(numCPU))
	metricSystemSliceMinGuaranteedCPU.Set(math.Round(systemSliceGuaranteedCPUTimePercent))
	metricKubepodsCurrentCPUConsumptionPercent.Set(math.Round(kubepodsCPUTimePercent))
	metricSystemSliceCurrentCPUConsumptionPercent.Set(math.Round(systemSliceCPUTimePercent))
	metricOverallCPUUsagePercent.Set(math.Round(overallCPUNonIdleTimePercent))
	// TODO: add metric that calculates the normal overall CPU utilisation of the node as: (metricOverallCPUUsagePercent/num_cores)
	metricCurrentReservedCPU.Set(float64(currentKubeReservedCPU))
	metricTargetReservedCPU.Set(float64(targetKubeReservedCPU))

	// calculated metrics
	// only because all allotted CPU shares have been used for a cgroup, does not mean there is resource contention
	// as a cgroup can use more than their "fair share" if another cgroup does not need all of theirs
	kubepodsFreeCPUTime := kubepodsGuaranteedCPUTimePercent - kubepodsCPUTimePercent
	metricKubepodsFreeCPUTime.Set(kubepodsFreeCPUTime)

	systemSliceFreeCPUTime := systemSliceGuaranteedCPUTimePercent - systemSliceCPUTimePercent
	metricSystemSliceFreeCPUTime.Set(systemSliceFreeCPUTime)

	quantity := resource.MustParse(fmt.Sprintf("%dm", targetKubeReservedCPU))
	return &quantity, nil
}

// getCPUStat reads a numerical cgroup stat from the cgroupFS
func getCPUStat(cgroupsHierarchyCPU, cgroup, cpuStat string) (int64, error) {
	// unfortunately, github.com/containerd/cgroups only supports rudimentary CPU cgroup stats.
	// have to read it myself from the filesystem
	value, err := ReadUint(filepath.Join(cgroupsHierarchyCPU, cgroup, cpuStat))
	if err != nil {
		return 0, err
	}
	return int64(value), nil
}

// measureAverageCPUUsage measures the relative CPU usage of the kubepods and system.slice cgroup over a period of time
// compared to the overall CPU time of all CPU cores
// a return value of 1.1 means that the cgroup has used 110% of the CPU time of one core
func measureAverageCPUUsage(log *logrus.Logger, cgroupsHierarchyCPU string, reconciliationPeriod time.Duration) (float64, float64, float64, error) {
	startSystemSlice := time.Now().UnixNano()

	startSystemSliceCPUUsage, err := getCPUStat(cgroupsHierarchyCPU, cgroupSystemSlice, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, 0, err
	}

	startKubepods := time.Now().UnixNano()
	startKubepodsCPUUsage, err := getCPUStat(cgroupsHierarchyCPU, cgroupKubepods, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, 0, err
	}

	// measure CPU usage outside kubepods with /proc/stats
	startCPUTime, startIdleCPUTime, err := readProcStats(err)
	if err != nil {
		return 0, 0, 0, err
	}

	duration := reconciliationPeriod / 2
	time.Sleep(duration)

	stopSystemSliceCPUUsage, err := getCPUStat(cgroupsHierarchyCPU, cgroupSystemSlice, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, 0, err
	}
	stopSystemSlice := time.Now().UnixNano()

	stopKubepodsCPUUsage, err := getCPUStat(cgroupsHierarchyCPU, cgroupKubepods, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, 0, err
	}
	stopKubepods := time.Now().UnixNano()

	// measure CPU usage outside kubepods with /proc/stats
	stopTotalCPUTime, stopIdleCPUTime, err := readProcStats(err)
	if err != nil {
		return 0, 0, 0, err
	}


	// /proc/stats cpu stats are in given in Jiffies (duration of 1 tick of the system timer interrupt.)
	// So we cannot just divide the diff by the elapsed time measure with time.Now() - nanoseconds.
	// First would need to convert the Jiffies to nanoseconds using USR_HERTZ -> the length of a clock tick.
	// It is easier however, to just calculate the relation between idling and processing CPU time to get the usage during sleep()
	diffTotal := stopTotalCPUTime - startCPUTime
	diffIdle := stopIdleCPUTime - startIdleCPUTime
	log.Debugf("Jiffie diff total: %d | diffIdle: %d", diffTotal, diffIdle)
	procStatOverallCPUUsage := (float64(diffTotal) - float64(diffIdle)) / float64(diffTotal)
	log.Debugf("Overall CPU usage is %.2f percent", procStatOverallCPUUsage * 100)


	elapsedTimeKubepods := float64(stopKubepods) - float64(startKubepods)
	elapsedTimeSystemSlice := float64(stopSystemSlice) - float64(startSystemSlice)
	systemSliceRelativeCPUUsage := (float64(stopSystemSliceCPUUsage) - float64(startSystemSliceCPUUsage)) / elapsedTimeSystemSlice
	kubepodsRelativeCPUUsage := (float64(stopKubepodsCPUUsage) - float64(startKubepodsCPUUsage)) / elapsedTimeKubepods
	return procStatOverallCPUUsage, systemSliceRelativeCPUUsage, kubepodsRelativeCPUUsage, nil
}

// readProcStats reads from /proc/stat and returns
//  - the total processing CPU time since system start as first return value
//  - the total idle CPU time since system start as second return value
func readProcStats(err error) (uint64, uint64, error) {
	statsCurr, err := linuxproc.ReadStat("/proc/stat")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to rad from /proc/stat to determine current CPU usage")
	}

	// time spent since system startKubepods on CPU idle or IO Wait
	idleCPUTime := statsCurr.CPUStatAll.Idle + statsCurr.CPUStatAll.IOWait

	// sum em all up to get the total amount of CPU time that has been spent since system start Kubepods of processing
	totalCPUTime :=
		statsCurr.CPUStatAll.User +
		statsCurr.CPUStatAll.Nice +
		statsCurr.CPUStatAll.System +
		statsCurr.CPUStatAll.IRQ +
		statsCurr.CPUStatAll.SoftIRQ +
		statsCurr.CPUStatAll.Steal +
			// also add idle to make up total
			idleCPUTime
	return totalCPUTime, idleCPUTime, nil
}

