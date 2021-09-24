package kubelet

import (
	"context"
	"time"

	resources "github.com/danielfoehrkn/resource-reservations-grpc/pkg/proto/gen/resource-reservations"
	"k8s.io/apimachinery/pkg/api/resource"
)

// UpdateResourceReservations uses the given grpc client to update the resource reservations via the kubelet's grpc server
// Limitations:
// - only support memory at the moment
// - only sets the kube-reserved settings
func UpdateResourceReservations(ctx context.Context, client resources.ResourceReservationsClient, targetReservedMemory, targetReservedCPU resource.Quantity) error {
	// Unfortunately, enforcing CPU reservations does not make any sense at this point (hence, its commented out)
	// Please see: https://github.com/kubernetes/kubernetes/issues/72881#issuecomment-897217732
	// Problem: By reserving 5 cores on a 94 core machine, Linux actually only granted 0.1 cores more in relation to system.slice.
	// => the scheduler prevents actual workload of 5 cores to be scheduled which makes it not usable -,-
	// TODO: propose fix for this bug for kubelet
	kubeReserved := map[string]string{
		"memory": targetReservedMemory.String(),
		// "cpu": targetReservedCPU.String(),
	}

	request := &resources.UpdateResourceReservationsRequest{KubeReserved: kubeReserved, SystemReserved: map[string]string{}}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.UpdateResourceReservations(ctx, request)
	if err != nil {
		return err
	}
	return nil
}


func GetResourceReservations(ctx context.Context, client resources.ResourceReservationsClient) (map[string]string, map[string]string, error) {
	getResourceReservationsResponse, err := client.GetResourceReservations(ctx, &resources.GetResourceReservationsRequest{})
	if err != nil {
		return nil ,nil, err
	}
	// log.Printf("received current resource reservations: %v", getResourceReservationsResponse)
	return getResourceReservationsResponse.SystemReserved, getResourceReservationsResponse.KubeReserved, nil
}

