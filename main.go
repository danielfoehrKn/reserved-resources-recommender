package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"strconv"
	"time"

	linuxproc "github.com/c9s/goprocinfo/linux"
	"github.com/containerd/cgroups"
	cgroupstatsv1 "github.com/containerd/cgroups/stats/v1"
	systemdDbus "github.com/coreos/go-systemd/v22/dbus"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeletv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/yaml"
)

// This is a PoC!

// Problem statement:
//  - The amount of CPU & memory the kubelet, the container runtime and OS processes consume cannot be predicted
//    before node creation
//  - Consumption depends on a) number of pods deployed b) the kind of workload deployed (in pods)
//  - Given these two variables are unknown, any prediction for kube- & system-reserved will
//    either under ("global" OOM) or over-reserve resources (costs)
//  - Most problematic:
//     a) Under-reserving Memory
//     	- even though calculated based on machine-type, the common formula by GKE & Azure do not reserve enough
//     	- will cause a "global" OOM instead of hitting cgroup memory limit (cgroup-OOM) or kubelet eviction is triggered
//       (eviction only triggered if kubepods cgroup is close to its memory limit)
//      - "global" OOM can kill any process in the OS based on oom_score (also e.g container runtime & kubelet)
//     b) Over reserving CPU shares
//      - The CPU shares are calculated relative to the other cgroups, so over reserving CPU shares for kubepods
//        slice means under reserving CPU shares for system.slice
//		- Kubelet, container runtime & OS processes do not get enough CPU time -> node stability threatened

// Idea: reconcile the limit of the kubepods cgroup via kube-reserved to not exceed the OS available
// memory (prevent "global" OOM). Instead the safer option, kubelet eviction or cgroup-level OOM should be triggered.
// The kubepods cgroup memory limit is indirectly updated by updating the kubelet kube-reserved
// and restarting its systemdDbus unit.

// Terminology

// - /proc/meminfo "MemAvailable" is the total OS memory available without going into swapping
// Calculated from MemFree, SReclaimable, the size of the file LRU lists, and the
// low watermarks in each zone. The estimate takes into account that the system needs
// some page cache to function well, and that not all reclaimable slab will be reclaimable,
// due to items being in use.
// - /proc/meminfo "MemTotal" total physical memory available on the system.

// working_set_bytes is the "working set memory" of a process / cgroup
// this is the memory in active use that cannot be evicted in case of resource contention
// calculated on cgroups as memory.usage_in_bytes - memory.stat "total_inactive_file"
// "total_inactive_file" is the amount of Linux Page cache on the inactive LRU list (e.g, files not recently accessed)


// Calculation
// 	kube-reserved memory = MemTotal - MemAvailable  - working_set_bytes for kubepods cgroup
// 	The kube-reserved should stay rather constant, unless processes outside kubepods need more memory (e.g OS daemons, container runtime, kubelet)
// 	--> if the kube-reserved changed > MIN_DELTA_ABSOLUTE (e.g 100 Mi), then the kubelet config is adjusted and the kubelet is restarted
//  --> this can happen if there are now many more pods deployed or workload that causes more memory consumption for the container runtime / kubelet
// Limit on kubepods cgroup (set by kubelet) = Node Capacity - kube-reserved (+ eviction.hard?)

// Unfortunately, just measuring the memory_working_set_bytes on the system.slice cgroup and reserving that does not work.
// It does not account for kernel memory and processes in no / other cgroups.

// Example:
// MemTotal: 10 Gi, working_set_bytes kubepods: 7 Gi, MemAvailable: 1 Gi
// we know: everything else consumes 2 Gi = Total 10 - available 1 - working_set 7)
// Hence, kube reserved = 2 Gi
// cgroup limit kubepods = Node Capacity(10 Gi) - 2Gi = 8 Gi

// WHY THIS SHOULD BE IN THE KUBELET
// - No need to restart kubelet (API requests!!)
// - PoC relies on systemdDbus (how does it work on non-systemdDbus OS?)
// - General problem, every managed Kubernetes provider will have the same problem (do not know what workload runs on it)

var (
	log = logrus.New()
	// minDeltaAbsolute is the minimum absolut difference between kube-reserved memory and the actual available memory.
	// If the difference is greater, the kube-reserved config will be updated and the kubelet restart
	// This is to avoid too many kubelet restarts
	// values must be a resource.Quantity
	minDeltaAbsolute string
	// minThresholdPercent defines the minimum percentage of OS memory available, that triggeres an update & restart of the kubelet
	// This is a mechanism to reduce unnecessary kubelet restarts when there is enough memory available.
	// Example: a value of 0.2 means that only if the OS memory available is in the range of 0 - 20% available, the kubelet
	// reserved-memory should be updated
	minThresholdPercent string
	// safetyMargin is the additional amount of memory added to the kube-reserved memory compared to what is
	// actually reserved by other processes + kernel
	// this is to make sure the cgroup limit hits before the OS OOM
	safetyMargin = resource.MustParse(defaultMinDeltaAbsolute)
)

const (
 	gardenerKubeletFilepath = "/var/lib/kubelet/config/kubelet"
	// PoC: assumes kubepods memory controller mounted at /sys/fs/cgroups/memory/kubepods
	// Should be based on cgroup driver (systemdDbus: kubepods.slice, cgroups: kubepods) and from kubelet config
	memoryCgroupRoot = "/sys/fs/cgroup"
	// or should right away deploy as pod?
	kubepodsMemoryCgroupName = "kubepods"

	// defaultMinThresholdPercent     = "0.3"
	defaultMinThresholdPercent     = "0.9"
	defaultMinDeltaAbsolute        = "100Mi"
	kubeletServiceName             = "kubelet.service"
	kubeletServiceMinActiveSeconds = 30.0
)

func init() {
	minDeltaAbsolute = os.Getenv("MIN_DELTA_ABSOLUTE")
	minThresholdPercent = os.Getenv("MIN_THRESHOLD_PERCENT")
}

func main() {
	minimumDelta, err := getMinimumAbsoluteDelta()
	if err != nil {
		log.Fatalf("failed to determine minimum delta: %v", err)
	}

	memTotal, _, err := parseProcMemInfo()
	if err != nil {
		log.Fatalf("fatal error during reconciliation: %v", err)
	}

	minimumThreshold, err := getMinimumAbsoluteThreshold(uint64(memTotal.Value()))
	if err != nil {
		log.Fatalf("failed to determine minimum threshold: %v", err)
	}

	log.Infof("Minimum threshold is %q and minimum delta is %q", minimumThreshold.String(), minimumDelta.String())

	ctx, controllerCancel := context.WithCancel(context.Background())
	defer controllerCancel()

	systemdConnection, err := systemdDbus.New()
	if err != nil {
		log.Fatalf("failed to init connection with systemdDbus socket: %v", err)
	}
	defer systemdConnection.Close()

	wait.Until(func() {
		memTotal, memAvailable, err := parseProcMemInfo()
		if err != nil {
			log.Fatalf("fatal error during reconciliation: %v", err)
		}

		if skip, reason := skipReconciliation(memAvailable, minimumThreshold, systemdConnection); skip {
			log.Infof("Skipping reconciliation: %s", reason)
			return
		}

		if err := reconcileKubeReserved(memTotal, memAvailable, minimumThreshold, minimumDelta, systemdConnection); err != nil {
			log.Warnf("fatal error during reconciliation: %v", err)
		}
	}, 10*time.Second, ctx.Done())
}

// skipReconciliation checks if the reconciliation should be skipped
// If should be skipped, returns true as the first, and a reason as the second argument
func skipReconciliation(memAvailable, minimumThreshold resource.Quantity, connection *systemdDbus.Conn) (bool, string) {
	// check the last time the kubelet service has been restarted
	// do not allow restarts if < 20 seconds ago
	kubeletActiveDuration, err := getSystemdUnitActiveDuration(kubeletServiceName, connection)
	if err != nil {
		return true, fmt.Sprintf("unable to determine since how long the kubelet systemdDbus service is already running : %v", err)
	}

	if kubeletActiveDuration.Seconds() < kubeletServiceMinActiveSeconds {
		return true, fmt.Sprintf("kubelet is running since less than %f seconds. Skipping", kubeletServiceMinActiveSeconds)
	}

	// check if available memory has fallen below threshold where action needs to be taken
	diffAvailableThreshold := memAvailable
	diffAvailableThreshold.Sub(minimumThreshold)
	if diffAvailableThreshold.Value() > 0 {
		return true, fmt.Sprintf("Available memory of %s does not fall below threshold of %s. Do nothing.", memAvailable.String(), minimumThreshold.String())
	}

	return false, ""
}

// getKubepodsMemoryWorkingSet reads the kubepods memory cgroup and calculates
// the working set bytes
func getKubepodsMemoryWorkingSet() (resource.Quantity, error) {
	// mocked out for now
	memoryController := cgroups.NewMemory(memoryCgroupRoot)

	stats := &cgroupstatsv1.Metrics{}
	if err := memoryController.Stat(kubepodsMemoryCgroupName, stats); err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to read memory stats for kubepods cgroup: %v", err)
	}

	memoryWorkingSetBytes := stats.Memory.Usage.Usage  - stats.Memory.InactiveAnon
	return resource.ParseQuantity(fmt.Sprintf("%d", memoryWorkingSetBytes))
}

// parseProcMemInfo parses /proc/meminfo and returns MemTotal, MemAvailable or an error
func parseProcMemInfo() (resource.Quantity, resource.Quantity, error) {
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

	return memAvailable, memTotal, nil
}

// reconcileKubeReserved reconciles the memory reserved settings in the kubelet configuration
// with the actual OS unevictable memory (that should be blocked)
// this makes sure that the cgroup limit on the kubepods memory cgroup is set properly preventing a "global" OOM
func reconcileKubeReserved(memTotal, memAvailable, minimumThreshold, minimumDelta resource.Quantity, connection *systemdDbus.Conn) error {
	log.Infof("Starting kube-reserved reconciliation")

	// TODO: need to get the cgroup stats for kubepods
	kubepodsWorkingSetBytes, err := getKubepodsMemoryWorkingSet()
	if err != nil {
		return err
	}

	config, err := loadKubeletConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubelet config: %v", err)
	}

	kubeReservedMemory, err := getKubeReservedMemory(*config)
	if err != nil {
		return fmt.Errorf("failed to parse kube-reserved memory as resource quantity: %v", err)
	}

	systemReservedMemory, err := getSystemReservedMemory(*config)
	if err != nil {
		return fmt.Errorf("failed to parse system-reserved memory as resource quantity: %v", err)
	}

	// total reserved memory of the kubelet is system + kube-reserved
	oldReservedMemory := kubeReservedMemory
	oldReservedMemory.Add(systemReservedMemory)


	// Calculation: target reserved memory = MemTotal
	// - MemAvailable
	// - working_set_bytes of kubepods cgroup
	// + safety margin
	targetReservedMemory := memTotal
	targetReservedMemory.Sub(memAvailable)
	targetReservedMemory.Sub(kubepodsWorkingSetBytes)
	targetReservedMemory.Add(safetyMargin)

	// difference old reserved settings - target reserved
	diffOldMinusNewReserved := oldReservedMemory
	diffOldMinusNewReserved.Sub(targetReservedMemory)

	log.Infof("Total memory: %s; \n" +
		"Available memory: %q; \n" +
		"Working set memory kubepods cgroup: %q; \n" +
		"Target reserved memory: %q; \n" +
		"Current reserved memory: %q (kube: %q, system: %q).",
		memTotal.String(),
		memAvailable.String(),
		kubepodsWorkingSetBytes.String(),
		targetReservedMemory.String(),
		oldReservedMemory.String(),
		kubeReservedMemory.String(),
		systemReservedMemory.String())


	// check if diffOldMinusNewReserved > threshold
	// TODO: corner case if less than safety margin available memory --> then this will never trigger and cause OOM
	if math.Abs(float64(diffOldMinusNewReserved.Value())) <  float64(minimumDelta.Value()) {
		log.Infof("SKIPPING: Delta of new reserved memory (%q) and old reserved memory (%q) is %q (minimum delta: %q).", targetReservedMemory.String(), oldReservedMemory.String(), diffOldMinusNewReserved.String(), minimumDelta.String())
		return nil
	}

	// kube-reserved = reserved memory - system-reserved
	targetKubeReserved := targetReservedMemory // includes safety margin
	targetKubeReserved.Sub(systemReservedMemory)

	if err := updateKubeReserved(targetKubeReserved, config); err != nil {
		return err
	}

	action := "DECREASED"
	if diffOldMinusNewReserved.Value() > 0 {
		action = "INCREASED"
	}

	log.Infof("Successfully %q kube-reserved from %q to %q (including safety margin of %q)", action, kubeReservedMemory.String(), targetKubeReserved.String(), safetyMargin.String())
	return restartKubelet(err, connection)
}

// restartKubelet restarts the kubelet systemdDbus service
func restartKubelet(err error, connection *systemdDbus.Conn) error {
	c := make(chan string)

	// mode can be replace, fail, isolate, ignore-dependencies, ignore-requirements.
	_, err = connection.TryRestartUnit(kubeletServiceName, "replace", c)
	if err != nil {
		return fmt.Errorf("failed to restart kubelet: %v", err)
	}

	// wait until kubelet is restarted
	systemdResult := <-c
	if systemdResult != "done" {
		return fmt.Errorf("restarting the kubelet systemdDbus service did not succeed. Status returned: %s", systemdResult)
	}

	log.Infof("Successfully restarted the kubelet")
	return nil
}

// getSystemdUnitActiveDuration takes a systemdDbus connection and a unit name
// returns the duration since when the given service is running
func getSystemdUnitActiveDuration(unit string, connection *systemdDbus.Conn) (*time.Duration, error) {
	property, err := connection.GetUnitProperty(unit, "ActiveEnterTimestamp")
	if err != nil {
		return nil, err
	}

	if property == nil {
		return nil, fmt.Errorf("cannot determine last start time of kuebelet systemdDbus service. Property %q not found", "ActiveEnterTimestamp")
	}

	stringProperty := fmt.Sprintf("%v", property.Value.Value())
	activeEnterTimestamp, err := strconv.ParseInt(stringProperty, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot determine last start time of kuebelet systemdDbus service. Property %q cannot be parsed as int64", "ActiveEnterTimestamp")
	}

	activeEnterTimestampUTC := time.Unix(0, activeEnterTimestamp * 1000)
	duration := time.Now().Sub(activeEnterTimestampUTC)
	log.Debugf("kubelet is running since %q", duration.String())
	return &duration, nil
}

// updateKubeReserved calculates the new kube reserved memory and updates the kubelet config file
func updateKubeReserved(newReservedMemory resource.Quantity, config *kubeletv1beta1.KubeletConfiguration) error {
	config.KubeReserved[string(corev1.ResourceMemory)] = newReservedMemory.String()
	if err := updateKubeletConfig(config); err != nil {
		return err
	}
	return nil
}

// updateKubeletConfig writes an update to the kubelet configuration file
func updateKubeletConfig(config *kubeletv1beta1.KubeletConfiguration) error {
	out, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to write updated kubelet config: %w", err)
	}

	f, err := os.Create(gardenerKubeletFilepath)
	if err != nil {
		return fmt.Errorf("failed to open kubelet config file: %v", err)
	}

	_, err = f.Write(out)
	if err != nil {
		return fmt.Errorf("failed to write kubelet config file: %v", err)
	}

	return nil
}

func getMinimumAbsoluteThreshold(memTotal uint64) (resource.Quantity, error) {
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

	return resource.ParseQuantity(fmt.Sprintf("%dKi", value))
}

func getMinimumAbsoluteDelta() (resource.Quantity, error) {
	if len(minDeltaAbsolute) == 0 {
		minDeltaAbsolute = defaultMinDeltaAbsolute
	}

	return resource.ParseQuantity(minDeltaAbsolute)
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


// loadKubeletConfig loads the kubeconfig file from the default location for gardener nodes
func loadKubeletConfig() (*kubeletv1beta1.KubeletConfiguration, error) {
	if _, err := os.Stat(gardenerKubeletFilepath); err != nil {
		return nil, err
	}

	bytes, err := ioutil.ReadFile(gardenerKubeletFilepath)
	if err != nil {
		return nil, err
	}

	if len(bytes) == 0 {
		return nil, fmt.Errorf("kubelet config not found at %q", gardenerKubeletFilepath)
	}

	config := kubeletv1beta1.KubeletConfiguration{}
	err = yaml.Unmarshal(bytes, &config)
	if err != nil {
		return nil, fmt.Errorf("error decoding kubelet config: %w", err)
	}

	return &config, nil
}

