echo -n "Specify the period in seconds: ";
read period;

cgroup=system.slice
num_cpu_cores=$(nproc --all)

while true
do

# calculation of minimally guaranteed CPU time using CPU shares in case of resource contention
# in reality, on Gardener nodes, only kubepods and system.slice use any significant CPU time
system_slice_cpu_shares=$(cat /sys/fs/cgroup/cpu/$cgroup/cpu.shares) # 1024
kubepods_cpu_shares=$(cat /sys/fs/cgroup/cpu/kubepods/cpu.shares) # 1966 -> 2048 total shares - 80m reserved = 1968

# Total CPU time for all cores = (Active cgroup CPU shares) / (sum of all possible CPU shares of the cgroup SIBLINGS)
# Total CPU time of one core = Total CPU time for all cores * number of cores
system_slice_min_cpu_time=$(bc -l <<EOF
 (($system_slice_cpu_shares) / ($system_slice_cpu_shares + $kubepods_cpu_shares)) * $num_cpu_cores
EOF
)

system_slice_min_cpu_time_percent=$(bc -l <<EOF
 $system_slice_min_cpu_time * 100
EOF
)

printf "system.slice is guaranteed %.2f percent CPU time on this $num_cpu_cores core machine. \n" $system_slice_min_cpu_time_percent

# measure actual usage of the system.slice cgroup
tstart=$(date +%s%N)
cstart=$(cat /sys/fs/cgroup/cpu/$cgroup/cpuacct.usage)

sleep $period

tstop=$(date +%s%N)
cstop=$(cat /sys/fs/cgroup/cpu/$cgroup/cpuacct.usage)

system_slice_actual_usage=$(bc -l <<EOF
 ($cstop - $cstart) / ($tstop - $tstart)
EOF
)

system_slice_actual_usage_percentage=$(bc -l <<EOF
 $system_slice_actual_usage * 100
EOF
)

printf "cgroup $cgroup: %.2f percent CPU \n" $system_slice_actual_usage_percentage

# calculate if system.slice gets enough CPU time
# if it does not, kube.reserved CPU has to be increased in order to decrease the CPU shares
# on the kubepods cgroup

kubepods_target_cpu_shares=$(bc -l <<EOF
 (($system_slice_cpu_shares * $num_cpu_cores) / $system_slice_actual_usage) - $system_slice_cpu_shares
EOF
)
kubepods_target_cpu_shares=$(printf "%.0f" $kubepods_target_cpu_shares)
# can be > 2048, because for CPU shares only the ration matters, not the real amount of cores (1024 each)
# per convention (not K8s) 1024 shares means all cores and all other cgroups would use less.
# However,
#  kubepods_shares =  Num_cores * 1024 - kube-reserved_cpu
#  system_slice_shares =  1024

# No kube-reserved: 2 CPU cores -> (1024 / (2048 + 1024)) * 2 = 66% of one core minimum CPU time for system.slice
# No kube-reserved: 64 CPU cores -> (1024 / (64 * 1024 + 1024)) * 64 = 101% of one core minimum CPU time
# --> massive OVER-RESERVATION
#      No resource contention: --> not a problem: kubepods can use all shares in case of no resource-contention
#      Resource contention:
#         - Pod Workload gets CPU starved as needs way more CPU time than shares
#          (bad scheduling as thinks more CPU time can be requested (only 90 m reserved))
#         - Problem: if system.slice has not enough shares.

# !!!! Problem: adding 5 cores to kube-reserved on a machine of 96 cores, only adds ~ 0.1 core to
# what the non-pod processes get under contention. !!!!
# --> due to always 1024 shares for system.slice and kubepods 1024 * num_cores
# --> The writer of the k8s kube-reserved did not understand that cpu.shares are relative
#  See: https://github.com/kubernetes/kubernetes/issues/72881#issuecomment-821224980
# Solution: K8s should also set the cpu.shares on the system.slice:
# https://github.com/kubernetes/kubernetes/issues/72881#issuecomment-868452156



printf "kubepods CPU shares. Current: %.0f. Target: %.0f \n" $kubepods_cpu_shares $kubepods_target_cpu_shares


total_cpu_shares=$(bc -l <<EOF
 $num_cpu_cores * 1024
EOF
)

target_kube_reserved_cpu=$(bc -l <<EOF
 $total_cpu_shares - $kubepods_target_cpu_shares
EOF
)

# I cannot set more cpu shares on kubepods than 1024 * num_cores (if kube-reserved=0m)
# because this is how K8s sets the shares
if [ $kubepods_target_cpu_shares -gt $total_cpu_shares ];
then
  # system reserved shall be minimum default of 80m
  target_kube_reserved_cpu=80 # Gardener default
fi

current_kube_reserved_cpu=$(bc -l <<EOF
 $total_cpu_shares - $kubepods_cpu_shares
EOF
)

printf "system-reserved CPU->  Current: %.2f m | Target : %.2f m \n" $current_kube_reserved_cpu $target_kube_reserved_cpu

printf "\n"

done


# For processes own cgroup:
# cgroup=$(awk -F: '$2 == "cpu,cpuacct" { print $3 }' /proc/self/cgroup)