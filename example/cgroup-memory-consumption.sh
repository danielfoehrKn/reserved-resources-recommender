# cgroup name should be provided as first argument
cgroup=$1

if [ -z "$cgroup" ]
then
      # empty. Check on root cgroup
      memory_usage_bytes=$(cat /sys/fs/cgroup/memory/memory.usage_in_bytes)
      total_inactive_file=$(cat /sys/fs/cgroup/memory/memory.stat | grep -i "total_inactive_file" -m 1 | awk '{print $2}')
      echo "root"
else
      memory_usage_bytes=$(cat /sys/fs/cgroup/memory/$cgroup/memory.usage_in_bytes)
      total_inactive_file=$(cat /sys/fs/cgroup/memory/$cgroup/memory.stat | grep -i "total_inactive_file" -m 1 | awk '{print $2}')
fi

working_set_bytes="$((memory_usage_bytes-total_inactive_file))"
working_set_bytes_human=$(numfmt --to=iec-i $working_set_bytes)
total_inactive_file_human=$(numfmt --to=iec-i $total_inactive_file)

echo "cgroup $cgroup uses $working_set_bytes_human (working_set_bytes) that excluded $total_inactive_file_human page cache (inactive_file)"

