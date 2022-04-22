# find pod for cgroup path using nerdctl
interesting_cgroup_path=pod7e44ee88-1e65-4a16-8cdf-9a90c819180e
#interesting_cgroup_path=pod115d8276-f508-4006-afc2-ebf2e6b7f072/ee8247ebce1c2d2a855e27e5de33b61c385f92ecbfb32387aabc9eb56c47b7e7
for i in $(nerdctl ps -q)
do
  occurence=$(nerdctl inspect $i --mode=native | grep $interesting_cgroup_path -c)
  if [ $occurence -gt 0 ]; then
      echo "Found in container with id $i. Use 'nerdctl inspect $i --mode=native'"
  fi
done