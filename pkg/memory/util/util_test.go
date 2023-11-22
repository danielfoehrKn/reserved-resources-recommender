package util_test

import (
	"github.com/danielfoehrkn/better-kube-reserved/pkg/memory/util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("CalculateReservationBasedOnCapacity", func() {

	BeforeEach(func() {

	})

	It("should return the correct reservation for < 1Gi", func() {
		result, err := util.CalculateReservationBasedOnCapacity(resource.MustParse("512Mi"))
		Expect(err).ToNot(HaveOccurred())

		parse := resource.MustParse("255Mi")
		Expect(result.Value()).To(Equal(parse.Value()))
	})

	It("should return the correct reservation for < 4Gi", func() {
		result, err := util.CalculateReservationBasedOnCapacity(resource.MustParse("2Gi"))
		Expect(err).ToNot(HaveOccurred())

		parse := resource.MustParse("512Mi")
		Expect(result.Value()).To(Equal(parse.Value()))
	})

	It("should return the correct reservation for < 8Gi", func() {
		result, err := util.CalculateReservationBasedOnCapacity(resource.MustParse("6Gi"))
		Expect(err).ToNot(HaveOccurred())

		// 25 % of 4 Gi = 1024Mi = 1073741824
		// 20 % of 2 Gi = 409.599999428 Mi = 429496729
		// Total = 1503238553 = 1433.599999428 Mi
		Expect(result.Value()).To(Equal(int64(1503238553)))
	})

	It("should return the correct reservation for < 128Gi", func() {
		result, err := util.CalculateReservationBasedOnCapacity(resource.MustParse("64Gi"))
		Expect(err).ToNot(HaveOccurred())

		// 25 % of 4 Gi = 1024Mi = 1073741824
		// 20 % of 4 Gi = 819.199999809 Mi = 858993459
		// 10% of 8 Gi = 858993459,2
		// 6% of 48 Gi = 3092376453
		// Total: 5884105195 = 5611.519999504 Mi
		Expect(result.Value()).To(Equal(int64(5884105195)))
	})

	It("should return the correct reservation for > 128Gi", func() {
		result, err := util.CalculateReservationBasedOnCapacity(resource.MustParse("312Gi"))
		Expect(err).ToNot(HaveOccurred())

		// 25 % of 4 Gi = 1024Mi = 1073741824
		// 20 % of 4 Gi = 819.199999809 Mi = 858993459
		// 10% of 8 Gi = 819.199999809 Mi = 858993459
		// 6% of 112 Gi = 6881.279999733 Mi = 7215545057
		// 2% of 184 Gi = 3768.319999695 Mi = 3951369912
		// Total: 13958643711 = 13311.999999046 Mi
		Expect(result.Value()).To(Equal(int64(13958643711)))
	})
})
