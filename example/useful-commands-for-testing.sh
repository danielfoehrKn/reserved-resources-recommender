# These are commands useful during demoing
# the idea is to not rely on a constant exec shell as the kubelet can be restarted any time
# use each command in a diff. terminal window

watch "kubectl exec -ti debugpod-d060239 -- free -m"

# output the kubepods memory limit in Bytes
watch "echo "Kubepods limit in bytes: " &&  kubectl exec -ti debugpod-d060239 -- bash -c 'cat /hostroot/sys/fs/cgroup/memory/kubepods/memory.limit_in_bytes'"

# current kube reserved
watch "kubectl exec -ti debugpod-d060239 -- bash -c 'cat /hostroot/var/lib/kubelet/config/kubelet | grep kubeReserved -A 4'"

# working set bytes for kubepods
# requires get-cgroup-memory.sh script with chmox +x in /hostroot
watch "kubectl exec -ti debugpod-d060239 -- bash -c \"chroot /hostroot /bin/bash -c './get-cgroup-memory.sh kubepods'\""

# working set bytes for system.slice
watch "kubectl exec -ti debugpod-d060239 -- bash -c \"chroot /hostroot /bin/bash -c './get-cgroup-memory.sh system.slice'\""

# dmesg get kernel logs (watch does not show whole logs)
kubectl exec -ti debugpod-d060239 -- bash -c "chroot /hostroot /bin/bash -c 'dmesg -T'"

# last kubelet restart time
watch "kubectl exec -ti debugpod-d060239 -- bash -c \"chroot /hostroot /bin/bash -c 'systemctl status kubelet | grep -i Active'\""

# get non-terminated pods on the node the debug pod is deployed to
watch "kubectl get pod debugpod-d060239 -o json | jq -r .spec.nodeName |  read nodeName && kubectl describe node $nodeName | grep -i 'non-terminated'"

# kubelet eviction happened
journalctl -u kubelet -f | grep -i "pods ranked"
