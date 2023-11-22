ebpf_podname="d060239-ebpf-lt4md"

mkdir -p /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/hack/bpf/kubelet/prow_gl_934_10/no_load/$ebpf_podname

for i in {4..4}
do
  kubectl cp --retries=-1 $ebpf_podname:kubelet.oncpu_kernel.stacks.$i /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/hack/bpf/kubelet/prow_gl_934_10/no_load/$ebpf_podname/kubelet.oncpu_kernel.stacks_${ebpf_podname}_${i}
  kubectl cp --retries=-1 $ebpf_podname:kubelet.offcpu_kernel.stacks.$i /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/hack/bpf/kubelet/prow_gl_934_10/no_load/$ebpf_podname/kubelet.offcpu_kernel.stacks_${ebpf_podname}_${i}

  kubectl cp --retries=-1 $ebpf_podname:kubelet.oncpu_userspace.stacks.$i /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/hack/bpf/kubelet/prow_gl_934_10/no_load/$ebpf_podname/kubelet.oncpu_userspace.stacks_${ebpf_podname}_${i}
  kubectl cp --retries=-1 $ebpf_podname:kubelet.offcpu_userspace.stacks.$i /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/hack/bpf/kubelet/prow_gl_934_10/no_load/$ebpf_podname/kubelet.offcpu_userspace.stacks_${ebpf_podname}_${i}
  echo "done copying iteration $i"
done
