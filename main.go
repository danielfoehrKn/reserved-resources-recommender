package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	systemdDbus "github.com/coreos/go-systemd/v22/dbus"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/cpu"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/kubelet"
	"github.com/danielfoehrkn/better-kube-reserved/pkg/memory"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	log = logrus.New()
	// enforceRecommendation defines if the kubelet config shall be changed and the kubelet process shall be restarted to enact the new kube-reserved
	// default: true
	enforceRecommendation string
	// minThresholdPercent defines the minimum percentage of OS memory available, that triggeres an update & restart of the kubelet
	// This is a mechanism to reduce unnecessary kubelet restarts when there is enough memory available.
	// Example: a value of 0.2 means that only if the OS memory available is in the range of 0 - 20% available, the kubelet
	// reserved-memory should be updated
	minThresholdPercent string
	// minDeltaAbsolute is the minimum absolut difference between kube-reserved memory and the actual available memory.
	// If the difference is greater, the kube-reserved config will be updated and the kubelet restarted.
	// This is to avoid too many kubelet restarts.
	// values must be a resource.Quantity
	minDeltaAbsolute string
)

const (
	kubeletServiceName             = "kubelet.service"
	kubeletServiceMinActiveSeconds = 60.0
)

func init() {
	minDeltaAbsolute = os.Getenv("MIN_DELTA_ABSOLUTE")
	minThresholdPercent = os.Getenv("MIN_THRESHOLD_PERCENT")
	enforceRecommendation = os.Getenv("ENFORCE_RECOMMENDATION")

	if len(enforceRecommendation) == 0 {
		enforceRecommendation = "true"
	}
}

func main() {
	minimumDelta, err := memory.GetMemoryMinimumAbsoluteDelta(minDeltaAbsolute)
	if err != nil {
		log.Fatalf("failed to determine minimum delta: %v", err)
	}

	memTotal, _, err := memory.ParseProcMemInfo()
	if err != nil {
		log.Fatalf("fatal error during reconciliation: %v", err)
	}

	// determine the threshold when the memory reconciliation should act
	// Skip reconciliation, when there is more memory available in the OS than this threshold
	minimumThreshold, err := memory.GetMemoryMinimumAbsoluteThreshold(uint64(memTotal.Value()), minThresholdPercent)
	if err != nil {
		log.Fatalf("failed to determine minimum threshold: %v", err)
	}

	log.Infof("Enforcing recommendations: %v. Minimum threshold is %q and minimum delta is %q", enforceRecommendation == "true", minimumThreshold.String(), minimumDelta.String())

	ctx, controllerCancel := context.WithCancel(context.Background())
	defer controllerCancel()

	systemdConnection, err := systemdDbus.New()
	if err != nil {
		log.Fatalf("failed to init connection with systemd socket: %v", err)
	}
	defer systemdConnection.Close()

	reconciliationPeriod := 20 * time.Second

	go wait.Until(func() {
		reconcileContext, cancel := context.WithTimeout(ctx, reconciliationPeriod)
		defer cancel()

		fmt.Println("----")

		if err := reconcileKubeReserved(reconcileContext, minimumDelta, minimumThreshold, systemdConnection, reconciliationPeriod); err != nil {
			log.Warnf("fatal error during reconciliation: %v", err)
		}
	}, reconciliationPeriod, ctx.Done())

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":16911", nil)
}

// reconcileKubeReserved reconciles the memory reserved settings in the kubelet configuration
// with the actual OS unevictable memory (that should be blocked)
// this makes sure that the cgroup limit on the kubepods memory cgroup is set properly preventing a "global" OOM
func reconcileKubeReserved(ctx context.Context, minimumDelta, minimumThreshold resource.Quantity, systemdConnection *systemdDbus.Conn, reconciliationPeriod time.Duration) error {

	config, err := kubelet.LoadKubeletConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubelet config: %v", err)
	}

	targetReservedMemory, shouldUpdateReservedMemory, reasonMemory, err := memory.ReconcileKubeReservedMemory(log, config, minimumDelta, minimumThreshold)
	if err != nil {
		return fmt.Errorf("failed to to reconcile rreserved memory: %w", err)
	}

	if err := cpu.ReconcileKubeReservedCPU(log, reconciliationPeriod); err != nil {
		return fmt.Errorf("failed to to reconcile rreserved memory: %w", err)
	}

	// if we are not restarting the  kubelet.service, then we can return here
	if enforceRecommendation != "true" {
		return nil
	}

	// CPU currently does not return a recommendation to be enforced
	shouldUpdateReservedCPU := false
	if !shouldUpdateReservedMemory && !shouldUpdateReservedCPU {
		log.Infof("Both memory and CPU reservations should not be updated. Reason: %v", reasonMemory)
		return nil
	}

	if skip, reason := shouldSkipKubeletRestart(log, systemdConnection); skip {
		log.Infof("Skipping kubelet restart: %s", reason)
		return nil
	}

	if err := kubelet.UpdateKubeReserved(*targetReservedMemory, config); err != nil {
		return err
	}

	if err := restartKubelet(ctx, systemdConnection); err != nil {
		return err
	}

	log.Infof("Successfully updated kube-reserved in the kubelet config and restarted the kubelet service")
	return nil
}

// shouldSkipKubeletRestart checks if the reconciliation should be skipped
// If should be skipped, returns true as the first, and a reason as the second argument
func shouldSkipKubeletRestart(log *logrus.Logger, connection *systemdDbus.Conn) (bool, string) {
	// check the last time the kubelet service has been restarted
	// do not allow restarts if < 30 seconds ago
	kubeletActiveDuration, err := kubelet.GetKubeletSystemdUnitActiveDuration(log, connection)
	if err != nil {
		return true, fmt.Sprintf("unable to determine since how long the kubelet systemd service is already running : %v", err)
	}

	if kubeletActiveDuration.Seconds() < kubeletServiceMinActiveSeconds {
		return true, fmt.Sprintf("kubelet is running since less than %f seconds. Skipping", kubeletServiceMinActiveSeconds)
	}

	return false, ""
}

// restartKubelet restarts the kubelet systemd service
func restartKubelet(ctx context.Context, connection *systemdDbus.Conn) error {
	c := make(chan string)

	// mode can be replace, fail, isolate, ignore-dependencies, ignore-requirements.
	_, err := connection.TryRestartUnitContext(ctx, kubeletServiceName, "replace", c)
	if err != nil {
		return fmt.Errorf("failed to restart kubelet: %v", err)
	}

	// wait until kubelet is restarted
	systemdResult := <-c
	if systemdResult != "done" {
		return fmt.Errorf("restarting the kubelet systemd service did not succeed. Status returned: %s", systemdResult)
	}

	return nil
}
