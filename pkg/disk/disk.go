package disk

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

const cmdGetRootDiskPartitionName = "cat /proc/1/mounts | grep ' / ' | cut -d ' ' -f 1"
const cmdGetRootDiskPartitionSizeBytes = "blockdev --getsize64"

var (
	metricRootDiskAvailableBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_disk_available_bytes",
		Help: "The available bytes in the filesystem mounted on the root disk",
	})

	metricRootDiskAvailablePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_disk_available_percent",
		Help: "The kubelet reserved memory in percent calculated as (size / root_disk_capacity)",
	})

	metricRootDiskUsedBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_disk_used_bytes",
		Help: "The available bytes in the filesystem mounted on the root disk",
	})

	metricRootDiskUsedPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_disk_used_percent",
		Help: "The kubelet reserved memory in percent calculated as (size / root_disk_capacity)",
	})

	metricRootDiskReservedBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_disk_reserved_bytes",
		Help: "The available bytes in the filesystem mounted on the root disk",
	})

	metricRootDiskReservedPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_disk_reserved_percent",
		Help: "The kubelet reserved memory in percent calculated as (size / root_disk_capacity)",
	})

	metricContainerdSnapshotSizeBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_containerd_overlayfs_snapshotter_size_bytes",
		Help: "The size of the overlayfs snapshotter",
	})

	metricContainerdSnapshotSizePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_containerd_overlayfs_snapshotter_size_percent",
		Help: "The size of the overlayfs snapshotter as (size / root_disk_capacity)",
	})

	metricContainerdStateSizeBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_containerd_state_size_bytes",
		Help: "The size of the containerd state directory",
	})

	metricContainerdStateSizePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_containerd_state_size_percent",
		Help: "The size of the containerd state directory as (size / root_disk_capacity)",
	})

	metricContainerdContentStoreSizeBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_containerd_content_store_size_bytes",
		Help: "The size of the containerd content store",
	})

	metricContainerdContentStoreSizePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_containerd_content_store_size_percent",
		Help: "The size of the containerd content store as (size / root_disk_capacity)",
	})

	metricPodLogsSizeBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_pod_logs_size",
		Help: "The size of the pod logs as (size / root_disk_capacity)",
	})

	metricPodLogsSizePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_pod_logs_size_percent",
		Help: "The size of pod logs",
	})

	metricPodVolumesSizeBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_pod_volumes_size_bytes",
		Help: "The size of pod volumes (includes containerd snapshots & the size of the container working directories. Excludes CSI volumes, hostPath, tmpfs emptyDir)",
	})

	metricPodVolumesSizePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "node_pod_volumes_size_percent",
		Help: "The size of pod volumes (includes containerd snapshots & the size of the container working directories. Excludes CSI volumes, hostPath, tmpfs emptyDir) as (size / root_disk_capacity)",
	})

	metricKubeletPluginSizeBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_plugin_size_bytes",
		Help: "The size of kubelet plugins",
	})

	metricKubeletPluginSizePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_plugin_size_percent",
		Help: "The size of kubelet plugins as (size / root_disk_capacity)",
	})


	metricKubeletTargetReservedDiskBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_target_reserved_disk_bytes",
		Help: "The recommended reserved bytes for the kubelet disk reservation",
	})

	metricKubeletTargetReservedDiskPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubelet_target_reserved_disk_percent",
		Help: "The recommended reserved bytes for the kubelet disk reservation as (size / root_disk_capacity)",
	})
)


// RecommendDiskReservation recommends kubelet disk reservations based on actual disk usage.
// Disk_Reservation == disk_space_used_by_non_pods =
// Capacity - fs_reservation(determined by filesystem)
// - available_bytes
// - containers disk size on root disk (excluding content store (not unpacked images))
//
// Size of all containers on root disk =
//   sizeOf(`/run/containerd` without each `rootfs` dir) #containerd root dir, contains pod working dirs. + other state (pod sandbox state, OCI bundles, containerd state)
// + sizeOf(`/var/lib/containerd/`) # containerd state dir, contains content-store and snapshotter!
// + size of logs (`/var/log/pods`)
// + size of kubelet plugins (`/var/lib/kubelet/plugins`)
// + size of /var/lib/kubelet/pods # contains size of all relevant volumes
//  - includes size of emptyDir volume (on disk, the tmpfs version has size 0)
//
// Excluded:
// - size of hostPath volume (not included in kubelet Summary API + cannot be reasonably determined manually)
// - size of network-attached disks (CSI - not on root disk). Excluded from /var/lib/kubelet/pods
// - size of emptyDir with tmpfs (bytes in virtual-memory, not on disk)
// Caveat:
//  - Containerd must be used as the container runtime
//  - assumes directories mounted under `/` are mounted on the root disk (this is not necessarely the case - e.g the kubelet directory /var/lib/kubelet could be mounted on a non-root disk in which case the recommendation is incorrect)
//  - hostPath volumes are not considered. You have to manually check the disk usage for pods mounting host path volumes and adjust the recommendation accordingly.
func RecommendDiskReservation(log *logrus.Logger, containerdRootDirectory string, containerdStateDirectory string, kubeletDirectory string) error {
	// We are only interested in the mounts as seen from the host (we do not want to access them)
	// However, the container this go application executes in is in a dedicated mount namespace, hence we see different mounts than the host.
	// As a trick, we can setup the container (or the pod in k8s) to run in the host PID namespace.
	// This allows the recommender to inspect the mounts of a process known to run in the host mount namespace (such as PID 1)
	// Example:
	// $ nerdctl exec 373d872b2180 mount | grep ' / ' | cut -d ' ' -f 1
	// overlay
	// $ nerdctl exec 373d872b2180 cat /proc/1/mounts | grep ' / ' | cut -d ' ' -f 1
	// /dev/nvme0n1p3
	// How to setup
	//  - k8s: on pod resource set `hostPID: true`
	//  - using nerdctl: `nerdctl run --pid=host`
	rootDiskPartitionNameBytes, err := exec.Command("sh", "-c", cmdGetRootDiskPartitionName).Output()
	if err != nil {
		return err
	}
	rootDiskPartitionName := sanitize(string(rootDiskPartitionNameBytes))
	log.Debugf("Root disk partition name: %s", rootDiskPartitionName)

	// requires mounting the host devices from the /dev directory
	// to access the device (owned by root user), we need to run the container as privileged
	// - k8s: securityContext.privileged: true
	// - nerdctl run --privileged
	capacity, err := exec.Command("sh", "-c", fmt.Sprintf("%s %s", cmdGetRootDiskPartitionSizeBytes, rootDiskPartitionName)).Output()
	if err != nil {
		return err
	}

	rootDiskPartitionCapacityBytes, err := strconv.ParseInt(sanitize(string(capacity)), 10, 64)
	if err != nil {
		return err
	}

	log.Debugf("Root disk partition size in bytes: %s", humanize.IBytes(uint64(rootDiskPartitionCapacityBytes)))

	available, err := exec.Command("sh", "-c", fmt.Sprintf("df %s | tr -s ' ' | cut -d\" \" -f 4  | tail -1", rootDiskPartitionName)).Output()
	if err != nil {
		return err
	}

	rootDiskPartitionAvailableKiloBytes, err := strconv.ParseInt(sanitize(string(available)), 10, 64)
	if err != nil {
		return err
	}

	rootDiskPartitionAvailableBytes := rootDiskPartitionAvailableKiloBytes * 1024
	log.Debugf("Root disk partition available bytes: %s", humanize.IBytes(uint64(rootDiskPartitionAvailableBytes)))

	used, err := exec.Command("sh", "-c", fmt.Sprintf("df %s | tr -s ' ' | cut -d\" \" -f 3  | tail -1", rootDiskPartitionName)).Output()
	if err != nil {
		return err
	}

	rootDiskPartitionUsedKiloBytes, err := strconv.ParseInt(sanitize(string(used)), 10, 64)
	if err != nil {
		return err
	}

	rootDiskPartitionUsedBytes := rootDiskPartitionUsedKiloBytes * 1024
	log.Debugf("Root disk partition used bytes: %s", humanize.IBytes(uint64(rootDiskPartitionUsedBytes)))

	rootDiskPartitionReservedBytes :=  rootDiskPartitionCapacityBytes - rootDiskPartitionAvailableBytes - rootDiskPartitionUsedBytes
	log.Debugf("Root disk partition reserved bytes: %s", humanize.IBytes(uint64(rootDiskPartitionReservedBytes)))

	contentStore, err := exec.Command("sh", "-c", "du -sb /var/lib/containerd/io.containerd.content.v1.content/ | awk '{ print $1 }'").Output()
	if err != nil {
		return err
	}

	containerdContentStoreBytes, err := strconv.ParseInt(sanitize(string(contentStore)), 10, 64)
	if err != nil {
		return err
	}

	log.Debugf("Containerd content store bytes: %s", humanize.IBytes(uint64(containerdContentStoreBytes)))

	snapshotStore, err := exec.Command("sh", "-c", "du -sb /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs | awk '{ print $1 }'").Output()
	if err != nil {
		return err
	}

	containerdSnapshotStoreBytes, err := strconv.ParseInt(sanitize(string(snapshotStore)), 10, 64)
	if err != nil {
		return err
	}

	log.Debugf("Containerd snapshot store size (snapshots including working directories of containers): %s", humanize.IBytes(uint64(containerdSnapshotStoreBytes)))

	containerdState, err := exec.Command("sh", "-c", "du -sb --exclude=\"rootfs\" /run/containerd | awk '{ print $1 }'").Output()
	if err != nil {
		return err
	}

	containerdStateBytes, err := strconv.ParseInt(sanitize(string(containerdState)), 10, 64)
	if err != nil {
		return err
	}

	log.Debugf("Containerd state size (/run/containerd) without rootfs: %s", humanize.IBytes(uint64(containerdStateBytes)))

	logs, err := exec.Command("sh", "-c", "du -sb /var/log/pods | awk '{ print $1 }'").Output()
	if err != nil {
		return err
	}

	podLogsBytes, err := strconv.ParseInt(sanitize(string(logs)), 10, 64)
	if err != nil {
		return err
	}

	log.Debugf("Size of container logs: %s", humanize.IBytes(uint64(podLogsBytes)))

	volumeSize, err := exec.Command("sh", "-c", "du -sb --exclude=\"kubernetes.io~csi\" /var/lib/kubelet/pods | awk '{ print $1 }'").Output()
	if err != nil {
		return err
	}

	podVolumeSizeBytes, err := strconv.ParseInt(sanitize(string(volumeSize)), 10, 64)
	if err != nil {
		return err
	}

	log.Debugf("Size of pod volumes (only on root disk): %s", humanize.IBytes(uint64(podVolumeSizeBytes)))

	pluginsSize, err := exec.Command("sh", "-c", "du -sb --exclude=\"csi\" /var/lib/kubelet/plugins | awk '{ print $1 }'").Output()
	if err != nil {
		return err
	}

	kubeletPluginsSizeBytes, err := strconv.ParseInt(sanitize(string(pluginsSize)), 10, 64)
	if err != nil {
		return err
	}

	log.Debugf("Size of kubelet plugins: %s", humanize.IBytes(uint64(kubeletPluginsSizeBytes)))

	diskReservationRecommendation := rootDiskPartitionCapacityBytes - rootDiskPartitionReservedBytes - rootDiskPartitionAvailableBytes - containerdStateBytes - podLogsBytes - podVolumeSizeBytes - kubeletPluginsSizeBytes - containerdSnapshotStoreBytes

	log.Debugf("Disk reservation recommendation: %s", humanize.IBytes(uint64(diskReservationRecommendation)))

	logRecommendation(
		rootDiskPartitionName,
		humanize.IBytes(uint64(rootDiskPartitionCapacityBytes)),
		humanize.IBytes(uint64(rootDiskPartitionAvailableBytes)),
		int64(math.Round(float64(rootDiskPartitionAvailableBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(rootDiskPartitionUsedBytes)),
		int64(math.Round(float64(rootDiskPartitionUsedBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(rootDiskPartitionReservedBytes)),
		int64(math.Round(float64(rootDiskPartitionReservedBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(containerdSnapshotStoreBytes)),
		int64(math.Round(float64(containerdSnapshotStoreBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(containerdStateBytes)),
		int64(math.Round(float64(containerdStateBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(containerdContentStoreBytes)),
		int64(math.Round(float64(containerdContentStoreBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(podLogsBytes)),
		int64(math.Round(float64(podLogsBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(podVolumeSizeBytes)),
		int64(math.Round(float64(podVolumeSizeBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(kubeletPluginsSizeBytes)),
		int64(math.Round(float64(kubeletPluginsSizeBytes)/float64(rootDiskPartitionCapacityBytes)*100)),
		humanize.IBytes(uint64(diskReservationRecommendation)),
		diskReservationRecommendation,
		int64(math.Round(float64(diskReservationRecommendation)/float64(rootDiskPartitionCapacityBytes)*100)),
	)

	// record metrics
	metricRootDiskAvailableBytes.Set(float64(rootDiskPartitionAvailableBytes))
	metricRootDiskAvailablePercent.Set(float64(rootDiskPartitionAvailableBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricRootDiskUsedBytes.Set(float64(rootDiskPartitionUsedBytes))
	metricRootDiskUsedPercent.Set(float64(rootDiskPartitionUsedBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricRootDiskReservedBytes.Set(float64(rootDiskPartitionReservedBytes))
	metricRootDiskReservedPercent.Set(float64(rootDiskPartitionReservedBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricContainerdSnapshotSizeBytes.Set(float64(containerdSnapshotStoreBytes))
	metricContainerdSnapshotSizePercent.Set(float64(containerdSnapshotStoreBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricContainerdStateSizeBytes.Set(float64(containerdStateBytes))
	metricContainerdStateSizePercent.Set(float64(containerdStateBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricContainerdContentStoreSizeBytes.Set(float64(containerdContentStoreBytes))
	metricContainerdContentStoreSizePercent.Set(float64(containerdContentStoreBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricPodLogsSizeBytes.Set(float64(podLogsBytes))
	metricPodLogsSizePercent.Set(float64(podLogsBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricPodVolumesSizeBytes.Set(float64(podVolumeSizeBytes))
	metricPodVolumesSizePercent.Set(float64(podVolumeSizeBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricKubeletPluginSizeBytes.Set(float64(kubeletPluginsSizeBytes))
	metricKubeletPluginSizePercent.Set(float64(kubeletPluginsSizeBytes)/float64(rootDiskPartitionCapacityBytes)*100)
	metricKubeletTargetReservedDiskBytes.Set(float64(diskReservationRecommendation))
	metricKubeletTargetReservedDiskPercent.Set(float64(diskReservationRecommendation)/float64(rootDiskPartitionCapacityBytes)*100)
	return nil
}

func sanitize(s string) string {
	return strings.ReplaceAll(s, "\n", "")
}

func logRecommendation(
	rootDiskName string,
	rootDiskCapacity string,
	rootDiskAvailable string,
	rootDiskAvailablePercentTotal int64,
	rootDiskUsed string,
	rootDiskUsedPercentTotal int64,
	rootDiskReserved string,
	rootDiskReservedPercentTotal int64,
	containerdSnapshotStoreSize string,
	containerdSnapshotStoreSizePercentTotal int64,
	containerdStateSize string,
	containerdStateSizePercentTotal int64,
	containerdContentStoreSize string,
	containerdContentStoreSizePercentTotal int64,
	podLogsSize string,
	podLogsSizePercentTotal int64,
	podVolumesSize string,
	podVolumesSizePercentTotal int64,
	kubeletPluginSize string,
	kubeletPluginSizePercentTotal int64,
	targetReservedDisk string,
	targetReservedDiskPrecise int64,
	targetReservedDiskPercentTotal int64) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Disk Metric", "Value"})

	t.AppendRows([]table.Row{
		{"Root disk", rootDiskName},
		{"Capacity", rootDiskCapacity},
		{"Available", fmt.Sprintf("%s (%d%%)", rootDiskAvailable, rootDiskAvailablePercentTotal)},
		{"Used (Capacity - Available)", fmt.Sprintf("%s (%d%%)", rootDiskUsed, rootDiskUsedPercentTotal)},
		{"Filesystem reserved", fmt.Sprintf("%s (%d%%)", rootDiskReserved, rootDiskReservedPercentTotal)},
		{"Size of containerd snapshot store (/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs)", fmt.Sprintf("%s (%d%%)", containerdSnapshotStoreSize, containerdSnapshotStoreSizePercentTotal)},
		{"Size of containerd state (/run/containerd)", fmt.Sprintf("%s (%d%%)", containerdStateSize, containerdStateSizePercentTotal)},
		{"Size of containerd content store (/var/lib/containerd/io.containerd.content.v1.content)", fmt.Sprintf("%s (%d%%)", containerdContentStoreSize, containerdContentStoreSizePercentTotal)},
		{"Size of container logs (/var/log/pods)", fmt.Sprintf("%s (%d%%)", podLogsSize, podLogsSizePercentTotal)},
		{"Size of pod volumes (/var/lib/kubelet/pods, excluding CSI, hostPath, tmpfs emptyDir)", fmt.Sprintf("%s (%d%%)", podVolumesSize, podVolumesSizePercentTotal)},
		{"Size of kubelet plugins (/var/lib/kubelet/plugins)", fmt.Sprintf("%s (%d%%)", kubeletPluginSize, kubeletPluginSizePercentTotal)},
	})

	t.AppendSeparator()
	t.AppendRow(table.Row{"RECOMMENDATION", fmt.Sprintf("%s (%d bytes, %d%%)", targetReservedDisk, targetReservedDiskPrecise, targetReservedDiskPercentTotal)})
	t.Render()
}

