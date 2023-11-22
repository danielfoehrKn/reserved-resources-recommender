# Usage
# 1) find pod for cgroup path using nerdctl (when inspecting the cgroup filesystem)
#   - interesting_cgroup_path=<cgroup under kubepods>
# 2) find pod for mountpoint (when checking mounts)
#    - mount | grep volume-subpaths
       #/dev/nvme6n1 on /var/lib/kubelet/pods/39f81d56-39d4-46d3-af80-826d97966a5a/volume-subpaths/pv-shoot-garden-aws-1479e4b1-e4ec-43b2-b751-46bbc036d7c2/prometheus/4 type ext4 (rw,relatime)
       # interesting_cgroup_path=pod39f81d56-39d4-46d3-af80-826d97966a5a

interesting_cgroup_path=$1
for i in $(nerdctl -n k8s.io ps -q -a)
do
  occurence=$(nerdctl -n k8s.io inspect $i --mode=native | grep $interesting_cgroup_path -c)
  if [ $occurence -gt 0 ]; then
      echo "Found in container with id $i. Use 'nerdctl -n k8s.io inspect $i --mode=native'"
  fi
done