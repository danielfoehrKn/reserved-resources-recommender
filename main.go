package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/danielfoehrkn/better-kube-reserved/pkg/cpu"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/memory"
	"github.com/dustin/go-humanize"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	defaultMemorySafetyMarginAbsolute = "100Mi"
	defaultCgroupsHierarchyRoot       = "/sys/fs/cgroup"
	// TODO: required for the disk-based reservation recommendation
	defaultKubeletDirectory = "/var/lib/kubelet/"
)

var (
	log = logrus.New()
	// kubeletStateDirectory  is the directory that contains the kubelet's state
	// defaults to: /var/lib/kubelet/
	kubeletDirectory string
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
	// period is the measurement period (e.g every 30 seconds).
	// The recommender also uses this time to check the cpu reservation
	period time.Duration
)

func init() {
	kubeletDirectory = os.Getenv("KUBELET_DIRECTORY")
	memorySafetyMarginString := os.Getenv("MEMORY_SAFETY_MARGIN_ABSOLUTE")
	cgroupsHierarchyRoot = os.Getenv("CGROUPS_HIERARCHY_ROOT")
	periodString := os.Getenv("PERIOD")

	if len(kubeletDirectory) == 0 {
		kubeletDirectory = defaultKubeletDirectory
	}

	if len(memorySafetyMarginString) == 0 {
		memorySafetyMarginAbsolute = resource.MustParse(defaultMemorySafetyMarginAbsolute)
	} else {
		memorySafetyMarginAbsolute = resource.MustParse(memorySafetyMarginString)
	}

	if len(cgroupsHierarchyRoot) == 0 {
		cgroupsHierarchyRoot = defaultCgroupsHierarchyRoot
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
	log.Infof("Period: %s", period.String())

	memTotal, _, err := memory.ParseProcMemInfo()
	if err != nil {
		log.Fatalf("fatal -failed to read /proc/meminfo: %v", err)
	}
	log.Infof("Memory capacity: %s", humanize.IBytes(uint64(memTotal.Value())))

	numCPU := int64(runtime.NumCPU())
	log.Infof("CPU cores: %d", numCPU)

	ctx, controllerCancel := context.WithCancel(context.Background())
	defer controllerCancel()


	go wait.Until(func() {
		if err := recommendReservedResources(period, numCPU); err != nil {
			log.Warnf("error during reconciliation: %v", err)
		}
	}, period * 2, ctx.Done())

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":16911", nil)
	log.Warnf("terminating server....")
}

// recommendReservedResources recommends kubelet reserved resources. To enforce the limits, the kubelet configuration
// has to be updated and the kubelet process must be re-started.
// - Memory -> Goal: cgroup limit on the kubepods memory cgroup is set properly preventing a "global" OOM
// - CPU -> Goal: Give fair amount of CPU shares to kubepods cgroup still leaving enough CPU time for non-pod processes (container runtime, kubelet, ...) to operate.
// - Disk -> Goal: Accurate disk reservations allows good scheduling decisions for pods with ephemeral size requests
func recommendReservedResources(reconciliationPeriod time.Duration, numCPU int64) error {
	// does not return a recommendation when CPU resource reservations should be updated
	// this is because CPU reservations are not as critical as memory reservations (100 % CPU usage does not cause necessarily any harm)
	if err := cpu.RecommendReservedCPU(log, reconciliationPeriod, cgroupsHierarchyRoot, numCPU); err != nil {
		return fmt.Errorf("failed to to reconcile reserved cpu: %w", err)
	}

	fmt.Println("")

	if err := memory.RecommendReservedMemory(log, memorySafetyMarginAbsolute); err != nil {
		return fmt.Errorf("failed to to reconcile reserved memory: %w", err)
	}

	return nil
}
