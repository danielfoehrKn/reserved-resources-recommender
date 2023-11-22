package util_test

import (
	"github.com/danielfoehrkn/better-kube-reserved/pkg/cpu/util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("CalculateCPUReservationBasedOnCapacity", func() {
	It("should return the correct reservation for <= 1 Core", func() {
		result := util.CalculateCPUReservationBasedOnCapacity(1)

		Expect(result).To(Equal(60))
	})

	It("should return the correct reservation for <= 2 Cores", func() {
		result := util.CalculateCPUReservationBasedOnCapacity(2)

		// 6% of 1 core => 60m
		// 1% of 1 core => 10m
		Expect(result).To(Equal(70))
	})

	It("should return the correct reservation for < 4 Cores", func() {
		result := util.CalculateCPUReservationBasedOnCapacity(3)

		// 6% of 1 core => 60m
		// 1% of 1 core => 10m
		// 0.5% of 1 core => 5m
		Expect(result).To(Equal(75))
	})

	It("should return the correct reservation for > 4 Cores", func() {
		result := util.CalculateCPUReservationBasedOnCapacity(16)

		// 6% of 1 core => 60m
		// 1% of 1 core => 10m
		// 0.5% of 2 cores => 10m
		// 0.25% of 12 cores => 30m
		Expect(result).To(Equal(110))
	})
})

var _ = Describe("DecimalSIForBinarySi", func() {
	It("should return the correct reservation for 1024", func() {
		result := util.DecimalSIForBinarySi(1024)

		Expect(result).To(Equal(int64(1000)))
	})
})
