### Build kubelet:

```
KUBE_BUILD_PLATFORMS=linux/amd64 make WHAT=cmd/kubelet
```

or 
```
build/run.sh make kubelet KUBE_BUILD_PLATFORMS=linux/amd64
```
Exports to when building in docker: 
```
/Users/d060239/go/src/k8s.io/kubernetes/_output/dockerized/bin/linux/amd64
```

*This dir is synced read-only with lima*!

### Run make local-garden-up 

Run rom lima exclusive folder `/home/d060239.linux/gardener` to not intersect with OSX setup

### Add flag --allow-privileged=true to kube-apiserver docker container 

*Required for*: calico CNI 
Adjust lima copy of local setup in `/home/d060239.linux/gardener/hack/local-development/local-garden/run-kube-apiserver`

### Run kubelet in lima as a process

Compiled above. 
Run from `/Users/d060239/go/src/k8s.io/kubernetes/_output/dockerized/bin/linux/amd64`

```
sudo ./kubelet \
--config=/Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/example/kubelet/config \
--cni-bin-dir=/opt/cni/bin/ \
--cni-conf-dir=/etc/cni/net.d/ \
--kubeconfig=/Users/d060239/go/src/github.com/gardener/gardener/hack/local-development/local-garden/kubeconfigs/default-admin.conf \
--network-plugin=cni \
--v=2
```

The kubelet should create a Node in `NotReady` state.

```
d060239@lima-docker:/var/log/containers$ k get nodes
NAME          STATUS   ROLES    AGE   VERSION
lima-docker   NotReady    <none>   18h   v1.23.0-alpha.1.413+dd2d12f6dc0e65-dirty
```

However, will complain about missing CNI.

```
341] "Container runtime network not ready" networkReady="NetworkReady=false reason:NetworkPluginNotReady message:docker: network plugin is not ready: cni config uninitialized"
I0915 16:44:10.189524   34787 cni.go:240] "Unable to update cni config" err="no networks found in /etc/cni/net.d/"
```

### Install kube-proxy
- for any `svc` or anthing requiring IPTable rules to work
  - Calico-node requires kube-proxy that install IPtables rules for `services`
  - otherwise, CNI binaries and config is installed, but calico-node is crashlooping

  
Check for rules with comment "Kubernetes"
```
sudo iptables -vL -n -t nat
```

Before the proxy has started

```
sudo iptables -vL -n -t nat
# Warning: iptables-legacy tables present, use iptables-legacy to see them
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination
    1    44 DOCKER     all  --  *      *       0.0.0.0/0            0.0.0.0/0            ADDRTYPE match dst-type LOCAL

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination
    2   120 DOCKER     all  --  *      *       0.0.0.0/0           !127.0.0.0/8          ADDRTYPE match dst-type LOCAL

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination
30375 1861K KUBE-POSTROUTING  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* kubernetes postrouting rules */
    0     0 MASQUERADE  all  --  *      !docker0  172.17.0.0/16        0.0.0.0/0

Chain DOCKER (2 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 RETURN     all  --  docker0 *       0.0.0.0/0            0.0.0.0/0

Chain KUBE-KUBELET-CANARY (0 references)
 pkts bytes target     prot opt in     out     source               destination

Chain KUBE-MARK-DROP (0 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0            MARK or 0x8000

Chain KUBE-MARK-MASQ (0 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0            MARK or 0x4000

Chain KUBE-POSTROUTING (1 references)
 pkts bytes target     prot opt in     out     source               destination
30375 1861K RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            mark match ! 0x4000/0x4000
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0            MARK xor 0x4000
    0     0 MASQUERADE  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* kubernetes service traffic requiring SNAT */ random-fully
```

After the proxy has started 

```
root@lima-docker:/var/log/containers# sudo iptables -vL -n -t nat
# Warning: iptables-legacy tables present, use iptables-legacy to see them
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination
    0     0 KUBE-SERVICES  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* kubernetes service portals */
    1    44 DOCKER     all  --  *      *       0.0.0.0/0            0.0.0.0/0            ADDRTYPE match dst-type LOCAL

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination
   17  1060 KUBE-SERVICES  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* kubernetes service portals */
    3   180 DOCKER     all  --  *      *       0.0.0.0/0           !127.0.0.0/8          ADDRTYPE match dst-type LOCAL

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination
32441 1990K KUBE-POSTROUTING  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* kubernetes postrouting rules */
    0     0 MASQUERADE  all  --  *      !docker0  172.17.0.0/16        0.0.0.0/0

Chain DOCKER (2 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 RETURN     all  --  docker0 *       0.0.0.0/0            0.0.0.0/0

Chain KUBE-KUBELET-CANARY (0 references)
 pkts bytes target     prot opt in     out     source               destination

Chain KUBE-MARK-DROP (0 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0            MARK or 0x8000

Chain KUBE-MARK-MASQ (1 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0            MARK or 0x4000

Chain KUBE-NODEPORTS (1 references)
 pkts bytes target     prot opt in     out     source               destination

Chain KUBE-POSTROUTING (1 references)
 pkts bytes target     prot opt in     out     source               destination
   17  1060 RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            mark match ! 0x4000/0x4000
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0            MARK xor 0x4000
    0     0 MASQUERADE  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* kubernetes service traffic requiring SNAT */ random-fully

Chain KUBE-PROXY-CANARY (0 references)
 pkts bytes target     prot opt in     out     source               destination

Chain KUBE-SEP-O5BWAI7KXUGEM3QA (1 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 KUBE-MARK-MASQ  all  --  *      *       172.26.0.3           0.0.0.0/0            /* default/kubernetes:https */
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            /* default/kubernetes:https */ tcp to:172.26.0.3:2443

Chain KUBE-SERVICES (2 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 KUBE-SVC-NPX46M4PTMTKRN6Y  tcp  --  *      *       0.0.0.0/0            10.0.0.1             /* default/kubernetes:https cluster IP */ tcp dpt:443
   17  1060 KUBE-NODEPORTS  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* kubernetes service nodeports; NOTE: this must be the last rule in this chain */ ADDRTYPE match dst-type LOCAL

Chain KUBE-SVC-NPX46M4PTMTKRN6Y (1 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 KUBE-SEP-O5BWAI7KXUGEM3QA  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* default/kubernetes:https */
```

Added rule for the `default/kubernetes` service
 - `kube-apiserver` is serving at `10.0.0.1`


d060239@lima-docker:/sys/fs/cgroup/memory/kubepods$ ka endpoints
NAMESPACE   NAME         ENDPOINTS         AGE
default     kubernetes   172.26.0.3:2443   20h


```
Chain KUBE-SERVICES (2 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 KUBE-SVC-NPX46M4PTMTKRN6Y  tcp  --  *      *       0.0.0.0/0            10.0.0.1             /* default/kubernetes:https cluster IP */ tcp dpt:443
   17  1060 KUBE-NODEPORTS  all  --  *      *       0.0.0.0/0            0.0.0.0/0            /* kubernetes service nodeports; NOTE: this must be the last rule in this chain */ ADDRTYPE match dst-type LOCAL

```

### Install CNI 
- could not quiclly get Calico CNI running as I deploys calico-kube-controllers deployment (needs scheduler + deployment controller in kcm)
- could not connect via default kubernetes svc to kube-apiserver container :(


Run `/cni/init-cni.sh`
 - should be run on startup by lima


#### Alternatively: install calico CNI


Run calico-node to install CNI
- Otherwise, node will not get READY as no CNI is installed


Download calico manifests

```
 curl https://docs.projectcalico.org/manifests/calico.yaml -O
```
Remove the daemonset calico-node, as we will not use it.


Just install one calico-node pod with `nodeName: lima-docker`
 - we only want one node
 - no need to activate the `DaemonSet` controller in the `kube-controller manager`
 - no need to run the `kube-scheduler` locally

The calico node pod has toleration for Node `NotReady` and uses host network (no existing CNI required) to install CNI.
 - this is how CNI is bootstrapped without existing CNI (prerequisite for any pod in the pod network) and the Node in `NotReady`


Required kube-proxy to run prior. `calico-node` connects to API server default backing store via default service `ClusterIP`
using a kubeconfig written to "/etc/cni/net.d/calico-kubeconfig"

```
export KUBECONFIG=/etc/cni/net.d/calico-kubeconfig
root@lima-docker:/var/log/containers# kubectl config view
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: DATA+OMITTED
    server: https://[10.0.0.1]:443
  name: local
contexts:
- context:
    cluster: local
    user: calico
  name: calico-context
current-context: calico-context
kind: Config
preferences: {}
users:
- name: calico
  user:
    token: REDACTED
```

See master service

```
d060239@lima-docker:/var/log/containers$ ka svc
NAMESPACE   NAME         TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)   AGE
default     kubernetes   ClusterIP   10.0.0.1     <none>        443/TCP   18h
```









