nerdctl run \
--net=host \
-v /var/lib/kubelet:/var/lib/kubelet \
-v /sys/fs/cgroup:/sys/fs/cgroup \
--env MEMORY_SAFETY_MARGIN_ABSOLUTE=200Mi \
--env PERIOD=30s \
--pull=always \
eu.gcr.io/gardener-project/gardener/reserved-resources-recommender:latest

#-v /var/lib/kubelet:/var/lib/kubelet \
#-v /sys/fs/cgroup:/sys/fs/cgroup \
#--net=host \
#--pid=host \