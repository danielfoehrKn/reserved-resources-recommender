package util

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)


var (
	small = resource.MustParse("255Mi")
	oneGi = resource.MustParse("1Gi")
	fourGi = resource.MustParse("4Gi")
	eightGi = resource.MustParse("8Gi")
	highThresholdGi = resource.MustParse("112Gi")
)

// CalculateReservationBasedOnCapacity calculates the target reserved memory as a function of the node's capacity
// 255 MiB of memory for machines with less than 1 GiB of memory
// 25% of the first 4 GiB of memory
// 20% of the next 4 GiB of memory (up to 8 GiB)
// 10% of the next 8 GiB of memory (up to 16 GiB)
// 6% of the next 112 GiB of memory (up to 128 GiB)
// 2% of any memory above 128 GiB
func CalculateReservationBasedOnCapacity(capacity resource.Quantity) (resource.Quantity, error) {
	if capacity.Value() < oneGi.Value() {
		return small, nil
	}

	// 25% of the first 4 GiB of memory
	capacity.Sub(fourGi)
	if capacity.Value() < 0 {
		capacity.Add(fourGi)
		return getPercentageOfResource(capacity, 25)
	}

	reservation, err := getPercentageOfResource(fourGi, 25)
	if err != nil {
		return resource.Quantity{}, err
	}

	// 20% of the next 4 GiB of memory (up to 8 GiB)
	capacity.Sub(fourGi)
	if capacity.Value() < 0 {
		capacity.Add(fourGi)
		v, err := getPercentageOfResource(capacity, 20)
		if err != nil {
			return resource.Quantity{}, err
		}
		reservation.Add(v)
		return reservation, nil
	}

	additionalReservation, err := getPercentageOfResource(fourGi, 20)
	if err != nil {
		return resource.Quantity{}, err
	}

	reservation.Add(additionalReservation)

	// 10% of the next 8 GiB of memory (up to 16 GiB)
	capacity.Sub(eightGi)
	if capacity.Value() < 0 {
		capacity.Add(eightGi)
		v, err := getPercentageOfResource(capacity, 10)
		if err != nil {
			return resource.Quantity{}, err
		}
		reservation.Add(v)
		return reservation, nil
	}

	additionalReservation2, err := getPercentageOfResource(eightGi, 10)
	if err != nil {
		return resource.Quantity{}, err
	}

	reservation.Add(additionalReservation2)

	// 6% of the next 112 GiB of memory (up to 128 Gi)
	capacity.Sub(highThresholdGi)
	if capacity.Value() < 0 {
		capacity.Add(highThresholdGi)
		v, err := getPercentageOfResource(capacity, 6)
		if err != nil {
			return resource.Quantity{}, err
		}
		reservation.Add(v)
		return reservation, nil
	}

	additionalReservation3, err := getPercentageOfResource(highThresholdGi, 6)
	if err != nil {
		return resource.Quantity{}, err
	}

	reservation.Add(additionalReservation3)

	// 2% of any memory above 128 GiB
	additionalReservation4, err := getPercentageOfResource(capacity, 2)
	if err != nil {
		return resource.Quantity{}, err
	}

	reservation.Add(additionalReservation4)

	return reservation, nil
}

func getPercentageOfResource(v resource.Quantity, percentage int) (resource.Quantity, error) {
	value := float64(v.Value()) * float64(percentage)/100
	i := int64(value)
	quantity := resource.NewQuantity(i, resource.BinarySI)
	if quantity == nil {
		return resource.Quantity{}, fmt.Errorf("failed to calculate percentage of reserved resources")
	}
	return *quantity, nil
}
