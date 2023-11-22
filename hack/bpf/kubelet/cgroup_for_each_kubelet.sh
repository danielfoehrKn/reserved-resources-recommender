array_kubelet_pids=$(pgrep kubelet)
for x in $array_kubelet_pids;
do
  prefix="0::/"
  string=$(cat /proc/$x/cgroup)
  cgroup=${string#"$prefix"}
  echo "Kubelet($x): $cgroup"
done