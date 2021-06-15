package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	linuxproc "github.com/c9s/goprocinfo/linux"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
)

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
)

const (
	defaultMinThresholdPercent = "0.3"
	defaultMinDeltaAbsolute    = "100Mi"
)

func init() {
	minDeltaAbsolute = os.Getenv("MIN_DELTA_ABSOLUTE")
	minThresholdPercent = os.Getenv("MIN_THRESHOLD_PERCENT")
}

func main() {
	if _, err := os.Stat("/proc/meminfo"); os.IsNotExist(err) {
		log.Fatal("Unable to set reserved resources. Cannot determine available memory because /proc/meminfo could not be found.")
	}

	// meminfo values are given in kiB/kibibytes (1024 bytes) (even though given as "kb")
	meminfo, err := linuxproc.ReadMemInfo("/proc/meminfo")
	if err != nil {
		log.Fatalf("failed to read file: /proc/meminfo: %v", err)
	}

	if meminfo.MemTotal == 0 {
		log.Fatalf("unable to calculate threshold values as /proc/meminfo MemTotal is not set")
	}

	minimumDelta, err := getMinimumAbsoluteDelta()
	if err != nil {
		log.Fatalf("failed to determine minimum delta: %v", err)
	}

	minimumThreshold, err := getMinimumAbsoluteThreshold(meminfo.MemTotal)
	if err != nil {
		log.Fatalf("failed to determine minimum threshold: %v", err)
	}

	log.Infof("Minimum threshold is %q and minimum delta is %q", minimumThreshold.String(), minimumDelta.String())

	ctx, controllerCancel := context.WithCancel(context.Background())
	defer controllerCancel()

	go wait.Until(func() {
		if err := reconcileKubeReserved(minimumThreshold, minimumDelta); err != nil {
			log.Warnf("fatal error during reconciliation: %v", err)
		}
	}, 10*time.Second, ctx.Done())
}

func reconcileKubeReserved(minimumThreshold, minimumDelta resource.Quantity) error {
	log.Infof("Starting kube-reserved reconciliation")

	// meminfo values are given in kiB/kibibytes (1024 bytes) (even though given as "kb")
	meminfo, err := linuxproc.ReadMemInfo("/proc/meminfo")
	if err != nil {
		return fmt.Errorf("failed to read file: /proc/meminfo: %v", err)
	}

	// MemAvailable
	// An estimate of how much memory is available for starting new applications, without swapping.
	// Calculated from MemFree, SReclaimable, the size of the file LRU lists, and t
	// he low watermarks in each zone. The estimate takes into account that the system needs
	// some page cache to function well, and that not all reclaimable slab will be reclaimable,
	// due to items being in use.

	// for sake of simplicity, expect that "MemAvailable" field is available
	// alternatively, the available memory (before swapping) could also be calculated from other values in /proc/meminfo.
	// see here: https://unix.stackexchange.com/questions/261247/how-can-i-get-the-amount-of-available-memory-portably-across-distributions
	if meminfo.MemAvailable == 0 {
		return fmt.Errorf("MemAvailable field in /proc/meminfo is not set. Please make sure that your Linux kernel includes this commit: https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/commit/?id=34e431b0a")
	}

	memAvailable, err := resource.ParseQuantity(fmt.Sprintf("%dKi", meminfo.MemAvailable))
	if err != nil {
		return fmt.Errorf("failed to parse MemAvailable field in /proc/meminf (%q) as resource quantity: %v", meminfo.MemAvailable, err)
	}

	// check if memoryAvailable has fallen below threshold where action needs to be taken
	diffAvailableThreshold := memAvailable
	memAvailable.Sub(minimumThreshold)
	if diffAvailableThreshold.Value() < 0 {
		log.Infof("Available memory of %s does not fall below threshold of %s. Do nothing.", memAvailable.String(), minimumThreshold.String())
		return nil
	}

	config, err := loadKubeletConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubelet config: %v", err)
	}

	kubeReservedMemory, err := getKubeReservedMemory(*config)
	if err != nil {
		return fmt.Errorf("failed to parse kube-reserved memory as resource quantity: %v", err)
	}

	log.Infof("Available memory (%q) is %s and kube-reserved memory is %s", "MemAvailable", memAvailable.String(), kubeReservedMemory.String())

	switch kubeReservedMemory.Cmp(memAvailable) {
	case -1:
		// kube-reserved is less than available memory
		// kube-reserved needs to be increased
		// TODO: check threshold
		log.Infof("kube-reserved should be increased")

	case 1:
		// kube-reserved is greater than available memory
		// kube-reserved needs to be decreased
		log.Infof("kube-reserved should be decreased")
		break
	default:
		// kube-reserved is equal to the available memory. Do nothing.
		return nil
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
		minThresholdPercent = defaultMinDeltaAbsolute
	}

	return resource.ParseQuantity(minDeltaAbsolute)
}

// getCurrentKubeReservedMemory reads the kubelet configuration expected in the default location for Gardener nodes
// and extracts the kube reserved memory.available
// returns the the kubeReserved.memory or 0 if not set
// returns an error if the kubelet configuration could not be found / marshalled
func getKubeReservedMemory(config KubeletConfiguration) (resource.Quantity, error) {
	mem, ok := config.KubeReserved["memory"]
	if !ok {
		// currently not set in config. Defaulted by kubelet to 100Mi
		return resource.MustParse("100Mi"), nil
	}

	// parse memory (can be BinarySI or DecimalSI)
	return resource.ParseQuantity(mem)
}

// TODO: overwrite via env variable
const gardenerKubeletFilepath = "/var/lib/kubelet/config/kubelet"

// loadKubeletConfig loads the kubeconfig file from the default location for gardener nodes
func loadKubeletConfig() (*KubeletConfiguration, error) {
	if _, err := os.Stat(gardenerKubeletFilepath); err != nil {
		return nil, err
	}

	bytes, err := ioutil.ReadFile(gardenerKubeletFilepath)
	if err != nil {
		return nil, err
	}

	config := &KubeletConfiguration{}
	if len(bytes) == 0 {
		return config, nil
	}

	err = yaml.Unmarshal(bytes, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal kubelet config from %s: %w", gardenerKubeletFilepath, err)
	}
	return config, nil
}
