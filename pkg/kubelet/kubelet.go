package kubelet

import (
	"fmt"
	"io/ioutil"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	kubeletv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/yaml"
)



// LoadKubeletConfig loads the kubeconfig file from the default location for gardener nodes
func LoadKubeletConfig(path string) (*kubeletv1beta1.KubeletConfiguration, error) {
	// fmt.Printf("loading kubelet configuration from %q", path)

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(bytes) == 0 {
		return nil, fmt.Errorf("kubelet config not found at %q", path)
	}

	config := kubeletv1beta1.KubeletConfiguration{}
	err = yaml.Unmarshal(bytes, &config)
	if err != nil {
		return nil, fmt.Errorf("error decoding kubelet config: %w", err)
	}

	return &config, nil
}

// UpdateKubeReservedInConfigFile  updates the kubelet config file with the new kube reserved memory
func UpdateKubeReservedInConfigFile(newReservedMemory resource.Quantity, config *kubeletv1beta1.KubeletConfiguration, path string) error {
	config.KubeReserved[string(corev1.ResourceMemory)] = newReservedMemory.String()
	if err := updateKubeletConfig(config, path); err != nil {
		return err
	}
	return nil
}

// updateKubeletConfig writes an update to the kubelet configuration file
// assumes a default gardener-specific location
func updateKubeletConfig(config *kubeletv1beta1.KubeletConfiguration, path string) error {
	out, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to write updated kubelet config: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to open kubelet config file: %v", err)
	}

	_, err = f.Write(out)
	if err != nil {
		return fmt.Errorf("failed to write kubelet config file: %v", err)
	}

	return nil
}
