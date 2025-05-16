package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/cofide/spiffe-enable/internal/helper"
	"github.com/cofide/spiffe-enable/internal/proxy"
	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Pod annotations
const (
	enabledAnnotation                     = "spiffe.cofide.io/enabled"
	injectAnnotation                      = "spiffe.cofide.io/inject"
	debugAnnotation                       = "spiffe.cofide.io/debug"
	spiffeHelperIncIntermediateAnnotation = "spiffe.cofide.io/spiffe-helper-include-intermediate-bundle"
)

// Components that can be injected
const (
	injectAnnotationHelper = "helper"
	injectAnnotationProxy  = "proxy"
)

// SPIFFE Workload API
const (
	spiffeWLVolume        = "spiffe-workload-api"
	spiffeWLMountPath     = "/spiffe-workload-api"
	spiffeWLSocketEnvName = "SPIFFE_ENDPOINT_SOCKET"
	spiffeWLSocket        = "unix:///spiffe-workload-api/spire-agent.sock"
	spiffeWLSocketPath    = "/spiffe-workload-api/spire-agent.sock"
)

// Cofide Agent
const (
	agentXDSPort    = 18001
	agentXDSService = "cofide-agent.cofide.svc.cluster.local"
	envoyProxyPort  = 10000
)

// SPIFFE Enable
const (
	spiffeEnableCertVolumeName = "spiffe-enable-certs"
	spiffeEnableCertDirectory  = "/spiffe-enable"
)

// Debug UI constants
const (
	debugUIContainerName = "spiffe-enable-ui"
	debugUIPort          = 8000
)

// Container images
var (
	spiffeHelperImage = "ghcr.io/spiffe/spiffe-helper:0.10.0"
	initHelperImage   = "010438484483.dkr.ecr.eu-west-1.amazonaws.com/cofide/spiffe-enable-init:v0.1.0-alpha"
	debugUIImage      = "010438484483.dkr.ecr.eu-west-1.amazonaws.com/cofide/spiffe-enable-ui:v0.1.0-alpha"
)

type spiffeEnableWebhook struct {
	Client  client.Client
	decoder admission.Decoder
	Log     logr.Logger
}

// Helper function to check if a volume already exists
func volumeExists(pod *corev1.Pod, volumeName string) bool {
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == volumeName {
			return true
		}
	}
	return false
}

// Helper function to check if a volume mount already exists
func volumeMountExists(pod *corev1.Pod, volumeMountName string) bool {
	// Check standard containers
	for _, container := range pod.Spec.Containers {
		for _, vm := range container.VolumeMounts {
			if vm.Name == volumeMountName {
				return true
			}
		}
	}

	return false
}

// Helper function to check if a container already exists
func containerExists(containers []corev1.Container, containerName string) bool {
	for _, container := range containers {
		if container.Name == containerName {
			return true
		}
	}
	return false
}

// Helper function to check if an environment variable exists in a container
func envVarExists(container *corev1.Container, envVarName string) bool {
	for _, env := range container.Env {
		if env.Name == envVarName {
			return true
		}
	}
	return false
}

// Helper function to check if an init container already exists (by name)
func initContainerExists(pod *corev1.Pod, containerName string) bool {
	return containerExists(pod.Spec.InitContainers, containerName)
}

func NewSpiffeEnableWebhook(client client.Client, log logr.Logger, decoder admission.Decoder) (*spiffeEnableWebhook, error) {
	return &spiffeEnableWebhook{
		Client:  client,
		Log:     log,
		decoder: decoder,
	}, nil
}

func (a *spiffeEnableWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := a.decoder.Decode(req, pod); err != nil {
		a.Log.Error(err, "Failed to decode pod", "request", req.UID)
		return admission.Errored(http.StatusBadRequest, err)
	}

	logger := a.Log.WithValues("podNamespace", pod.Namespace, "podName", pod.Name, "request", req.UID)

	enableAnnotationValue, enableAnnotationExists := pod.Annotations[enabledAnnotation]
	spiffeInjectionEnabled := enableAnnotationExists && enableAnnotationValue == "true"

	if !spiffeInjectionEnabled {
		logger.Info("Skipping all injections, annotation not set or disabled", "annotation", enabledAnnotation)
		return admission.Allowed("Injection criteria not met")
	}

	// Add a CSI volume to the pod for the SPIFFE Workload API
	if !volumeExists(pod, spiffeWLVolume) {
		logger.Info("Adding SPIFFE CSI volume", "volumeName", spiffeWLVolume)
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name:         spiffeWLVolume,
			VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{Driver: "csi.spiffe.io", ReadOnly: ptr.To(true)}},
		})
	}

	var spiffeVolumeMount = corev1.VolumeMount{
		Name:      spiffeWLVolume,
		MountPath: spiffeWLMountPath,
		ReadOnly:  true,
	}

	var spiffeSocketEnvVar = corev1.EnvVar{
		Name:  spiffeWLSocketEnvName,
		Value: spiffeWLSocket,
	}

	// Check for a debug annotation
	debugAnnotationValue, debugAnnotationExists := pod.Annotations[debugAnnotation]

	if debugAnnotationExists && debugAnnotationValue == "true" {
		if !containerExists(pod.Spec.Containers, debugUIContainerName) {
			logger.Info("Adding SPIFFE Enable debug UI container", "containerName", debugUIContainerName)
			debugSidecar := corev1.Container{
				Name:            debugUIContainerName,
				Image:           debugUIImage,
				ImagePullPolicy: corev1.PullAlways,
				Ports: []corev1.ContainerPort{
					{ContainerPort: debugUIPort},
				},
			}
			pod.Spec.Containers = append(pod.Spec.Containers, debugSidecar)
		}
	}

	// Process each (standard) container in the pod
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		// Add CSI volume mounts
		ensureCSIVolumeMount(container, spiffeVolumeMount, logger)
		// Add SPIFFE socket environment variable
		ensureEnvVar(container, spiffeSocketEnvVar)
	}

	// Check for an inject annotation and process based on the value
	injectAnnotationValue, injectAnnotationExists := pod.Annotations[injectAnnotation]

	allowedModes := map[string]bool{
		injectAnnotationHelper: true,
		injectAnnotationProxy:  true,
	}

	var invalidModes []string

	if injectAnnotationExists {
		toInject := strings.Split(injectAnnotationValue, ",")

		// First check that the desired injections are permitted
		for _, mode := range toInject {
			trimmedMode := strings.TrimSpace(mode)
			if trimmedMode == "" {
				continue
			}

			if _, isValid := allowedModes[trimmedMode]; !isValid {
				invalidModes = append(invalidModes, trimmedMode)
			}
		}

		if len(invalidModes) > 0 {
			err := fmt.Errorf(
				"invalid mode(s) found in injection list: %v. Allowed modes are: %v",
				strings.Join(invalidModes, ", "),
				getKeys(allowedModes),
			)
			logger.Error(err, "Pod rejected due to invalid injection modes", "providedModes", injectAnnotationValue, "invalidFound", invalidModes)
			return admission.Denied(err.Error())
		}

		// Now iterate the injections and apply
		for _, mode := range toInject {
			switch mode {
			case injectAnnotationProxy:
				// Generate the Envoy configuration
				configParams := proxy.EnvoyConfigParams{
					NodeID:          "node",
					ClusterName:     "cluster",
					AdminPort:       9901,
					AgentXDSService: agentXDSService,
					AgentXDSPort:    agentXDSPort,
				}

				envoyConfig, err := proxy.NewEnvoyConfig(configParams)
				if err != nil {
					logger.Error(err, "Error creating proxy config")
					return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error creating proxy config: %w", err))
				}

				envoyConfigJSON, err := json.MarshalIndent(envoyConfig, "", "  ")
				if err != nil {
					logger.Error(err, "Error marshalling proxy config to JSON")
					return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error marshalling proxy config to JSON: %w", err))
				}

				// Add an emptyDir volume for the Envoy proxy configuration if it doesn't already exist
				if !volumeExists(pod, proxy.EnvoyConfigVolumeName) {
					logger.Info("Adding Envoy config volume", "volumeName", proxy.EnvoyConfigVolumeName)
					pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
						Name:         proxy.EnvoyConfigVolumeName,
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					})
				}

				configFilePath := filepath.Join(proxy.EnvoyConfigMountPath, proxy.EnvoyConfigFileName)

				// Add an init container to write out the Envoy config to a file
				if !initContainerExists(pod, proxy.EnvoyConfigInitContainerName) {
					logger.Info("Adding init container to inject Envoy config", "initContainerName", proxy.EnvoyConfigInitContainerName)

					// This command writes out an Envoy config file based on the contents of the environment variable
					envoyConfigCmd := fmt.Sprintf("mkdir -p %s && printf '%%s' \"${%s}\" > %s",
						filepath.Dir(configFilePath),
						proxy.EnvoyConfigContentEnvVar,
						configFilePath)

					cmd := fmt.Sprintf("set -e; %s && %s", envoyConfigCmd, envoyConfig.InitScript)

					initContainer := corev1.Container{
						Name:            proxy.EnvoyConfigInitContainerName,
						Image:           initHelperImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/sh", "-c"},
						Args:            []string{cmd},
						Env:             []corev1.EnvVar{{Name: proxy.EnvoyConfigContentEnvVar, Value: string(envoyConfigJSON)}},
						VolumeMounts:    []corev1.VolumeMount{{Name: proxy.EnvoyConfigVolumeName, MountPath: filepath.Dir(configFilePath)}},
						SecurityContext: &corev1.SecurityContext{
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"NET_ADMIN"}, // # NET_ADMIN is required to apply nftables rules
							},
							RunAsUser: ptr.To(int64(0)), // # Run as root in order to apply nftables rules
						},
					}
					pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)
				}

				// Add the Envoy container as a sidecar
				if !containerExists(pod.Spec.Containers, proxy.EnvoySidecarContainerName) {
					logger.Info("Adding Envoy proxy sidecar container", "containerName", proxy.EnvoySidecarContainerName)
					envoySidecar := corev1.Container{
						Name:            proxy.EnvoySidecarContainerName,
						Image:           proxy.EnvoyImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"envoy"},
						Args:            []string{"-c", configFilePath},
						VolumeMounts:    []corev1.VolumeMount{{Name: proxy.EnvoyConfigVolumeName, MountPath: proxy.EnvoyConfigMountPath}},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:    ptr.To(int64(101)), // # Run as non-root user
							RunAsGroup:   ptr.To(int64(101)), // # Run as non-root group
							RunAsNonRoot: ptr.To(true),
						},
						Ports: []corev1.ContainerPort{
							{ContainerPort: envoyProxyPort},
						},
					}
					pod.Spec.Containers = append(pod.Spec.Containers, envoySidecar)
				}

			case injectAnnotationHelper:
				// Inject a spiffe-helper sidecar container
				logger.Info("Applying 'helper' mode mutations")

				// Add an emptyDir volume for the SPIFFE Helper configuration if it doesn't already exist
				if !volumeExists(pod, helper.SPIFFEHelperConfigVolumeName) {
					logger.Info("Adding SPIFFE helper config volume", "volumeName", helper.SPIFFEHelperConfigVolumeName)
					pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
						Name:         helper.SPIFFEHelperConfigVolumeName,
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					})
				}

				// Add an emptyDir volume for the certs managed by SPIFFE Helper
				if !volumeExists(pod, spiffeEnableCertVolumeName) {
					logger.Info("Adding SPIFFE helper certs volume", "volumeName", spiffeEnableCertVolumeName)
					pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
						Name:         spiffeEnableCertVolumeName,
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					})
				}

				incIntermediateBundle := false
				incIntermediateValue, incIntermediateExists := pod.Annotations[spiffeHelperIncIntermediateAnnotation]
				if incIntermediateExists && incIntermediateValue == "true" {
					incIntermediateBundle = true
				}

				// Generate the spiffe-helper configuration
				configParams := helper.SPIFFEHelperConfigParams{
					AgentAddress:              spiffeWLSocketPath,
					CertPath:                  spiffeEnableCertDirectory,
					IncludeIntermediateBundle: incIntermediateBundle,
				}

				spiffeHelperConfig, err := helper.NewSPIFFEHelperConfig(configParams)
				if err != nil {
					logger.Error(err, "Error creating spiffe-helper config")
					return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error creating spiffe-helper config: %w", err))
				}

				if !initContainerExists(pod, helper.SPIFFEHelperInitContainerName) {
					logger.Info("Adding init container to inject spiffe-helper config", "initContainerName", helper.SPIFFEHelperInitContainerName)
					configFilePath := filepath.Join(helper.SPIFFEHelperConfigMountPath, helper.SPIFFEHelperConfigFileName)
					writeCmd := fmt.Sprintf("mkdir -p %s && printf %%s \"$${%s}\" > %s && echo -e \"\\n=== SPIFFE Helper Config ===\" && cat %s && echo -e \"\\n===========================\"",
						filepath.Dir(configFilePath),
						helper.SPIFFEHelperConfigContentEnvVar,
						configFilePath,
						configFilePath)

					initContainer := corev1.Container{
						Name:            helper.SPIFFEHelperInitContainerName,
						Image:           initHelperImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/sh", "-c"},
						Args:            []string{writeCmd},
						Env:             []corev1.EnvVar{{Name: helper.SPIFFEHelperConfigContentEnvVar, Value: spiffeHelperConfig.Cfg}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: helper.SPIFFEHelperConfigVolumeName, MountPath: filepath.Dir(configFilePath)},
							{Name: spiffeEnableCertVolumeName, MountPath: spiffeEnableCertDirectory},
						},
					}
					pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)
				}

				if !containerExists(pod.Spec.Containers, helper.SPIFFEHelperSidecarContainerName) {
					logger.Info("Adding SPIFFE Helper sidecar container", "containerName", helper.SPIFFEHelperSidecarContainerName)
					helperSidecar := corev1.Container{
						Name:            helper.SPIFFEHelperSidecarContainerName,
						Image:           spiffeHelperImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Args:            []string{"-config", filepath.Join(helper.SPIFFEHelperConfigMountPath, helper.SPIFFEHelperConfigFileName)},
						VolumeMounts: []corev1.VolumeMount{
							{Name: helper.SPIFFEHelperConfigVolumeName, MountPath: helper.SPIFFEHelperConfigMountPath, ReadOnly: true},
							{Name: spiffeEnableCertVolumeName, MountPath: spiffeEnableCertDirectory},
							spiffeVolumeMount,
						},
					}
					pod.Spec.Containers = append(pod.Spec.Containers, helperSidecar)
				}
			}
		}
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "Failed to marshal modified pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func getKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func ensureCSIVolumeMount(container *corev1.Container, targetMount corev1.VolumeMount, logger logr.Logger) bool {
	madeChange := false
	mountExists := false
	mountIndex := -1 // Index of the mount if found by name and path

	for i, vm := range container.VolumeMounts {
		if vm.Name == targetMount.Name && vm.MountPath == targetMount.MountPath {
			mountIndex = i
			if vm.ReadOnly == targetMount.ReadOnly {
				mountExists = true // Exact match found
			}
			break // Found the mount by name and path, no need to search further
		}
	}

	if !mountExists {
		if mountIndex != -1 {
			// Mount exists with the same name and path, but ReadOnly differs. Update it.
			logger.Info("Updating ReadOnly status for existing VolumeMount",
				"containerName", container.Name, "volumeMountName", targetMount.Name, "newReadOnly", targetMount.ReadOnly)
			container.VolumeMounts[mountIndex].ReadOnly = targetMount.ReadOnly
			madeChange = true
		} else {
			// Mount does not exist at all, append it.
			logger.Info("Adding new VolumeMount to container",
				"containerName", container.Name, "volumeMountName", targetMount.Name)
			container.VolumeMounts = append(container.VolumeMounts, targetMount)
			madeChange = true
		}
	}
	return madeChange
}

func ensureEnvVar(container *corev1.Container, envVar corev1.EnvVar) {
	if !envVarExists(container, envVar.Name) {
		container.Env = append(container.Env, envVar)
	}
}
