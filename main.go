package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/danielfoehrkn/better-kube-reserved/pkg/cpu"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/kubelet"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/memory"
	resources "github.com/danielfoehrkn/resource-reservations-grpc/pkg/proto/gen/resource-reservations"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	defaultGardenerKubeletFilePath = "/var/lib/kubelet/config/kubelet"
	defaultMemorySafetyMarginAbsolute = "100Mi"
	defaultCgroupsHierarchyRoot = "/sys/fs/cgroup"
)

var (
	log = logrus.New()
	// enforceRecommendation defines if the kubelet config shall be changed and the kubelet process shall be restarted to enact the new kube-reserved
	// default: true
	enforceRecommendation string
	// minMemoryThresholdPercent defines the minimum percentage of OS memory available, that triggeres an update & restart of the kubelet
	// This is a mechanism to reduce unnecessary kubelet restarts when there is enough memory available.
	// Example: a value of 0.2 means that only if the OS memory available is in the range of 0 - 20% available, the kubelet
	// reserved-memory should be updated
	minMemoryThresholdPercent string
	// minMemoryDeltaAbsolute is the minimum absolut difference between kube-reserved memory and the actual available memory.
	// If the difference is greater, the kube-reserved config will be updated.
	// values must be a resource.Quantity
	minMemoryDeltaAbsolute string
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
)

func init() {
	minMemoryDeltaAbsolute = os.Getenv("MIN_DELTA_ABSOLUTE")
	minMemoryThresholdPercent = os.Getenv("MIN_THRESHOLD_PERCENT")
	enforceRecommendation = os.Getenv("ENFORCE_RECOMMENDATION")
	kubeletDirectory = os.Getenv("KUBELET_DIRECTORY")
	kubeletConfigPath = os.Getenv("KUBELET_CONFIG_PATH")
	memorySafetyMarginString := os.Getenv("MEMORY_SAFETY_MARGIN_ABSOLUTE")
	cgroupsHierarchyRoot = os.Getenv("CGROUPS_HIERARCHY_ROOT")

	if len(enforceRecommendation) == 0 {
		enforceRecommendation = "true"
	}

	if len(kubeletDirectory) == 0 {
		kubeletDirectory = "/var/lib/kubelet/"
	}

	if len(kubeletConfigPath) == 0 {
		kubeletConfigPath = defaultGardenerKubeletFilePath
	}

	if len(memorySafetyMarginString) == 0 {
		memorySafetyMarginAbsolute = resource.MustParse(defaultMemorySafetyMarginAbsolute)
	} else {
		memorySafetyMarginAbsolute = resource.MustParse(memorySafetyMarginString)
	}

	if len(cgroupsHierarchyRoot) == 0 {
		cgroupsHierarchyRoot = defaultCgroupsHierarchyRoot
	}
}

func main() {
	minimumDelta, err := memory.GetMemoryMinimumAbsoluteDelta(minMemoryDeltaAbsolute)
	if err != nil {
		log.Fatalf("failed to determine minimum delta: %v", err)
	}

	memTotal, _, err := memory.ParseProcMemInfo()
	if err != nil {
		log.Fatalf("fatal error during reconciliation: %v", err)
	}

	// determine the threshold when the memory reconciliation should act
	// Skip reconciliation, when there is more memory available in the OS than this threshold
	minimumThreshold, err := memory.GetMemoryMinimumAbsoluteThreshold(uint64(memTotal.Value()), minMemoryThresholdPercent)
	if err != nil {
		log.Fatalf("failed to determine minimum threshold: %v", err)
	}

	log.Infof("Enforcing recommendations: %v. Minimum threshold is %q and minimum delta is %q", enforceRecommendation == "true", minimumThreshold.String(), minimumDelta.String())

	var (
		client         resources.ResourceReservationsClient
		grpcConnection *grpc.ClientConn
	)
	if enforceRecommendation == "true" {
		// check that the feature --dyanmic-resource-reservations is enabled by verifying
		// that the Unix socket exists (this is brittle and assumes inside knowledge)
		dynamicResourceReservationsSocket := fmt.Sprintf("%s/%s", kubeletDirectory, "dynamic-resource-reservations/kubelet.sock")
		if _, err := os.Stat(dynamicResourceReservationsSocket); err != nil {
			log.Fatalln(fmt.Errorf("failed to find the dynamic resource reservations Unix socket at %q. Make sure that the kubelet config directory is correct and that the kubelet flag --dynamic-resource-reservations is activated", err))
		}

		dialer := func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", addr)
		}

		// Get grpc client to communicate with kubelet's dynamic resource reservations grpc server
		grpcConnection, err = grpc.Dial(dynamicResourceReservationsSocket, grpc.WithInsecure(), grpc.WithContextDialer(dialer))
		if err != nil {
			log.Infof("failed to connect to kubelet's dynamic resource reservations grpc server: %v", err)
			os.Exit(1)
		}

		client = resources.NewResourceReservationsClient(grpcConnection)
		log.Infof("created resource reservations grpc client")
	}

	if grpcConnection != nil {
		defer grpcConnection.Close()
	}

	ctx, controllerCancel := context.WithCancel(context.Background())
	defer controllerCancel()

	reconciliationPeriod := 20 * time.Second

	go wait.Until(func() {
		reconcileContext, cancel := context.WithTimeout(ctx, reconciliationPeriod)
		defer cancel()

		fmt.Println("----")

		if err := reconcileKubeReserved(reconcileContext, client, minimumDelta, minimumThreshold, reconciliationPeriod); err != nil {
			log.Warnf("error during reconciliation: %v", err)
		}
	}, reconciliationPeriod, ctx.Done())

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":16911", nil)
	log.Warnf("terminating server....")
}

// reconcileKubeReserved reconciles the memory reserved settings in the kubelet configuration
// with the actual OS unevictable memory (that should be blocked)
// this makes sure that the cgroup limit on the kubepods memory cgroup is set properly preventing a "global" OOM
func reconcileKubeReserved(ctx context.Context, client resources.ResourceReservationsClient, minimumDelta, minimumThreshold resource.Quantity, reconciliationPeriod time.Duration) error {
	config, err := kubelet.LoadKubeletConfig(kubeletConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load kubelet config: %v", err)
	}

	systemReserved, kubeReserved, err := kubelet.GetResourceReservations(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to retrieve current resource reservations from the kubelet: %v", err)
	}

	targetReservedMemory, shouldUpdateReservedMemory, reasonMemory, err := memory.ReconcileKubeReservedMemory(log, systemReserved[string(corev1.ResourceMemory)], kubeReserved[string(corev1.ResourceMemory)], minimumDelta, minimumThreshold, memorySafetyMarginAbsolute)
	if err != nil {
		return fmt.Errorf("failed to to reconcile reserved memory: %w", err)
	}

	log.Infof("----")

	// does not return a recommendation when CPU resource reservations should be updated
	// this is because CPU reservations are not as critical as memory reservations (100 % CPU usage does not cause necessarily any harm)
	targetKubeReservedCPU, err := cpu.ReconcileKubeReservedCPU(log, reconciliationPeriod, cgroupsHierarchyRoot)
	if err != nil {
		return fmt.Errorf("failed to to reconcile reserved cpu: %w", err)
	}

	if enforceRecommendation != "true" {
		return nil
	}

	if !shouldUpdateReservedMemory {
		log.Infof("Memory reservations should not be updated. Reason: %v", reasonMemory)
		return nil
	}

	// use the grpc API to update the resource reservations
	if err := kubelet.UpdateResourceReservations(ctx, client, *targetReservedMemory, *targetKubeReservedCPU); err != nil {
		return fmt.Errorf("failed to update kubelet's resource reservations: %w", err)
	}

	// to keep the kubelet configuration file in sync with the recommendation provided over grpc
	// this way, even if the kubelet process is restarted, the right kube-reserved settings are set
	if err := kubelet.UpdateKubeReservedInConfigFile(*targetReservedMemory, config, kubeletConfigPath); err != nil {
		return err
	}

	log.Infof("Successfully updated kubelet's resource reservations")
	return nil
}
