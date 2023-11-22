echo -n "Specify the cpu cgroup name: ";
read cgroup;

echo -n "Specify the period in seconds: ";
read period;

while true
do

tstart=$(date +%s%N)
# Note the usage_usec value is measured in milliseconds, unlike the value returned by the cpuacct.usage file (in cgroupsv2), which is in nanoseconds.
cstart=$(cat /sys/fs/cgroup/$cgroup/cpu.stat | grep usage_usec | awk '{ print( $2)}')
cstart_nanoseconds=$(bc -l <<EOF
  $cstart * 1000
EOF
)

sleep $period

tstop=$(date +%s%N)
cstop=$(cat /sys/fs/cgroup/$cgroup/cpu.stat | grep usage_usec | awk '{ print( $2)}')

cstop_nanoseconds=$(bc -l <<EOF
  $cstop * 1000
EOF
)

usage_percentage=$(bc -l <<EOF
  ($cstop_nanoseconds - $cstart_nanoseconds) / ($tstop - $tstart) * 100
EOF
)

printf "cgroup $cgroup used %.2f percent CPU over $period seconds. \n" $usage_percentage

done