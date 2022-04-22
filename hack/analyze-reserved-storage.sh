# kubectl exec -ti debugpod-d060239 -- bash -c "chroot /hostroot /bin/bash -c 'dmesg -T'"
#script_location=/Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/hack/analyze-reserved-storage.sh
#kubectl exec -it debugpod-d060239 -- bash -c "chroot /hostroot /bin/bash -c $(cat $script_location)"


#Size of all containers on root disk =
#sizeOf(`/run/containerd` without each `rootfs` dir) #containerd root dir, contains pod working dirs. + other state (pod sandbox state, OCI bundles, containerd state)
#+ sizeOf(`/var/lib/containerd/`) # containerd state dir, contains content-store and snapshotter!
#+ size of logs (`/var/log/pods`)
#+ size of /var/lib/kubelet/pods (excluding CSI attached disk) # contains size of all relevant volumes
#  - includes size of emptyDir volume (on disk, the tmpfs version has  size 0)
#  - does not include hostPath
#  - need to exclude CSI volumes manually
#
#Excluded:
# - size of hostPath volume (not included in kubelet Summary API + cannot be reasonably determined manually)
# - size of network-attached disks (CSI - not on root disk)
# - size of emptyDir with tmpfs (bytes in virtual-memory, not on disk)

# TODO: determine if /var/lib/kubelet is mounted on the root disk or not (first, for Gardener only)

bytesToHumanReadable() {
    local i=${1:-0} d="" s=0 S=("Bytes" "KiB" "MiB" "GiB" "TiB" "PiB" "EiB" "YiB" "ZiB")
    while ((i > 1024 && s < ${#S[@]}-1)); do
        printf -v d ".%02d" $((i % 1024 * 100 / 1024))
        i=$((i / 1024))
        s=$((s + 1))
    done
    echo "$i$d ${S[$s]}"
}

root_disk_partition_name=$(mount|grep ' / ' |cut -d' ' -f 1)
root_disk_partition_size_bytes=$(blockdev --getsize64 $root_disk_partition_name)
root_disk_available_kbytes=$(df $root_disk_partition_name --output=avail | tail -1)
root_disk_used_kbytes=$(df $root_disk_partition_name --output=used | tail -1)
# By default, df displays values in 1-kilobyte blocks
root_disk_available_bytes="$(($root_disk_available_kbytes * 1024))"
root_disk_used_bytes="$(($root_disk_used_kbytes * 1024))"
root_disk_reserved_bytes="$(($root_disk_partition_size_bytes - $root_disk_available_bytes - $root_disk_used_bytes))"

echo "Root disk: $root_disk_partition_name"
size_human=$(bytesToHumanReadable $root_disk_partition_size_bytes)
echo " - Capacity: $size_human"
used_human=$(bytesToHumanReadable $root_disk_used_bytes)
echo " - Used: $used_human"
available_human=$(bytesToHumanReadable $root_disk_available_bytes)
echo " - Available: $available_human"
reserved_human=$(bytesToHumanReadable $root_disk_reserved_bytes)
echo " - Reserved: $reserved_human"

content_store_size_bytes=$(du -sb /var/lib/containerd/io.containerd.content.v1.content/ | awk '{ print $1 }')
# echo "Containerd content store size (images): $content_store_size_bytes"

snapshot_store_size_bytes=$(du -sb /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs | awk '{ print $1 }')
snapshot_store_size_human=$(bytesToHumanReadable $snapshot_store_size_bytes)
echo "Containerd snapshot store size (snapshots including working directories of containers): $snapshot_store_size_human"

# containerd root contains content store and snapshot size
containerd_state_size_bytes=$(du -sb --exclude="rootfs" /run/containerd | awk '{ print $1 }')
containerd_state_size_bytes_human=$(bytesToHumanReadable $containerd_state_size_bytes)
echo "Containerd state size (/run/containerd) without rootfs: $containerd_state_size_bytes_human"

container_logs_size_bytes=$(du -sb /var/log/pods | awk '{ print $1 }')
container_logs_size_human=$(bytesToHumanReadable $container_logs_size_bytes)
echo "Size of container logs: $container_logs_size_human"

# Alternative: use kubelet Summary API to get the log size (downside: for effort, upside: no disk usage)
# totalContainerLogSizeBytesMetrics=$(curl http://localhost:8001/api/v1/nodes/shoot--d060239--dev-seed-gcp-cpu-worker-z1-66bf7-xjzzn/proxy/stats/summary | jq '.pods[].containers[].logs.usedBytes' | awk '{s+=$1} END {printf "%.0f", s}')

# includes all pod volumes (secret, configMpa, projected, ...)
# excludes CSI attached network-disks + tmps-based emptyDir (always 0 size reported) + hostPath mounts are not included in the directory at all
container_volume_size_bytes=$(du -sb --exclude="kubernetes.io~csi" /var/lib/kubelet/pods | awk '{ print $1 }')
container_volume_size_bytes_human=$(bytesToHumanReadable $container_volume_size_bytes)
echo "Size of pod volumes (only on root disk): $container_volume_size_bytes_human"

container_plugins_size_bytes=$(du -sb --exclude="csi" /var/lib/kubelet/plugins | awk '{ print $1 }')
container_plugins_size_bytes_human=$(bytesToHumanReadable $container_plugins_size_bytes)
echo "Size of kubelet plugins: $container_plugins_size_bytes_human"


# Disk_Reservation == disk_space_used_by_non_pods = Capacity - reservation(by filesystem) - available_bytes - disk_use_pods (without content store/only images)
# Logic:
#  - Calculates the used bytes overall using (Capacity - reservation(by filesystem) - available_bytes).
#  - Then subtracts bytes used by pods to get the bytes that must be used by anything but pods.
disk_reservation="$(($root_disk_partition_size_bytes-$root_disk_reserved_bytes-$root_disk_available_bytes-$containerd_state_size_bytes-$container_logs_size_bytes-$container_volume_size_bytes-$container_plugins_size_bytes-$snapshot_store_size_bytes))"

content_store_size_bytes_human=$(bytesToHumanReadable $content_store_size_bytes)
echo "Max. pruneable images (content store): $content_store_size_bytes_human"

disk_reservation_human=$(bytesToHumanReadable $disk_reservation)
echo "Should reserve: $disk_reservation_human"

disk_reservation_comparison_human=$(du -h --total --max-depth=3  --exclude=/proc --exclude=/run/containerd/io.containerd.runtime.v2.task --exclude=/var/lib/kubelet/pods --exclude=/var/lib/kubelet/plugins --exclude=/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs --exclude=/var/log/pods / | tail -n 1 | awk '{ print $1 }')
echo "Should reserve according to direct measurement: $disk_reservation_comparison_human"