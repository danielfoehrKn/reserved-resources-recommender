package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/containerd/cgroups"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/cpu"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/disk"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/memory"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/types"
	"github.com/dustin/go-humanize"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/pointer"
)

const (
	defaultMemorySafetyMarginAbsolute     = "100Mi"
	defaultCgroupsHierarchyRoot           = "/sys/fs/cgroup"
	defaultContainerdCgroupsHierarchyRoot = "system.slice/containerd.service"
	defaultKubeletCgroupsHierarchyRoot    = "system.slice/kubelet.service"
	defaultKubeletDirectory               = "/var/lib/kubelet"
	defaultContainerdStateDirectory       = "/run/containerd"
	defaultContainerdRootDirectory        = "/var/lib/containerd"
)

var (
	log = logrus.New()
	// kubeletStateDirectory  is the directory that contains the kubelet's state
	// defaults to: /var/lib/kubelet
	kubeletDirectory string
	// containerdStateDirectory is the directory that contains the containerd state directory
	// defaults to: /run/containerd
	containerdStateDirectory string
	// containerdRootDirectory is the directory that contains the containerd root directory
	// defaults to: /var/lib/containerd
	containerdRootDirectory string
	// kubeletConfigPath is the path to the kubelet's configuration file
	// defaults to: /var/lib/kubelet/config/kubelet
	kubeletConfigPath string
	// memorySafetyMarginAbsolute is the additional amount of memory added to the kube-reserved memory compared to what is
	// actually reserved by other processes + kernel
	// this is to make sure the cgroup limit hits before the OS OOM in order to safely prevent a global OOM
	// defaults to 100Mi
	memorySafetyMarginAbsolute resource.Quantity
	// cgroupsHierarchyRoot defines where the root of the cgroup fs is mounted
	// defaults to "/sys/fs/cgroup"
	cgroupsHierarchyRoot string
	// containerdCgroupsRoot defines where the root of the containerd's cgroup fs is mounted under the cgroups hierarchy root
	// defaults to "system.slice/containerd.service"
	containerdCgroupsRoot string
	// kubeletCgroupsRoot defines where the root of the kubelet's cgroup fs is mounted under the cgroups hierarchy root
	// defaults to "system.slice/kubelet.service"
	kubeletCgroupsRoot string
	// period is the measurement period (e.g every 30 seconds).
	// The recommender also uses this time to check the cpu reservation
	period time.Duration
	// enforceRecommendation determines if the recommendations for memory and disk are applied directly to the kubepods/system.slice cgroups
	// the kubelet's configuration is NOT adjusted and might contain conflicting reservations
	enforceRecommendation bool
	// minimumReservedMemory is the minimum amount of memory that will be reserved when enforcing a recommendation
	// Please note, recommended memory reservations can still be lower than that
	minimumReservedMemory resource.Quantity
)

func init() {
	kubeletDirectory = os.Getenv("KUBELET_DIRECTORY")
	containerdStateDirectory = os.Getenv("CONTAINERD_STATE_DIRECTORY")
	containerdRootDirectory = os.Getenv("CONTAINERD_ROOT_DIRECTORY")
	memorySafetyMarginString := os.Getenv("MEMORY_SAFETY_MARGIN_ABSOLUTE")
	cgroupsHierarchyRoot = os.Getenv("CGROUPS_HIERARCHY_ROOT")
	containerdCgroupsRoot = os.Getenv("CGROUPS_CONTAINERD_ROOT")
	kubeletCgroupsRoot = os.Getenv("CGROUPS_KUBELET_ROOT")
	periodString := os.Getenv("PERIOD")
	enforce := os.Getenv("ENFORCE_RECOMMENDATION")
	minReservedMemory := os.Getenv("MINIMUM_RESERVED_MEMORY")

	if len(kubeletDirectory) == 0 {
		kubeletDirectory = defaultKubeletDirectory
	}

	if len(containerdStateDirectory) == 0 {
		containerdStateDirectory = defaultContainerdStateDirectory
	}

	if len(containerdRootDirectory) == 0 {
		containerdRootDirectory = defaultContainerdRootDirectory
	}

	if len(memorySafetyMarginString) == 0 {
		memorySafetyMarginAbsolute = resource.MustParse(defaultMemorySafetyMarginAbsolute)
	} else {
		memorySafetyMarginAbsolute = resource.MustParse(memorySafetyMarginString)
	}

	if len(cgroupsHierarchyRoot) == 0 {
		cgroupsHierarchyRoot = defaultCgroupsHierarchyRoot
	}

	if len(containerdCgroupsRoot) == 0 {
		containerdCgroupsRoot = defaultContainerdCgroupsHierarchyRoot
	}

	if len(kubeletCgroupsRoot) == 0 {
		kubeletCgroupsRoot = defaultKubeletCgroupsHierarchyRoot
	}

	var err error
	if len(enforce) > 0 {
		enforceRecommendation, err = strconv.ParseBool(enforce)
		if err != nil {
			log.Fatalf("The ENFORCE_RECOMMENDATION env variable is invalid: must be boolean: %v", err)
		}
	}

	if len(minReservedMemory) != 0 {
		minimumReservedMemory, err = resource.ParseQuantity(minReservedMemory)
		if err != nil {
			log.Fatalf("The MINIMUM_RESERVED_MEMORY env variable is invalid: %v", err)
		}
	}

	if len(periodString) == 0 {
		period = 20 * time.Second
	} else {
		p, err := time.ParseDuration(periodString)
		if err != nil {
			log.Fatalf("Supplied period is not a valid duration: %v", err)
		}
		period = p
	}
}

func main() {
	log.Infof("Kubelet directory: %s", kubeletDirectory)
	log.Infof("CgroupsV1 hierarchy root: %s", cgroupsHierarchyRoot)
	log.Infof("Recommended memory safety margin: %s", memorySafetyMarginAbsolute.String())
	log.Infof("Minimum reserved memory: %s", minimumReservedMemory.String())
	log.Infof("Period: %s", period.String())
	log.Infof("Enforce recommendation: %v", enforceRecommendation)

	memTotal, _, err := memory.ParseProcMemInfo()
	if err != nil {
		log.Fatalf("fatal -failed to read /proc/meminfo: %v", err)
	}
	log.Infof("Memory capacity: %s", humanize.IBytes(uint64(memTotal.Value())))

	numCPU := int64(runtime.NumCPU())
	log.Infof("CPU cores: %d", numCPU)

	go func() {
		for {
			// we measure the CPU consumption as the average CPU consumption over period/2 amount of time
			if err := recommendCPUReservation(period/2, numCPU); err != nil {
				log.Warnf("error during reconciliation: %v", err)
			}

			fmt.Println("")

			// we measure the CPU consumption as the average CPU consumption over period/2 amount of time
			if err := recommendDiskReservation(containerdRootDirectory, containerdStateDirectory, kubeletDirectory); err != nil {
				log.Warnf("error during reconciliation: %v", err)
			}

			// after the business logic is done, sleep for another period/2
			// the overall time between executions of business logic will be slightly larger than period
			time.Sleep(period / 2)
		}
	}()

	// start a dedicated goroutine for memory reservation + enforcement
	// this should run with a high frequency to be able to effectively protect the system in case of
	// system.slice memory usage spikes
	go func() {
		for {
			if err := recommendMemoryReservation(); err != nil {
				log.Warnf("error during reconciliation: %v", err)
			}

			time.Sleep(5 * time.Second)
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	if err := http.ListenAndServe(":16911", nil); err != nil {
		log.Fatalf("terminating server: %v", err)
	}
	log.Warnf("terminating server....")
}

// recommendReservedMemory recommends and optionally enforces kubelet reserved resources.
// - Memory -> Goal: cgroup limit on the kubepods memory cgroup is set properly preventing a "global" OOM
func recommendMemoryReservation() error {
	targetKubepodsMemoryLimitInBytes, err := memory.RecommendReservedMemory(log, minimumReservedMemory, memorySafetyMarginAbsolute, cgroupsHierarchyRoot, containerdCgroupsRoot, kubeletCgroupsRoot)
	if err != nil {
		return fmt.Errorf("failed to make memory recommendation: %w", err)
	}

	if enforceRecommendation {
		memoryController := cgroups.NewMemory(cgroupsHierarchyRoot)

		if err := memoryController.Update(types.DefaultkubepodsCgroupName, &specs.LinuxResources{
			Memory: &specs.LinuxMemory{
				Limit: pointer.Int64Ptr(targetKubepodsMemoryLimitInBytes.Value()),
			},
		}); err != nil {
			return fmt.Errorf("failed to enforce memory recommendation on the kubepods cgroup: %v", err)
		}
	}
	return nil
}

// recommendCPUReservation recommends and optionally enforces kubelet reserved resources.
// - CPU -> Goal: Give fair amount of CPU shares to kubepods cgroup still leaving enough CPU time for non-pod processes (container runtime, kubelet, ...) to operate.
func recommendCPUReservation(reconciliationPeriod time.Duration, numCPU int64) error {
	targetKubepodsCPUShares, err := cpu.RecommendCPUReservations(log, reconciliationPeriod, cgroupsHierarchyRoot, numCPU)
	if err != nil {
		return fmt.Errorf("failed to make CPU recommendation: %w", err)
	}

	if enforceRecommendation {
		cpuController := cgroups.NewCpu(cgroupsHierarchyRoot)

		shares := uint64(targetKubepodsCPUShares)
		cpuController.Update(types.DefaultkubepodsCgroupName, &specs.LinuxResources{
			CPU: &specs.LinuxCPU{
				Shares: &shares,
			},
		})
	}
	return nil
}

// recommendDiskReservation recommends kubelet reserved resources.
// - Disk -> Goal: Accurate disk reservations allows good scheduling decisions for pods with ephemeral size requests
func recommendDiskReservation(containerdRootDirectory, containerdStateDirectory, kubeletDirectory string) error {
	if err := disk.RecommendDiskReservation(log, containerdRootDirectory, containerdStateDirectory, kubeletDirectory); err != nil {
		return fmt.Errorf("failed to make disk recommendation: %w", err)
	}
	return nil
}
