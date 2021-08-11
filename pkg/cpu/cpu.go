package cpu

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

const (
	// cgroupRoot assumes kubepods cpu controller mounted at /sys/fs/cgroups/cpu/kubepods
	// Should be based on cgroup driver (systemdDbus: kubepods.slice, cgroups: kubepods) and from kubelet config
	cgroupRoot          = "/sys/fs/cgroup/cpu"
	cgroupKubepods      = "kubepods"
	cgroupSystemSlice   = "system.slice"
	cgroupStatCPUShares = "cpu.shares"
	cgroupStatCPUUsage  = "cpuacct.usage"
	defaultMinimumReservedCPU = 80
)

var (
	// TODO: 3 Grafana Dashboards
	//  - node_cgroup_system_slice_min_guaranteed_cpu vs. node_cgroup_system_slice_cpu_percent
	//  - kubelet_target_reserved_cpu vs. kubelet_current_reserved_cpu
	// - How much CPU the workload is using: node_cgroup_kubepods_cpu_percent
	// TODO: Dashboard per node!
	metricSystemSliceMinGuaranteedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_cgroup_system_slice_min_guaranteed_cpu",
		Help: "The minimum guaranteed CPU time of the system.slice cgroup based on the cpu.shares (1024)",
	})

	metricCores = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_num_cpu_cores",
		Help: "The number of CPU cores of this node",
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

	metricCurrentReservedCPU = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_current_reserved_cpu",
		Help: "The current kubelet reserved CPU",
	})
)

// ReconcileKubeReservedCPU reconciles the current kube-reserved CPU settings against
// the actual (target) CPU consumption of system.slice
func ReconcileKubeReservedCPU(log *logrus.Logger, reconciliationPeriod time.Duration) error {
	numCPU := int64(runtime.NumCPU())

	systemSliceCPUShares, err := getCPUStat(cgroupSystemSlice, cgroupStatCPUShares)
	if err != nil {
		return err
	}

	kubepodsCPUShares, err := getCPUStat(cgroupKubepods, cgroupStatCPUShares)
	if err != nil {
		return err
	}

	// System.slice's relative CPU time for all cores = (Active cgroup CPU shares) / (sum of all possible CPU shares of the cgroup SIBLINGS)
	// System.slice's relative CPU time for one core = Total CPU time for all cores * number of cores
	var systemSliceGuaranteedCPUTime float32
	systemSliceGuaranteedCPUTime = (float32(systemSliceCPUShares) / (float32(systemSliceCPUShares) + float32(kubepodsCPUShares))) * float32(numCPU)
	systemSliceGuaranteedCPUTimePercent := systemSliceGuaranteedCPUTime * 100

	log.Infof("system.slice is guaranteed %.2f percent CPU time on this %d core machine. \n", systemSliceGuaranteedCPUTimePercent, numCPU)

	systemSliceCPUTime, kubepodsCPUTime, err := measureCPUUsage(log, reconciliationPeriod)
	if err != nil {
		return fmt.Errorf("failed to measure relative CPU time: %w", err)
	}

	systemSliceCPUTimePercent := systemSliceCPUTime * 100
	kubepodsCPUTimePercent := kubepodsCPUTime * 100

	log.Infof("CPU usage: system.slice: %.2f percent. kubepods: %.2f percent", systemSliceCPUTimePercent, kubepodsCPUTimePercent)

	// calculate if system.slice gets enough CPU time
	// if it does not, kube-reserved CPU has to be increased in order to decrease the CPU shares
	// on the kubepods cgroup
	kubepodsTargetCPUShares := int64(((float64(systemSliceCPUShares) * float64(numCPU)) / systemSliceCPUTime) - float64(systemSliceCPUShares))
	log.Infof("kubepods CPU shares. Current: %d | Target: %d", kubepodsCPUShares, kubepodsTargetCPUShares)

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
		targetKubeReservedCPU = defaultMinimumReservedCPU
		log.Infof("defaulting reserved CPU to minimum")
	}

	// no need to read the kubelet configuration to get the current reserved CPU
	// it can be deduced by looking at the kubepods cpu.shares
	currentKubeReservedCPU := totalCPUShares - kubepodsCPUShares

	action := "INCREASE"
	if currentKubeReservedCPU - targetKubeReservedCPU > 0 {
		action = "DECREASE"
	}

	log.Infof("RECOMMENDATION: %s reserved CPU from %d m to %d m", action, currentKubeReservedCPU, targetKubeReservedCPU)

	// record prometheus metrics
	metricCores.Set(float64(numCPU))
	metricSystemSliceMinGuaranteedCPU.Set(math.Round(float64(systemSliceGuaranteedCPUTimePercent)))
	metricKubepodsCurrentCPUConsumptionPercent.Set(math.Round(systemSliceCPUTimePercent))
	metricSystemSliceCurrentCPUConsumptionPercent.Set(math.Round(kubepodsCPUTimePercent))
	metricCurrentReservedCPU.Set(float64(currentKubeReservedCPU))
	metricTargetReservedCPU.Set(float64(targetKubeReservedCPU))

	return nil
}

// getCPUStat reads a numerical cgroup stat from the cgroupFS
func getCPUStat(cgroup, cpuStat string) (int64, error) {
	// unfortunately, github.com/containerd/cgroups only supports rudimentary CPU cgroup stats.
	// have to read it myself from the filesystem
	value, err := ReadUint(filepath.Join(cgroupRoot, cgroup, cpuStat))
	if err != nil {
		return 0, err
	}
	return int64(value), nil
}

// measureCPUUsage measures the relative CPU usage of the kubepods and system.slice cgroup over a period of time
// compared to the overall CPU time of all CPU cores
// a return value of 1.1 means that the cgroup has used 110% of the CPU time of one core
func measureCPUUsage(log *logrus.Logger,reconciliationPeriod time.Duration) (float64, float64, error) {
	start := time.Now().UnixNano()
	log.Debugf("Start time: %v", start)

	startSystemSliceCPUUsage, err := getCPUStat(cgroupSystemSlice, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, err
	}
	log.Debugf("Start system: %v", startSystemSliceCPUUsage)


	startKubepodsCPUUsage, err := getCPUStat(cgroupKubepods, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, err
	}

	log.Debugf("Start kubepods: %v", startKubepodsCPUUsage)


	duration := reconciliationPeriod / 2
	time.Sleep(duration)

	stopSystemSliceCPUUsage, err := getCPUStat(cgroupSystemSlice, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, err
	}
	log.Debugf("Stop system: %v", stopSystemSliceCPUUsage)

	stopKubepodsCPUUsage, err := getCPUStat(cgroupKubepods, cgroupStatCPUUsage)
	if err != nil {
		return 0, 0, err
	}
	log.Debugf("Stop kubepods: %v", stopKubepodsCPUUsage)

	stop := time.Now().UnixNano()
	log.Debugf("Stop time: %v", stop)


	systemSliceRelativeCPUUsage := (float64(stopSystemSliceCPUUsage) - float64(startSystemSliceCPUUsage)) / (float64(stop) - float64(start))
	kubepodsRelativeCPUUsage := (float64(stopKubepodsCPUUsage) - float64(startKubepodsCPUUsage)) / (float64(stop) - float64(start))
	return systemSliceRelativeCPUUsage, kubepodsRelativeCPUUsage, nil
}

