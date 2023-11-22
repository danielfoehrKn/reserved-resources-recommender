# measure off and on-cpu time for 60 seconds, then sleep for 60 seconds and repeat.
# introduce ephemeral size limit to not flood the disk (hence will get deleted and restarted when ephemeral size limit is reached)

while :
do
  # only keep the last 1- measurements to not overflow the disk
  for i in {1..10}
  do
    echo "iteration: $i"
    echo "current kubelet processes"
    pgrep kubelet

    array_kubelet_pids=$(pgrep kubelet)
    root_kubelet_pid=$(echo $array_kubelet_pids | cut -d " " -f 1)
    prefix="0::/"
    string=$(cat /proc/$root_kubelet_pid/cgroup)
    cgroup=${string#"$prefix"}

    echo "root kubelet PID: $root_kubelet_pid with cgroup $cgroup"

    # measure off-cpu time foe root kubelet in uninterruptible sleep state for 60 seconds
    # - only measures time spend in kernel, not userspace (kernel stacks only)
    /usr/sbin/offcputime-bpfcc --stack-storage-size 30000 -K --pid $root_kubelet_pid --state 2 -f 60 > kubelet.offcpu_kernel.stacks.$i &
    /usr/sbin/offcputime-bpfcc --stack-storage-size 30000 -U --pid $root_kubelet_pid --state 2 -f 60 > kubelet.offcpu_userspace.stacks.$i &
    /usr/sbin/profile-bpfcc --stack-storage-size 30000 -K --pid $root_kubelet_pid -f 60 > kubelet.oncpu_kernel.stacks.$i &
    /usr/sbin/profile-bpfcc --stack-storage-size 30000 -U --pid $root_kubelet_pid -f 60 > kubelet.oncpu_userspace.stacks.$i &

    time cat /sys/fs/cgroup/memory.pressure > dev/null

    echo "measuring..."
    sleep 60
  done
done
