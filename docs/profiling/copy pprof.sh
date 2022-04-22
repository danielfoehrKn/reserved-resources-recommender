# How to get pprof with ctr
# ctr pprof heap > heap.out
# ctr pprof profile > profile.out
# ctr pprof goroutines > goroutines.out
# ctr pprof block > block.out


kubectl cp default/debugpod-d060239:/hostroot/heap.out  /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/docs/profiling/seed/heap.out
kubectl cp default/debugpod-d060239:/hostroot/profile.out  /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/docs/profiling/seed/profile.out
kubectl cp default/debugpod-d060239:/hostroot/goroutines.out  /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/docs/profiling/seed/goroutines.out
kubectl cp default/debugpod-d060239:/hostroot/block.out  /Users/d060239/go/src/github.com/danielfoehrkn/better-resource-reservations/docs/profiling/seed/block.out