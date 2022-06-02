Port forward to get pprof dumps
```
# to all pods / packets will pick one endpoint
k port-forward svc/reserved-resources-recommender 16911:16911
# to certain pod
k port-forward reserved-resources-recommender-h8lj7 16912:16911
```

Check goroutine issues
```
curl  http://localhost:16912/debug/pprof/goroutine > seed-goroutine.out
go tool pprof -png  goroutine.out > seed-goroutine.png
```

Check for increasing memory usage:
```
watch "kubectl top pods | grep -i j7"
reserved-resources-recommender-h8lj7                              20m          14Mi
```

Re-check later 