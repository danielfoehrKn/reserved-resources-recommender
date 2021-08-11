package kubelet

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	systemdDbus "github.com/coreos/go-systemd/v22/dbus"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	kubeletv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/yaml"
)

const (
	gardenerKubeletFilepath = "/var/lib/kubelet/config/kubelet"
	kubeletServiceName             = "kubelet.service"
)

// LoadKubeletConfig loads the kubeconfig file from the default location for gardener nodes
func LoadKubeletConfig() (*kubeletv1beta1.KubeletConfiguration, error) {
	if _, err := os.Stat(gardenerKubeletFilepath); err != nil {
		return nil, err
	}

	bytes, err := ioutil.ReadFile(gardenerKubeletFilepath)
	if err != nil {
		return nil, err
	}

	if len(bytes) == 0 {
		return nil, fmt.Errorf("kubelet config not found at %q", gardenerKubeletFilepath)
	}

	config := kubeletv1beta1.KubeletConfiguration{}
	err = yaml.Unmarshal(bytes, &config)
	if err != nil {
		return nil, fmt.Errorf("error decoding kubelet config: %w", err)
	}

	return &config, nil
}

// GetKubeletSystemdUnitActiveDuration takes a systemd connection
// returns the duration since when the kubelet systemd service is running
func GetKubeletSystemdUnitActiveDuration(log *logrus.Logger, connection *systemdDbus.Conn) (*time.Duration, error) {
	property, err := connection.GetUnitProperty(kubeletServiceName, "ActiveEnterTimestamp")
	if err != nil {
		return nil, err
	}

	if property == nil {
		return nil, fmt.Errorf("cannot determine last start time of kuebelet systemd service. Property %q not found", "ActiveEnterTimestamp")
	}

	stringProperty := fmt.Sprintf("%v", property.Value.Value())
	activeEnterTimestamp, err := strconv.ParseInt(stringProperty, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot determine last start time of kuebelet systemd service. Property %q cannot be parsed as int64", "ActiveEnterTimestamp")
	}

	activeEnterTimestampUTC := time.Unix(0, activeEnterTimestamp*1000)
	duration := time.Now().Sub(activeEnterTimestampUTC)
	log.Infof("kubelet is running since %q", duration.String())
	return &duration, nil
}

// UpdateKubeReserved  updates the kubelet config file with the new kube reserved memory
func UpdateKubeReserved(newReservedMemory resource.Quantity, config *kubeletv1beta1.KubeletConfiguration) error {
	config.KubeReserved[string(corev1.ResourceMemory)] = newReservedMemory.String()
	if err := updateKubeletConfig(config); err != nil {
		return err
	}
	return nil
}

// updateKubeletConfig writes an update to the kubelet configuration file
// assumes a default gardener-specific location
func updateKubeletConfig(config *kubeletv1beta1.KubeletConfiguration) error {
	out, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to write updated kubelet config: %w", err)
	}

	f, err := os.Create(gardenerKubeletFilepath)
	if err != nil {
		return fmt.Errorf("failed to open kubelet config file: %v", err)
	}

	_, err = f.Write(out)
	if err != nil {
		return fmt.Errorf("failed to write kubelet config file: %v", err)
	}

	return nil
}
