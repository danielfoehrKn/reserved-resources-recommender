package util


// COPIED FROM https://github.com/containerd/cgroups/blob/main/utils.go

import (
	"io/ioutil"
	"strconv"
	"strings"
)

func ReadUint(path string) (uint64, error) {
	v, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return parseUint(strings.TrimSpace(string(v)), 10, 64)
}

func parseUint(s string, base, bitSize int) (uint64, error) {
	v, err := strconv.ParseUint(s, base, bitSize)
	if err != nil {
		intValue, intErr := strconv.ParseInt(s, base, bitSize)
		// 1. Handle negative values greater than MinInt64 (and)
		// 2. Handle negative values lesser than MinInt64
		if intErr == nil && intValue < 0 {
			return 0, nil
		} else if intErr != nil &&
			intErr.(*strconv.NumError).Err == strconv.ErrRange &&
			intValue < 0 {
			return 0, nil
		}
		return 0, err
	}
	return v, nil
}

// DecimalSIForBinarySi converts a DecimalSI number to BinarySI
func DecimalSIForBinarySi(binarySi int64) int64 {
	return int64(float64(binarySi) / 1024 * 1000)
}

// CalculateCPUReservationBasedOnCapacity calculates the target CPU memory as a function of the node's capacity (#cpu_cores)
// 6% of the first core
// 1% of the next core (up to 2 cores)
// 0.5% of the next 2 cores (up to 4 cores)
// 0.25% of any cores above 4 cores
// example: GKE with 16 cores = 110m reservation = 16271 shares (total: 16384, diff: 113 shares)
// returns the reservation millicores
func CalculateCPUReservationBasedOnCapacity(capacity int64) int64 {
	capacityMilliCores := capacity * 1000
	// 6% of the first core
	if capacityMilliCores <= 1000 {
		i := float32(capacityMilliCores) * float32(0.06)
		return int64(i)
	}
	reservation := float32(1000) * float32(0.06)

	capacityMilliCores = capacityMilliCores - 1000

	// 1% of the next core (up to 2 cores)
	if capacityMilliCores <= 1000 {
		i := float32(capacityMilliCores) * float32(0.01)
		return int64(i) + int64(reservation)
	}
	reservation += float32(1000) * float32(0.01)

	capacityMilliCores = capacityMilliCores - 1000

	// 0.5% of the next 2 cores (up to 4 cores)
	if capacityMilliCores <= 2000 {
		i := float32(capacityMilliCores) * float32(0.005)
		return int64(i) + int64(reservation)
	}
	reservation += float32(2000) * float32(0.005)

	capacityMilliCores = capacityMilliCores - 2000

	// 0.25% of any cores above 4 cores
	rest := float32(capacityMilliCores) * float32(0.0025)
	return int64(rest) + int64(reservation)
}
