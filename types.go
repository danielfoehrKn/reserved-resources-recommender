package main

type KubeletConfiguration struct {
	// systemReserved is a set of ResourceName=ResourceQuantity (e.g. cpu=200m,memory=150G)
	// pairs that describe resources reserved for non-kubernetes components.
	// Currently only cpu and memory are supported.
	// See http://kubernetes.io/docs/user-guide/compute-resources for more detail.
	// Default: nil
	// +optional
	// This CAN be set for gardener, but does not HAVE to be
	// TODO: what happens if set? How do the limits on kubepods look like
	// if system-reserved + kube-reserved
	SystemReserved map[string]string `json:"systemReserved,omitempty"`
	// A set of ResourceName=ResourceQuantity (e.g. cpu=200m,memory=150G) pairs
	// that describe resources reserved for kubernetes system components.
	// Currently cpu, memory and local storage for root file system are supported.
	// See http://kubernetes.io/docs/user-guide/compute-resources for more detail.
	// Default: nil
	// +optional
	KubeReserved map[string]string `json:"kubeReserved,omitempty"`

	// POSSIBLY USEFUL IN THE FUTURE

	// TODO: should I use this instead of setting it with systemd
	// oomScoreAdj is The oom-score-adj value for kubelet process. Values
	// must be within the range [-1000, 1000].
	// Dynamic Kubelet Config (beta): If dynamically updating this field, consider that
	// it may impact the stability of nodes under memory pressure.
	// Default: -999
	// +optional
	// OOMScoreAdj *int32 `json:"oomScoreAdj,omitempty"`

	// Map of signal names to quantities that defines hard eviction thresholds. For example: {"memory.available": "300Mi"}.
	// To explicitly disable, pass a 0% or 100% threshold on an arbitrary resource.
	// Default:
	//   memory.available:  "100Mi"
	//   nodefs.available:  "10%"
	//   nodefs.inodesFree: "5%"
	//   imagefs.available: "15%"
	// +optional
	// EvictionHard map[string]string `json:"evictionHard,omitempty"`

	// // kubeletCgroups is the absolute name of cgroups to isolate the kubelet in
	// // Dynamic Kubelet Config (beta): This field should not be updated without a full node
	// // reboot. It is safest to keep this value the same as the local config.
	// // Default: ""
	// // +optional
	// KubeletCgroups string `json:"kubeletCgroups,omitempty"`
	// // systemCgroups is absolute name of cgroups in which to place
	// // all non-kernel processes that are not already in a container. Empty
	// // for no container. Rolling back the flag requires a reboot.
	// // Default: ""
	// // +optional
	// SystemCgroups string `json:"systemCgroups,omitempty"`
	//
	// // This flag helps kubelet identify absolute name of top level cgroup used to enforce `SystemReserved` compute resource reservation for OS system daemons.
	// // Refer to [Node Allocatable](https://git.k8s.io/community/contributors/design-proposals/node/node-allocatable.md) doc for more information.
	// // Dynamic Kubelet Config (beta): This field should not be updated without a full node
	// // reboot. It is safest to keep this value the same as the local config.
	// // Default: ""
	// // +optional
	// SystemReservedCgroup string `json:"systemReservedCgroup,omitempty"`
	// // This flag helps kubelet identify absolute name of top level cgroup used to enforce `KubeReserved` compute resource reservation for Kubernetes node system daemons.
	// // Refer to [Node Allocatable](https://git.k8s.io/community/contributors/design-proposals/node/node-allocatable.md) doc for more information.
	// // Dynamic Kubelet Config (beta): This field should not be updated without a full node
	// // reboot. It is safest to keep this value the same as the local config.
	// // Default: ""
	// // +optional
	// KubeReservedCgroup string `json:"kubeReservedCgroup,omitempty"`
}
