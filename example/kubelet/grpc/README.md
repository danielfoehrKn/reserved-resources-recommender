Default root configuration directory of the kubelet is at `/var/lib/kubelet/`.

You can also see the subdirectory `pod-resources` that contains the UDS for the `pod-resources` feature-gate.
This is also where my `dynamicResourceReservations` feature gate will create the UDS for the server.

```
d060239@lima-docker:~$ ls /var/lib/kubelet/
cpu_manager_state  device-plugins  memory_manager_state  pki  plugins  plugins_registry  pod-resources  pods
```


## Post new resource reservations against the kubelet server

CD to where the proto file resides (need to have it because the grpc service does not
support the reflection API to discover the services)

```
cd /Users/d060239/go/src/github.com/danielfoehrkn/resource-reservations-grpc/pkg/proto
```


See grpc service
```
grpcurl -unix -plaintext -proto ./resource-reservations.proto /var/lib/kubelet/dynamic-resource-reservations/kubelet.sock list
v1.ResourceReservations
```

See methods in service
```
grpcurl -unix -plaintext -proto ./resource-reservations.proto /var/lib/kubelet/dynamic-resource-reservations/kubelet.sock list
v1.ResourceReservations.UpdateResourceReservations
```

Describe the service

```
grpcurl -unix -plaintext -proto ./resource-reservations.proto /var/lib/kubelet/dynamic-resource-reservations/kubelet.sock describe v1.ResourceReservations
v1.ResourceReservations is a service:
// ResourceReservations is a service provided by the kubelet that provides information about the
// current resources reservations on the node
service ResourceReservations {
  rpc GetResourceReservations ( .v1.GetResourceReservationsRequest ) returns ( .v1.GetResourceReservationsResponse );
  rpc UpdateResourceReservations ( .v1.UpdateResourceReservationsRequest ) returns ( .v1.UpdateResourceReservationsResponse );
}
```

Describe method

```
grpcurl -unix -plaintext -proto ./resource-reservations.proto /var/lib/kubelet/dynamic-resource-reservations/kubelet.sock describe v1.ResourceReservations.UpdateResourceReservations

v1.ResourceReservations.UpdateResourceReservations is a method:
rpc UpdateResourceReservations ( .v1.UpdateResourceReservationsRequest ) returns ( .v1.UpdateResourceReservationsResponse );
```



## Send invalid kube-reserved

For mappings between proto and JSON see [here](https://developers.google.com/protocol-buffers/docs/proto3#json)
 - this is how the kube-reserved map can be send


Should cause a parser failure in the kubelet
```
grpcurl -unix -plaintext -proto ./resource-reservations.proto -d @ /var/lib/kubelet/dynamic-resource-reservations/kubelet.sock v1.ResourceReservations.UpdateResourceReservations <<EOM
{
    "KubeReserved" : {"k": "v"}
}
EOM
```

Returns:
```
ERROR:
  Code: Unknown
  Message: cannot reserve "k" resource
```

Kubelet logs:

```
UpdateResourceReservations: failed to parse system reserved: map[]
```

## Send valid kube-reserved

```
grpcurl -unix -plaintext -proto ./resource-reservations.proto -d @ /var/lib/kubelet/dynamic-resource-reservations/kubelet.sock v1.ResourceReservations.UpdateResourceReservations <<EOM
{
    "KubeReserved" : {"memory": "2Gi"}
}
EOM
```



See in the Node that it really got updated!

```
Normal  NodeReady                3m13s                 kubelet  Node lima-docker status is now: NodeReady
  Normal  NodeAllocatableEnforced  38s (x11 over 3m23s)  kubelet  Updated Node Allocatable limit across pods
```

Also check the cgroup kubepods
```
d060239@lima-docker:/sys/fs/cgroup/memory/kubepods$ watch cat memory.limit_in_bytes
```

## List current resource kube-reserved

```
root@lima-docker:/Users/d060239/go/src/github.com/danielfoehrkn/resource-reservations-grpc/pkg/proto# grpcurl -unix -plaintext -proto ./resource-reservations.proto -d @ /var/lib/kubelet/dynamic-resource-reservations/kubelet.sock v1.ResourceReservations.GetResourceReservations <<EOM
{}
EOM


{
  "SystemReserved": {
    "cpu": "80m",
    "memory": "1Gi",
    "pid": "20k"
  }
}
```