package cpu

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	linuxproc "github.com/c9s/goprocinfo/linux"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/cpu/util"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/types"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

const (
	// cgroupStatCPUShares is the name of the file setting the amount of cpu shares for a particular cgroup
	cgroupStatCPUShares = "cpu.shares"
	// cgroupStatCPUUsage is the name of the cpu usage file in the cgroup filesystem
	cgroupStatCPUUsage = "cpuacct.usage"
)

// TODO: update to work with cgroupsv2 ---> nore more cpu.shares file, but cpu.weight in each cgroup with cpu controller enabled
//  - https://utcc.utoronto.ca/~cks/space/blog/linux/CgroupV2FairShareScheduling
// root@shoot--cs-core--ghrunner-app-z1-54b5c-6lvcs:/# cat /sys/fs/cgroup/kubepods/cpu.weight
// 622

var (
	metricSystemSliceMinGuaranteedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_system_slice_min_guaranteed_cpu",
		Help: "The minimum guaranteed CPU time of the system.slice cgroup based on the cpu.shares (1024)",
	})

	metricKubepodsMinGuaranteedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_kubepods_min_guaranteed_cpu",
		Help: "The minimum guaranteed CPU time of the kubepods cgroup based on the cgroup's cpu.shares",
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

	metricTargetReservedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_target_reserved_cpu",
		Help: "The target kubelet reserved CPU",
	})

	metricTargetReservedCPUMachineType = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_target_reserved_cpu_machine_type",
		Help: "The target kubelet reserved CPU based on the machine type",
	})

	metricCurrentReservedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_current_reserved_cpu",
		Help: "The current kubelet reserved CPU",
	})
)

// RecommendCPUReservations recommends kubelet CPU reservations by
// measuring overall, kubepods and system.slice CPU consumption and comparing those measurements against current CPU reservations (based on CPU shares of the cgroups)
func RecommendCPUReservations(log *logrus.Logger, reconciliationPeriod time.Duration, cgroupsHierarchyRoot string, numCPU int64) (int64, error) {
	cgroupsHierarchyCPU := fmt.Sprintf("%s/cpu", cgroupsHierarchyRoot)

	systemSliceCPUShares, err := getCPUStat(cgroupsHierarchyCPU, types.SystemSliceCgroupName, cgroupStatCPUShares)
	if err != nil {
		return 0, err
	}

	kubepodsCPUShares, err := getCPUStat(cgroupsHierarchyCPU, types.DefaultkubepodsCgroupName, cgroupStatCPUShares)
	if err != nil {
		return 0, err
	}

	// System.slice's relative CPU time for ALL cores = (Active cgroup CPU shares) / (sum of all possible CPU shares of the cgroup SIBLINGS)
	// System.slice's relative CPU time for ONE core = Total CPU time for all cores * number of cores
	systemSliceGuaranteedCPUTimePercent := ((float64(systemSliceCPUShares) / (float64(systemSliceCPUShares) + float64(kubepodsCPUShares))) * float64(numCPU)) * 100
	kubepodsGuaranteedCPUTimePercent := ((float64(kubepodsCPUShares) / (float64(systemSliceCPUShares) + float64(kubepodsCPUShares))) * float64(numCPU)) * 100

	log.Debugf("Guaranteed CPU time: system.slice:  %.2f percent (%d shares) | kubepods:  %.2f percent (%d shares). \n", systemSliceGuaranteedCPUTimePercent, systemSliceCPUShares, kubepodsGuaranteedCPUTimePercent, kubepodsCPUShares)

	// Measure overall CPU usage using `/proc/stats` and cpu usage of cgroups using the cgroupfs
	// For Linux, to determine the overall CPU usage, use the info exposed by the kernel in `/proc/stats` instead of
	// cgroups on system.slice or the root cgroup hierarchy.
	// Why do we not just check the cgroup CPU usage stats on system.slice to obtain CPU usage for all non-pod processes?
	// Reason:
	// - Similar to why we read /proc/meminfo for memory and then calculate the CPU that is left based on that.
	// - cgroup accounting in system.slice fails to account for processes outside system.slice  (like when starting process from user shell, will end up in user.slice)
	//   - Example: cat /dev/zero > /dev/null   --> will not be account for in system.slice because it is in users.slice (check `ps -o cgroup <pid>`)
	// - CPU accounting information in cgroups v1 was not designed to be absolutely precise and can be way off.
	// Please refer to the following URL for more information: https://www.idnt.net/en-US/kb/941772
	overallCPUNonIdleTime, systemSliceCPUTime, kubepodsCPUTime, err := measureAverageCPUUsage(log, cgroupsHierarchyCPU, reconciliationPeriod, numCPU)
	if err != nil {
		return 0, fmt.Errorf("failed to measure relative CPU time: %w", err)
	}

	// Calculation:
	// - CPU usage without kubepods = total CPU Usage  - cpu usage kubepods (can be inaccurate)
	// - After, use below formula to calculate kubepodsTargetCPUShares (now I know systemSliceCPUTime which is more precise)
	// e.g overallCPUNonIdleTime(2.02 = 2 cores) - kubepodsCPUTime(1.7 core)
	cpuUsageNonPodProcesses := overallCPUNonIdleTime - kubepodsCPUTime

	// for metrics and logging
	overallCPUNonIdleTimePercent := overallCPUNonIdleTime * 100
	kubepodsCPUTimePercent := kubepodsCPUTime * 100
	systemSliceCPUTimePercent := systemSliceCPUTime * 100

	log.Debugf("CPU total via /proc/stat: %.2f percent| non-pod processes: %.2f percent | system.slice via cgroupfs: %.2f percent | kubepods via cgroupfs: %.2f percent", overallCPUNonIdleTimePercent, cpuUsageNonPodProcesses*100, systemSliceCPUTimePercent, kubepodsCPUTimePercent)

	// For the calculation, take the higher of the two cpu utilisations reported for system.slice
	//  -> They should in theory be identical, but are mostly a bit different probably due to best-effort accounting on the cgroup
	//  -> this might cause a slight over-reservation
	cpuTimeNonPodProcesses := systemSliceCPUTime
	if cpuUsageNonPodProcesses > systemSliceCPUTime {
		cpuTimeNonPodProcesses = cpuUsageNonPodProcesses
	}

	// Uses the same formula from above (just resolved to the target kubepodsCPUShares and not using percent (not multiplied by 100)).
	// We know the:
	// - systemSliceCPUShares -> from cgroupfs (sibling of kubepods)
	// - cpuUsageNonPodProcesses (like systemSliceCPUTime in above formula, only precisely measure via /proc/stats and as if it would be the total CPU usage)

	// Caveat: in this formula, system.slice is the only cgroup sibling of kubepods that is considered to consume any CPU shares (measured via /proc/stats - kubepods consumption).
	// This can lead to inaccurate target cpu shares for kubepods if  there are other cgroups that consume much CPU time
	//   - the ratio between the kubepods cpu shares and the other cgroups will be off (because we calculate as if there are only 2 cgroups)
	// Hierarchically, we assume:
	// L0: root
	// L1 - system.slice(usually 1024 shares) , kubepods (to be calculated)
	// Example:
	//  - system.slice: 1024 shares
	//  - CPU usage non-pod processes according to /proc/stat: 37.69% (calculated via total from /proc/stat - measurement for kubepods from cgroup)
	//  - kubepods cpu usage via cgroupfs: 206.99 percent
	//  - numCores = 16
	// Goal: we want that system.slice gets only 37.69% CPU time via CPU Shares
	// 42446 shares = ((1024 * 16) / 0.3769) - 1024
	// This makes sense (surprisingly) as that means that system.slice only gets 2.5% (42446 / 1024) of total CPU time. Which over all cores is 38.5 % (that's what we want).
	// Of course, if kubepods requires much more CPU it might also be that system.slice requires more than only 38.5 %, then this will be visible when executing the recommender again.
	kubepodsTargetCPUShares := int64(((float64(systemSliceCPUShares) * float64(numCPU)) / cpuTimeNonPodProcesses) - float64(systemSliceCPUShares))
	log.Debugf("CPU shares: kubepods current: %d | kubepods target: %d | system.slice current: %d", kubepodsCPUShares, kubepodsTargetCPUShares, systemSliceCPUShares)

	// kubernetesTotalCPUSharesForNCores set by the kubelet based on the amount of cores (not a Linux requirement)
	kubernetesTotalCPUSharesForNCores := numCPU * 1024

	var targetKubeReservedCPU int64
	if kubepodsTargetCPUShares < kubernetesTotalCPUSharesForNCores {
		// While we set CPU shares on the cgroup (Binary SI), we also want to report the target reserved memory in milli-CPU cores (Decimal SI)
		// Kube reserved is given in decimal SI (see: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#meaning-of-cpu)
		// --> conversion is needed
		// 1 core = 1024 CPU shares in the kubernetes world
		// 1024=2^10=1 in Binary Si  which equals 1000m = 1 core in Decimal SI
		targetKubeReservedCPU = util.DecimalSIForBinarySi(kubernetesTotalCPUSharesForNCores - kubepodsTargetCPUShares)
	} else {
		// kubepodsTargetCPUShares can be > kubernetesTotalCPUSharesForNCores
		// However, Kubernetes decided that the maximum CPU shares it  sets for the
		// kubepods cgroup = amount of cpus * 1024
		// Hence, if we want to give more than the K8s possible total amount of shares to the kubepods cgroup
		// (system.slice does not use a lot of CPU), then we could set the kube/system-reserved for CPU to 0
		// We set a low default amount of 80m instead, as over reserving CPU resources for system.slice
		// is not problematic (kubepods can exceed "fair share" of CPU time in case system.slice does not need it).

		// Please also note: Kubernetes STATICALLY SETS (or: does not change) the systems.slice cpu.shares to 1024
		// this is a problem, as cpu.shares work as a ratio against its siblings
		// targetKubeReservedCPU = defaultMinimumReservedCPU
		// Hence, unfortunately, enforcing CPU reservations does not make any sense at this point
		// - Please see: https://github.com/kubernetes/kubernetes/issues/72881#issuecomment-897217732
		// - Problem: By reserving 5 cores on a 94 core machine, Linux actually only granted 0.1 cores more in relation to system.slice.
		// => the scheduler prevents actual workload of 5 cores to be scheduled which makes it not usable -,-
		targetKubeReservedCPU = 0
		log.Debugf("defaulting reserved CPU to minimum")
	}

	// for comparison, also calculate target CPU reservation based on GKE's formula which uses a function of the capacity (num cores)
	targetKubeReservedCPUMachineType := util.CalculateCPUReservationBasedOnCapacity(numCPU)

	// no need to read the kubelet configuration to get the current reserved CPU
	// it can be deduced by looking at the kubepods cpu.shares
	currentKubeReservedCPU := kubernetesTotalCPUSharesForNCores - kubepodsCPUShares

	log.Debugf("Recommended reserved CPU: %dm (current: %dm). Reason: reserving %.2f percent CPU for non-pod processes requires %d CPU shares for kubepods with system.slice having %d CPU shares.", targetKubeReservedCPU, currentKubeReservedCPU, cpuUsageNonPodProcesses*100, kubepodsTargetCPUShares, systemSliceCPUShares)

	logRecommendation(
		overallCPUNonIdleTimePercent,
		systemSliceGuaranteedCPUTimePercent,
		kubepodsGuaranteedCPUTimePercent,
		kubepodsCPUShares,
		cpuUsageNonPodProcesses*100,
		systemSliceCPUTimePercent,
		kubepodsCPUTimePercent,
		targetKubeReservedCPU,
		currentKubeReservedCPU,
		kubepodsTargetCPUShares,
		systemSliceCPUShares)

	// record prometheus metrics
	recordMetrics(
		numCPU,
		systemSliceGuaranteedCPUTimePercent,
		kubepodsCPUTimePercent,
		systemSliceCPUTimePercent,
		overallCPUNonIdleTimePercent,
		currentKubeReservedCPU,
		targetKubeReservedCPU,
		targetKubeReservedCPUMachineType,
		kubepodsGuaranteedCPUTimePercent)

	// Do not enforce kubepods CPU shares that would exceed the maximum CPU shares set by the kubelet
	// this effectively makes sure that system.slice has the same minimum guaranteed CPU time as if the kubelet does not reserve any CPU for system processes
	// For example:
	//  - Total cores=16.
	//  - Maximum shares set on kubepods by kubelet (for 0 CPU reservation) = 16 * 1024 shares.
	//  - System.slice always has 1024 shares.
	//    This guarantees a minimum CPU time of 95% of one core for system.slice processes no matter how little CPU the system processes actually consume.
	if kubepodsTargetCPUShares > kubernetesTotalCPUSharesForNCores {
		kubepodsTargetCPUShares = kubernetesTotalCPUSharesForNCores
	}

	return kubepodsTargetCPUShares, nil
}

func recordMetrics(numCPU int64,
	systemSliceGuaranteedCPUTimePercent float64,
	kubepodsCPUTimePercent float64,
	systemSliceCPUTimePercent float64,
	overallCPUNonIdleTimePercent float64,
	currentKubeReservedCPU int64,
	targetKubeReservedCPU int64,
	targetKubeReservedCPUMachineType int64,
	kubepodsGuaranteedCPUTimePercent float64) {
	metricCores.Set(float64(numCPU))
	metricSystemSliceMinGuaranteedCPU.Set(math.Round(systemSliceGuaranteedCPUTimePercent))
	metricKubepodsMinGuaranteedCPU.Set(math.Round(kubepodsGuaranteedCPUTimePercent))
	metricKubepodsCurrentCPUConsumptionPercent.Set(math.Round(kubepodsCPUTimePercent))
	metricSystemSliceCurrentCPUConsumptionPercent.Set(math.Round(systemSliceCPUTimePercent))
	metricOverallCPUUsagePercent.Set(math.Round(overallCPUNonIdleTimePercent))
	metricCurrentReservedCPU.Set(float64(currentKubeReservedCPU))
	metricTargetReservedCPU.Set(float64(targetKubeReservedCPU))
	metricTargetReservedCPUMachineType.Set(float64(targetKubeReservedCPUMachineType))
}

func logRecommendation(
	overallCPUNonIdleTimePercent float64,
	systemSliceGuaranteedCPUTimePercent float64,
	kubepodsGuaranteedCPUTimePercent float64,
	currentKubepodsCPUShares int64,
	cpuUsageNonPodProcesses float64,
	systemSliceCPUTimePercent float64,
	kubepodsCPUTimePercent float64,
	targetKubeReservedCPU int64,
	currentKubeReservedCPU int64,
	kubepodsTargetCPUShares int64,
	systemSliceCPUShares int64) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"CPU Metric", "Value"})

	t.AppendRows([]table.Row{
		{"Total CPU usage via /proc/stat", fmt.Sprintf("%.2f%%", overallCPUNonIdleTimePercent)},
		{"Current guaranteed CPU time", fmt.Sprintf("system.slice: %.2f%% | kubepods: %.2f%%", systemSliceGuaranteedCPUTimePercent, kubepodsGuaranteedCPUTimePercent)},
		{"Current CPU shares", fmt.Sprintf("system.slice: %d | kubepods: %d", systemSliceCPUShares, currentKubepodsCPUShares)},
		{"CPU usage non-pod processes", fmt.Sprintf("%.2f%%", cpuUsageNonPodProcesses)},
		{"CPU usage system.slice (cgroupfs)", fmt.Sprintf("%.2f%%", systemSliceCPUTimePercent)},
		{"CPU usage kubepods (cgroupfs)", fmt.Sprintf("%.2f%%", kubepodsCPUTimePercent)},
		{"Current reservation", fmt.Sprintf("%dm", currentKubeReservedCPU)},
	})

	t.AppendSeparator()
	t.AppendRow(table.Row{"RECOMMENDATION", fmt.Sprintf("%dm (kubepods CPU shares: %d)", targetKubeReservedCPU, kubepodsTargetCPUShares)})
	t.Render()
}

// getCPUStat reads a numerical cgroup stat from the cgroupFS
func getCPUStat(cgroupsHierarchyCPU, cgroup, cpuStat string) (int64, error) {
	// unfortunately, github.com/containerd/cgroups only supports rudimentary CPU cgroup stats.
	// have to read it myself from the filesystem
	value, err := util.ReadUint(filepath.Join(cgroupsHierarchyCPU, cgroup, cpuStat))
	if err != nil {
		return 0, err
	}
	return int64(value), nil
}

// measureAverageCPUUsage measures the relative CPU usage of the kubepods and system.slice cgroup over a period of time
// compared to the overall CPU time of all CPU cores.
// A return value of 1.1 means that the cgroup has used 110% of the CPU time of one core
func measureAverageCPUUsage(log *logrus.Logger, cgroupsHierarchyCPU string, period time.Duration, numCPU int64) (float64, float64, float64, error) {
	startSystemSlice := time.Now().UnixNano()
	startSystemSliceCPUUsage, err := getCPUStat(cgroupsHierarchyCPU, types.SystemSliceCgroupName, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, 0, err
	}

	startKubepods := time.Now().UnixNano()
	startKubepodsCPUUsage, err := getCPUStat(cgroupsHierarchyCPU, types.DefaultkubepodsCgroupName, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, 0, err
	}

	// measure CPU usage outside kubepods with /proc/stats
	startTotalCPUTime, startIdleCPUTime, err := readProcStats(err)
	if err != nil {
		return 0, 0, 0, err
	}

	time.Sleep(period)

	stopSystemSliceCPUUsage, err := getCPUStat(cgroupsHierarchyCPU, types.SystemSliceCgroupName, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, 0, err
	}
	stopSystemSlice := time.Now().UnixNano()

	stopKubepodsCPUUsage, err := getCPUStat(cgroupsHierarchyCPU, types.DefaultkubepodsCgroupName, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, 0, err
	}
	stopKubepods := time.Now().UnixNano()

	// measure CPU usage outside kubepods with /proc/stats
	stopTotalCPUTime, stopIdleCPUTime, err := readProcStats(err)
	if err != nil {
		return 0, 0, 0, err
	}

	// For more information on CPU usage calculation using /proc/stats, please refer to: https://rosettacode.org/wiki/Linux_CPU_utilization
	// - /proc/stats cpu stats are in given in Jiffies (duration of 1 tick of the system timer interrupt.)
	// So we cannot just divide the diff by the elapsed time measure with time.Now() - nanoseconds.
	// First would need to convert the Jiffies to nanoseconds using USR_HERTZ -> the length of a clock tick.
	// It is easier however, to just calculate the relation between idling and processing CPU time to get the usage during sleep()
	diffTotal := stopTotalCPUTime - startTotalCPUTime
	diffIdle := stopIdleCPUTime - startIdleCPUTime
	log.Debugf("Jiffie diff total: %d | diffIdle: %d", diffTotal, diffIdle)

	idleQuotient := float64(diffIdle) / float64(diffTotal)
	procStatOverallCPUUsage := (1 - idleQuotient) * float64(numCPU)
	log.Debugf("CPU usage from /proc/stat is %.2f percent (%.2f cores of %d)", procStatOverallCPUUsage*100, procStatOverallCPUUsage, numCPU)

	elapsedTimeKubepods := float64(stopKubepods) - float64(startKubepods)
	elapsedTimeSystemSlice := float64(stopSystemSlice) - float64(startSystemSlice)

	systemSliceRelativeCPUUsage := (float64(stopSystemSliceCPUUsage) - float64(startSystemSliceCPUUsage)) / elapsedTimeSystemSlice
	kubepodsRelativeCPUUsage := (float64(stopKubepodsCPUUsage) - float64(startKubepodsCPUUsage)) / elapsedTimeKubepods
	return procStatOverallCPUUsage, systemSliceRelativeCPUUsage, kubepodsRelativeCPUUsage, nil
}

// readProcStats reads from /proc/stat and returns
//   - the total processing CPU time since system start as first return value
//   - the total idle CPU time since system start as second return value
func readProcStats(err error) (uint64, uint64, error) {
	statsCurr, err := linuxproc.ReadStat("/proc/stat")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read from /proc/stat to determine current CPU usage")
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
			statsCurr.CPUStatAll.GuestNice +
			statsCurr.CPUStatAll.Steal +
			// also add idle to make up total
			idleCPUTime

	return totalCPUTime, idleCPUTime, nil
}
