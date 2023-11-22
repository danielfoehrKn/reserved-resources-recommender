echo -n "Specify the cpu cgroup name: ";
read cgroup;

echo -n "Specify the period in seconds: ";
read period;

while true
do

tstart=$(date +%s%N)
cstart=$(cat /sys/fs/cgroup/cpu/$cgroup/cpuacct.usage)

sleep $period

tstop=$(date +%s%N)
cstop=$(cat /sys/fs/cgroup/cpu/$cgroup/cpuacct.usage)

usage_percentage=$(bc -l <<EOF
($cstop - $cstart) / ($tstop - $tstart) * 100
EOF
)

printf "cgroup $cgroup used %.2f percent CPU over $period seconds. \n" $usage_percentage

done


# For processes own cgroup:
# cgroup=$(awk -F: '$2 == "cpu,cpuacct" { print $3 }' /proc/self/cgroup)