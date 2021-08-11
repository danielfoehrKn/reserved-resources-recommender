kubectl exec -ti debugpod-d060239 -- bash -c "chroot /hostroot"

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

# metrics from single pods
k port-forward pod/better-resource-reservations-<pod-id-from-daemonset>  16911:16911
watch 'curl localhost:16911/metrics | grep -e "node_memory" -e "kubelet_"'
curl localhost:16911/metrics | grep -e "node_cgroup_" -e "kubelet_"

# metrics via prometheus, visit http://localhost:8080/targets
kubectl port-forward svc/prometheus-web 8080:9090 -n monitoring

# grafana (default password and user: admin)
#visit http://localhost:3000
kubectl port-forward svc/grafana 3000:3000 -n monitoring
# configure data source with URL: prometheus-web:9090