package workload

import (
	constants "github.com/cofide/spiffe-enable/internal/const"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

var spiffeWLVolume = corev1.Volume{
	Name: constants.SPIFFEWLVolume,
	VolumeSource: corev1.VolumeSource{
		CSI: &corev1.CSIVolumeSource{
			Driver:   "csi.spiffe.io",
			ReadOnly: ptr.To(true),
		},
	},
}

var spiffeWLVolumeMount = corev1.VolumeMount{
	Name:      constants.SPIFFEWLVolume,
	MountPath: constants.SPIFFEWLMountPath,
	ReadOnly:  true,
}

var spiffeWLEnvVar = corev1.EnvVar{
	Name:  constants.SPIFFEWLSocketEnvName,
	Value: constants.SPIFFEWLSocket,
}

func GetSPIFFEVolume() corev1.Volume {
	return spiffeWLVolume
}

func GetSPIFFEVolumeMount() corev1.VolumeMount {
	return spiffeWLVolumeMount
}

func GetSPIFFEEnvVar() corev1.EnvVar {
	return spiffeWLEnvVar
}

// Helper function to check if a volume already exists
func VolumeExists(pod *corev1.Pod, volumeName string) bool {
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == volumeName {
			return true
		}
	}
	return false
}

// Helper function to check if a container already exists
func ContainerExists(containers []corev1.Container, containerName string) bool {
	for _, container := range containers {
		if container.Name == containerName {
			return true
		}
	}
	return false
}

// Helper function to check if an environment variable exists in a container
func EnvVarExists(container *corev1.Container, envVarName string) bool {
	for _, env := range container.Env {
		if env.Name == envVarName {
			return true
		}
	}
	return false
}

// Helper function to check if an init container already exists (by name)
func InitContainerExists(pod *corev1.Pod, containerName string) bool {
	return ContainerExists(pod.Spec.InitContainers, containerName)
}
