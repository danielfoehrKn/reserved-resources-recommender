For the Kubelet Summary API 
- use the K8s API server proxy to connect to the kubelet
- First, get the node name 
```
k proxy &
curl http://localhost:8001/api/v1/nodes/shoot--d060239--dev-seed-gcp-cpu-worker-z1-66bf7-xjzzn/proxy/stats/summary  > /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/example/disk_reservation_calculation/kubelet_summary_api/stats_summary.json
```