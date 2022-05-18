nerdctl run \
--net=host \
--pid=host \
-v /var/lib/kubelet:/var/lib/kubelet \
-v /sys/fs/cgroup:/sys/fs/cgroup \
-v /dev:/dev \
-v /var/lib/containerd/:/var/lib/containerd \
-v /run/containerd:/run/containerd \
-v /var/log/pods:/var/log/pods \
--env MEMORY_SAFETY_MARGIN_ABSOLUTE=200Mi \
--env PERIOD=30s \
--env CGROUPS_HIERARCHY_ROOT=/sys/fs/cgroup \
--env CGROUPS_CONTAINERD_ROOT=system.slice/containerd.service \
--env CGROUPS_KUBELET_ROOT=system.slice/kubelet.service \
--env KUBELET_DIRECTORY=/var/lib/kubelet/ \
--pull=always \
--privileged \
eu.gcr.io/gardener-project/gardener/reserved-resources-recommender:latest