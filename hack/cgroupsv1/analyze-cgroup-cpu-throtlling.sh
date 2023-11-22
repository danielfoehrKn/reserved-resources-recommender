#!/bin/sh

echo -n "Specify the cpu cgroup name: ";
read cgroup;

# Should be used when execing into a container
echo "checking in /sys/fs/cgroup/cpuacct/$cgroup"

cd /sys/fs/cgroup

while true
do

echo ""
echo ""

tstart=$(date +%s%N)
# nanoseconds the cgroup was actually executing on CPU
cstart=$(cat /sys/fs/cgroup/cpuacct/$cgroup/cpuacct.usage)

#nr_periods 41861
#nr_throttled 670
#throttled_time 334571311017  --> Hint: calculated as `nr_throttled` *`cpu.cfs_period_us`
total_periods_start=$(cat /sys/fs/cgroup/cpuacct/$cgroup/cpu.stat | awk 'FNR == 1 {print $2}')
throttled_periods_start=$(cat /sys/fs/cgroup/cpuacct/$cgroup/cpu.stat | awk 'FNR == 2 {print $2}')
throttled_time_start=$(cat /sys/fs/cgroup/cpuacct/$cgroup/cpu.stat | awk 'FNR == 3 {print $2}')


sleep 1

tstop=$(date +%s%N)

cstop=$(cat /sys/fs/cgroup/cpuacct/$cgroup/cpuacct.usage)
total_periods_stop=$(cat /sys/fs/cgroup/cpuacct/$cgroup/cpu.stat | awk 'FNR == 1 {print $2}')
throttled_periods_stop=$(cat /sys/fs/cgroup/cpuacct/$cgroup/cpu.stat | awk 'FNR == 2 {print $2}')
throttled_time_stop=$(cat /sys/fs/cgroup/cpuacct/$cgroup/cpu.stat | awk 'FNR == 3 {print $2}')

# First, convert nanoseconds to seconds
# Meaning: We take a sample between t0 and t1 (1 second elapsed)
# - Use 1 second of CPU time during a 1 second period = we used 1 core!
# - Use 2 seconds during a 1 second period = 2 CPU cores
cpu_usage_seconds_total=$(awk -v var1=$cstart -v var2=$cstop 'BEGIN { print  ( (var2 - var1) / 1000000000 ) }') # division by 1000000000 to convert nano second to second

# throttled time during 1 second period
# process waits to be executed again :(
throttled_time_seconds=$(awk -v var1=$throttled_time_start -v var2=$throttled_time_stop 'BEGIN { print  ( (var2 - var1) / 1000000000 ) }')

# throttled time (waiting) vs. CPU usage time
# e.g to execute for 1.05445s, it had to wait in total due to throttling: 1.65718
throttle_quotient=$(awk -v var1=$throttled_time_seconds -v var2=$cpu_usage_seconds_total 'BEGIN { print  ( var1 / var2 ) }')

# total periods the threads in the cgroup where running
num_used_periods=$(awk -v var1=$total_periods_start -v var2=$total_periods_stop 'BEGIN { print  ( var2 - var1 ) }')

# Example: 0.101395 s cpu_usage_seconds_total in 10 periods m
#  -> means: while it executed in 10 periods, it only executed for a bit in each of those (not taking the whole 100ms) for a total of 101ms (1 full period)
execution_time=$(awk -v var1=$cpu_usage_seconds_total -v var2=$throttled_time_seconds 'BEGIN { print  ( var2 + var1 ) }')

# Amount of throttled periods of 100ms during the 1 second period (== 10 , then throttled for the equivalent of one core => would need 1 core more)
num_throttled_periods=$(awk -v var1=$throttled_periods_start -v var2=$throttled_periods_stop 'BEGIN { print  ( var2 - var1 ) }')

# Percentage of throttled periods
# Calculation: diff_throttled_periods(start, stop) / diff_total_periods(start, stop)
throttled_percentage=$(awk -v var1=$throttled_periods_start -v var2=$throttled_periods_stop -v var3=$total_periods_start -v var4=$total_periods_stop 'BEGIN { print  ( (var2 - var1) / (var4 - var3) ) * 100 }')

total_throttled_percentage=$(awk -v var1=$throttled_periods_stop -v var2=$total_periods_stop 'BEGIN { print  ( var1 / var2 ) }')

echo "Total processing time during $num_used_periods periods:  $execution_time (usage + throttled time)"
echo "rate(cpu_usage_seconds_total[1s]): $cpu_usage_seconds_total  (1 = one core during 1 second period)"
echo "rate(throttled_time_seconds[1s]): $throttled_time_seconds (throttled_time_stop($throttled_time_stop) - throttled_time_start($throttled_time_start)"
echo "rate(throttled_periods[1s]) $num_throttled_periods had some throttling (total periods executing within 1s interval: $num_used_periods)"
echo "rate(throttled_periods_percentage[1s]): $throttled_percentage% (100% = all periods (100 ms) within one second interval had some throttling. Does not mean threads could not execute)"
echo "since birth: percentage of throttled periods: $total_throttled_percentage (nr_periods_throttled($throttled_periods_stop) / nr_periods($total_periods_stop)"
echo "throttle_quotient for 1s interval (throttled_time_seconds / cpu_usage_seconds_total):  $throttle_quotient"

done






#echo "CPU Metrics directly from the cgroup fs at 1s resolution"
#echo
#
#cd /sys/fs/cgroup || exit 1
#
#while true; do
#  sleep 1 &
#  awk 'FILENAME == "cpuacct/cpuacct.usage" {printf "cpu_usage_seconds_total{} %s\n", $1/1e9}
#       FILENAME == "cpu/cpu.stat"          {printf "cpu_stat_%s{} %s\n", $1, $2}' \
#    cpuacct/cpuacct.usage \
#    cpu/cpu.stat
#  wait
#done \
#| awk '/cpu_usage_seconds_total/ {if (last != 0) printf "cpu_usage_seconds_total_rate{} %s\n", $NF - last; last=$NF}
#       /cpu_stat_nr_periods/     {periods_n_1=periods_n;     periods_n=$NF}
#       /cpu_stat_nr_throttled/   {throttled_n_1=throttled_n; throttled_n=$NF
#                                  if (periods_n_1 != 0)
#                                    printf "cpu_cfs_throttled_percent{} %s\n\n",
#                                      (throttled_n - throttled_n_1) / (periods_n - periods_n_1)}'
#



