# systemctl cat containerd
# to see where the config file is located to be able to edit it
[debug]
  address = "/run/containerd/debug.sock"
  uid = 0
  gid = 0
  level = "debug"




# OLD: Look at containerd-io instead


## Analyse the Memory and CPU usage of
- containerd (use pprof)
- containerd-shims for the container processes

Question: where is the main memory increase coming from?
 - I assume from a mix between containerd + its runtime shim for 


### Overhead due to IPC

See: [containerd docs](https://github.com/containerd/containerd/blob/f0a32c66dad1e9de716c9960af806105d691cd78/runtime/v2/README.md#debugging-and-shim-logs).
```
A fifo on unix or named pipe on Windows will be provided to the shim. 
It can be located inside the cwd of the shim named "log". 
The shims can use the existing github.com/containerd/containerd/log package to log debug messages. 
Messages will automatically be output in the containerd's daemon logs with the correct fields and runtime set.
```


Containerd-shims can use IPC (message passing via FIFO) to write logs 
- will be read by the containerd daemon and outputtet to its own logs!

**Resource impact**:
- message passing IPC requires OS to cross user-kernel space and also spend CPU cycles on copying data from P1 to P2
- excessive use of that mechanism could lead to the OS needed more CPU and memory!

**Question**:
- is that FIFO used also by the container process to write it's logs or is it only for the shim's logs? 
- 
I remeber, the container logs are 
1) Written to STDOUT (PIPE) by container process
2) containerd shim reads from PIPE and forwards to log file
3) Containerd deamon has fd to log file open

--> I think only logs of the containerd-shim itself are written to the log 

### Containerd loggin deeper look

Look at shim directory
 - see that the `log` file is a pipe.
 - this is the pipe the documentation above talks about that the containerd shim can use to log debug messages to containerd!
```
root@ip-10-250-29-173:/# ls -la /run/containerd/io.containerd.runtime.v2.task/k8s.io/89210462c7be2b5bf98a0fb1bc978bdf4e4632a836e009ae56c471ca1931c315/
total 20
drwx--x--x  3 root root  220 Sep 28 06:03 .
drwx--x--x 44 root root  880 Sep 28 11:26 ..
-rw-r--r--  1 root root   89 Sep 28 06:03 address
-rw-r--r--  1 root root 3085 Sep 28 06:03 config.json
-rw-r--r--  1 root root    4 Sep 28 06:03 init.pid
prwx------  1 root root    0 Sep 28 06:03 log
-rw-r--r--  1 root root    0 Sep 28 06:03 log.json
-rw-------  1 root root    2 Sep 28 06:03 options.json
drwxr-xr-x  1 root root 4096 Sep 28 06:03 rootfs
-rw-------  1 root root    0 Sep 28 06:03 runtime
lrwxrwxrwx  1 root root  121 Sep 28 06:03 work -> /var/lib/containerd/io.containerd.runtime.v2.task/k8s.io/89210462c7be2b5bf98a0fb1bc978bdf4e4632a836e009ae56c471ca1931c315
```


Shim process fds indicates that
- STDIO set to `/dev/null`
- has `log` pipe open
- also reads from the other side of the pipe of the container process (Pipes not shown here)

```
root@ip-10-250-29-173:/# ls -la /proc/3753/fd
total 0
dr-x------ 2 root root  0 Sep 28 06:03 .
dr-xr-xr-x 9 root root  0 Sep 28 06:03 ..
lr-x------ 1 root root 64 Sep 28 12:35 0 -> /dev/null
l-wx------ 1 root root 64 Sep 28 12:35 1 -> /dev/null
l-wx------ 1 root root 64 Sep 28 12:35 2 -> /dev/null
l-wx------ 1 root root 64 Sep 28 12:35 10 -> /run/containerd/io.containerd.runtime.v2.task/k8s.io/89210462c7be2b5bf98a0fb1bc978bdf4e4632a836e009ae56c471ca1931c315/log
l--------- 1 root root 64 Sep 28 06:03 9 -> /run/containerd/io.containerd.runtime.v2.task/k8s.io/89210462c7be2b5bf98a0fb1bc978bdf4e4632a836e009ae56c471ca1931c315/log
```

Containerd daemon has fd open to all the logs files of every shim process (many!)
```
$ ls -la /proc/467620/fd

l--------- 1 root root 64 Sep 28 11:31 74 -> /run/containerd/io.containerd.runtime.v2.task/k8s.io/89210462c7be2b5bf98a0fb1bc978bdf4e4632a836e009ae56c471ca1931c315/log
lrwx------ 1 root root 64 Sep 28 11:31 75 -> /run/containerd/io.containerd.runtime.v2.task/k8s.io/89210462c7be2b5bf98a0fb1bc978bdf4e4632a836e009ae56c471ca1931c315/log
```

Now the question is: 
 - does the daemon send the logs to containerd via message passing (UDS via grpc) or by opening the same file? 
 - I hope not UDS as this can be very expensive


These are the logs files of all the pod and their cotainer processes
 - the kubelet actually created those directories
```
tree /var/log/pods
/var/log/pods
|-- default_debugpod-d060239_e9996bab-e340-40b4-8b3e-0316c917c9d4
|   `-- root-container
|       `-- 0.log
|-- garden_dependency-watchdog-endpoint-7f6547748-jmldz_409e8a63-40ae-4837-ade3-09c89c17c895
|   `-- dependency-watchdog
|       |-- 0.log
|       `-- 1.log
|-- garden_dependency-watchdog-probe-5fd8698f6f-6dvbx_ecafedd1-6773-4d1b-a70b-f725fecf07f2
|   `-- dependency-watchdog
|       `-- 0.log
|-- garden_fluent-bit-4qq62_59d92877-c938-4223-ae11-497188942fca
|   |-- fluent-bit
|   |   `-- 0.log
|   |-- install-plugin
|   |   `-- 0.log
|   `-- journald-config
|       `-- 0.log
|-- garden_gardener-resource-manager-697c9579b4-wbmz6_184c9609-7a43-4f1b-a6ab-e08490dc9322
|   `-- gardener-resource-manager
|       `-- 0.log
|-- garden_hvpa-controller-574d8d645-g6cgs_3461b7d5-6783-4b91-a460-cd4a308ba4b6
|   `-- hvpa-controller
|       `-- 0.log
|-- garden_vpa-admission-controller-6d8d4c447f-2sgtc_39feebe6-f775-4f97-a890-5da2b5f795e5
|   `-- admission-controller
|       `-- 0.log
|-- garden_vpa-exporter-86f79d69bc-gjp9d_93552e56-39c0-4202-9b80-be3338669987
|   `-- exporter
|       `-- 0.log
|-- garden_vpa-recommender-64bf9bdb9b-j8qcf_3115035c-7322-4bf1-ad1b-e02e2d763c20
|   `-- recommender
|       `-- 0.log
|-- istio-system_istiod-78cf88cbc6-dt2mk_41312b3b-1bda-4081-a230-1064935fcf40
|   `-- discovery
|       `-- 0.log
|-- kube-system_apiserver-proxy-qflw8_e2b45da6-947c-4c5a-acce-5e9045142070
|   |-- proxy
|   |   `-- 0.log
|   |-- setup
|   |   `-- 0.log
|   `-- sidecar
|       `-- 0.log
|-- kube-system_blackbox-exporter-7bf7d8b94-xhhr8_989fd539-3474-4497-9c46-4dfccd1aec64
|   `-- blackbox-exporter
|       `-- 0.log
|-- kube-system_calico-kube-controllers-7f6744f49c-78ssf_f72e053b-15cc-4e75-9cbf-46d893ee19c0
|   `-- calico-kube-controllers
|       `-- 0.log
|-- kube-system_calico-node-5s9sn_8be7a35f-361f-437e-ac10-3dfc46f4397d
|   |-- calico-node
|   |   `-- 0.log
|   |-- flexvol-driver
|   |   `-- 0.log
|   |-- install-cni
|   |   `-- 0.log
|   `-- install-cni-check
|       `-- 0.log
|-- kube-system_csi-driver-node-msdq7_f33a1a07-336b-4070-96d1-ee7c057af8cb
|   |-- csi-driver
|   |   `-- 0.log
|   |-- csi-liveness-probe
|   |   `-- 0.log
|   `-- csi-node-driver-registrar
|       `-- 0.log
|-- kube-system_kube-proxy-tzdld_7f2ea5f2-6eff-4689-adbc-b80e44f7349e
|   |-- cleanup
|   |   `-- 0.log
|   |-- conntrack-fix
|   |   `-- 0.log
|   `-- kube-proxy
|       `-- 0.log
|-- kube-system_node-exporter-fhrz8_52b445fb-ee16-49ba-a7a2-bbdaa354ae3f
|   `-- node-exporter
|       `-- 0.log
|-- kube-system_node-problem-detector-r455q_e7d63418-5671-49c9-b7e4-e478ae7a252a
|   `-- node-problem-detector
|       `-- 0.log
`-- ls-system_gardener-landscaper-webhooks-6dbf775c66-d9gqg_878f7806-0cf6-4e49-b60b-164a235347f6
    `-- landscaper
        |-- 1.log
        `-- 2.log
```


Question: how do those logs end up under `var/logs/containers`?
 - those are not symlinks like when docker ist the container runtime!
```
root@ip-10-250-29-173:/# ls -la /var/log/pods/garden_dependency-watchdog-endpoint-7f6547748-jmldz_409e8a63-40ae-4837-ade3-09c89c17c895/dependency-watchdog/
total 16
drwxr-xr-x 2 root root 4096 Sep 28 06:52 .
drwxr-xr-x 3 root root 4096 Sep 28 06:06 ..
-rw-r----- 1 root root 1992 Sep 28 06:52 0.log
-rw-r----- 1 root root 1693 Sep 28 06:52 1.log

```

Directly access logs
```
root@ip-10-250-29-173:/# cat /var/log/pods/garden_dependency-watchdog-endpoint-7f6547748-jmldz_409e8a63-40ae-4837-ade3-09c89c17c895/dependency-watchdog/1.log
2021-09-28T06:52:05.160881377Z stderr F W0928 06:52:05.160748       1 client_config.go:543] Neither --kubeconfig nor --master was specified.  Using the inClusterConfig.  This might not work.
2021-09-28T06:52:05.162467002Z stderr F I0928 06:52:05.162378       1 leaderelection.go:242] attempting to acquire leader lease  garden/dependency-watchdog...
2021-09-28T06:52:05.358612543Z stderr F I0928 06:52:05.358509       1 leaderelection.go:252] successfully acquired lease garden/dependency-watchdog
```



```
root@ip-10-250-29-173:/# ps auxf | grep 3753
root        3753  0.1  0.1 1527128 20368 ?       SLl  06:03   0:43 /usr/bin/containerd-shim-runc-v2 -namespace k8s.io -id 89210462c7be2b5bf98a0fb1bc978bdf4e4632a836e009ae56c471ca1931c315 -address /run/containerd/containerd.sock
root      578889  0.0  0.0   3172   716 ?        S+   12:49   0:00          \_ grep 3753
```




