# Goal: configure kubelet's "cgroup-root" config to the pod-private cgroup hierarchy
dir=kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod79251c5f_6128_4462_920c_7f59851b1ef0.slice/prowparent.slice/8649fc57370b7efca6895b1436f35fba427134114b9f97bbcc61ef49f76a0034/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-besteffort.slice/kubelet-kubepods-besteffort-pod0a111b9c_2cca_4ea5_a1e8_b6f9abd91662.slice/cri-containerd-5bceb708305ca363d62bf870877cee45bef716e04d9cbdf0cacdea2fd166adcf.scope/system.slice/kubelet.service
parentdir="$(dirname "$dir")"
