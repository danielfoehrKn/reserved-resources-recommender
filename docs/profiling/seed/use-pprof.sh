go tool pprof -png block.out > block.png
go tool pprof -png heap.out > heap.png
go tool pprof -png profile.out > profile.png
go tool pprof -png goroutines.out > goroutines.png