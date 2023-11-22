# mkdir -p /sys/fs/cgroup/memory/system.slice/containerd.service/shims
mkdir -p /sys/fs/cgroup/memory/system.slice/containerd-shims

PID_containerd_deamon=$(systemctl show --property MainPID --value  containerd.service)

if [ -z "$PID_containerd_deamon" ]; then
    echo "could not determine contianerd PID"
    return
fi

#  NOTE: No explicit move necessary from one cgroup to another  (just write to new cgroup)
#  Within a hierarchy, a process can be a member of exactly one
  #       cgroup.  Writing a process's PID to a cgroup.procs file
  #       automatically removes it from the cgroup of which it was
  #       previously a member.
for pid in $(cat /sys/fs/cgroup/memory/system.slice/containerd.service/cgroup.procs) ; do
  if [ "$pid" == $PID_containerd_deamon ]; then
      echo "Skipping containerd daemon PID " + $PID_containerd_deamon
      continue
  fi
  # echo "$pid"
  echo "$pid" > /sys/fs/cgroup/memory/system.slice/containerd-shims/cgroup.procs;
done


