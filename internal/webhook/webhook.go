package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"text/template"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const enabledAnnotation = "spiffe.cofide.io/enabled"
const modeAnnotation = "spiffe.cofide.io/mode"
const debugAnnotation = "spiffe.cofide.io/debug"

const modeAnnotationHelper = "helper"

const spiffeHelperIncIntermediateAnnotation = "spiffe.cofide.io/spiffe-helper-include-intermediate-bundle"

const spiffeWLVolume = "spiffe-workload-api"
const spiffeWLMountPath = "/spiffe-workload-api"
const spiffeWLSocketEnvName = "SPIFFE_ENDPOINT_SOCKET"
const spiffeWLSocket = "unix:///spiffe-workload-api/spire-agent.sock"
const spiffeWLSocketPath = "/spiffe-workload-api/spire-agent.sock"

const spiffeHelperConfigVolumeName = "spiffe-helper-config"
const spiffeHelperSidecarContainerName = "spiffe-helper"
const spiffeHelperConfigContentEnvVar = "SPIFFE_HELPER_CONFIG"
const spiffeHelperConfigMountPath = "/etc/spiffe-helper"
const spiffeHelperConfigFileName = "config.conf"
const spiffeHelperInitContainerName = "inject-spiffe-helper-config"

const spiffeEnableCertVolumeName = "spiffe-enable-certs"
const spiffeEnableCertDirectory = "/spiffe-enable"

const debugUIContainerName = "spiffe-enable-ui"
const debugUIPort = 8000

var spiffeHelperImage = "ghcr.io/spiffe/spiffe-helper:0.10.0"
var initHelperImage = "010438484483.dkr.ecr.eu-west-1.amazonaws.com/cofide/spiffe-enable-init:v0.1.0-alpha"
var debugUIImage = "010438484483.dkr.ecr.eu-west-1.amazonaws.com/cofide/spiffe-enable-ui:v0.1.0-alpha"

var spiffeHelperConfigTemplate = `
agent_address = "{{ .AgentAddress }}"
include_federated_domains = true
{{ if .IncludeIntermediateBundle }}
add_intermediates_to_bundle = true
{{ end }}
cmd = ""
cmd_args = ""
cert_dir = "{{ .CertPath }}"
renew_signal = ""
svid_file_name = "tls.crt"
svid_key_file_name = "tls.key"
svid_bundle_file_name = "ca.pem"
jwt_bundle_file_name = "cert.jwt"
jwt_svids = [{jwt_audience="aud", jwt_svid_file_name="jwt_svid.token"}]
daemon_mode = true
`

type spiffeHelperTemplateData struct {
	AgentAddress              string
	CertPath                  string
	IncludeIntermediateBundle bool
}

type spiffeEnableWebhook struct {
	Client                 client.Client
	decoder                admission.Decoder
	Log                    logr.Logger
	spiffeHelperConfigTmpl *template.Template
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
	spiffeHelperTmpl, err := template.New("spiffeHelperConfig").Parse(spiffeHelperConfigTemplate)
	if err != nil {
		log.Error(err, "Failed to parse spiffe-helper config template")
		return nil, fmt.Errorf("failed to parse spiffe-helper config template: %w", err)
	}

	return &spiffeEnableWebhook{
		Client:                 client,
		Log:                    log,
		decoder:                decoder,
		spiffeHelperConfigTmpl: spiffeHelperTmpl,
	}, nil
}

func (a *spiffeEnableWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := a.decoder.Decode(req, pod); err != nil {
		a.Log.Error(err, "Failed to decode pod", "request", req.UID)
		return admission.Errored(http.StatusBadRequest, err)
	}

	logger := a.Log.WithValues("podNamespace", pod.Namespace, "podName", pod.Name, "request", req.UID)

	injectAnnotationValue, injectAnnotationExists := pod.Annotations[enabledAnnotation]
	spiffeInjectionEnabled := injectAnnotationExists && injectAnnotationValue == "true"

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

	logger.Info("Observed pod annotations", "annotations", pod.Annotations)

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

	// Check for a mode annotation and process based on the value
	modeAnnotationValue, modeAnnotationExists := pod.Annotations[modeAnnotation]

	if modeAnnotationExists {
		if modeAnnotationValue != modeAnnotationHelper {
			err := fmt.Errorf(
				"invalid value %q for annotation %q; allowed values is %q",
				modeAnnotationValue,
				modeAnnotation,
				modeAnnotationHelper,
			)
			logger.Error(err, "Pod rejected due to invalid annotation value", "annotationKey", modeAnnotation, "providedValue", modeAnnotationValue)
			return admission.Denied(err.Error())
		}

		switch modeAnnotationValue {
		// Inject a spiffe-helper sidecar container
		case modeAnnotationHelper:
			logger.Info("Applying 'helper' mode mutations")

			// Add an emptyDir volume for the SPIFFE Helper configuration if it doesn't already exist
			if !volumeExists(pod, spiffeHelperConfigVolumeName) {
				logger.Info("Adding SPIFFE helper config volume", "volumeName", spiffeHelperConfigVolumeName)
				pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
					Name:         spiffeHelperConfigVolumeName,
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

			templateData := spiffeHelperTemplateData{
				AgentAddress:              spiffeWLSocketPath,
				CertPath:                  spiffeEnableCertDirectory,
				IncludeIntermediateBundle: incIntermediateBundle,
			}

			var configBuf bytes.Buffer
			if err := a.spiffeHelperConfigTmpl.Execute(&configBuf, templateData); err != nil {
				logger.Error(err, "Failed to execute spiffe-helper config template")
				return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to template helper config: %w", err))
			}
			spiffeHelperConfig := configBuf.String()

			if !initContainerExists(pod, spiffeHelperInitContainerName) {
				logger.Info("Adding init container to inject spiffe-helper config", "initContainerName", spiffeHelperInitContainerName)
				configFilePath := filepath.Join(spiffeHelperConfigMountPath, spiffeHelperConfigFileName)
				writeCmd := fmt.Sprintf("mkdir -p %s && printf %%s \"$${%s}\" > %s && echo -e \"\\n=== SPIFFE Helper Config ===\" && cat %s && echo -e \"\\n===========================\"",
					filepath.Dir(configFilePath),
					spiffeHelperConfigContentEnvVar,
					configFilePath,
					configFilePath)

				initContainer := corev1.Container{
					Name:            spiffeHelperInitContainerName,
					Image:           initHelperImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"/bin/sh", "-c"},
					Args:            []string{writeCmd},
					Env:             []corev1.EnvVar{{Name: spiffeHelperConfigContentEnvVar, Value: spiffeHelperConfig}},
					VolumeMounts: []corev1.VolumeMount{
						{Name: spiffeHelperConfigVolumeName, MountPath: filepath.Dir(configFilePath)},
						{Name: spiffeEnableCertVolumeName, MountPath: spiffeEnableCertDirectory},
					},
				}
				pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)
			}

			if !containerExists(pod.Spec.Containers, spiffeHelperSidecarContainerName) {
				logger.Info("Adding SPIFFE Helper sidecar container", "containerName", spiffeHelperSidecarContainerName)
				helperSidecar := corev1.Container{
					Name:            spiffeHelperSidecarContainerName,
					Image:           spiffeHelperImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            []string{"-config", filepath.Join(spiffeHelperConfigMountPath, spiffeHelperConfigFileName)},
					VolumeMounts: []corev1.VolumeMount{
						{Name: spiffeHelperConfigVolumeName, MountPath: spiffeHelperConfigMountPath, ReadOnly: true},
						{Name: spiffeEnableCertVolumeName, MountPath: spiffeEnableCertDirectory},
						spiffeVolumeMount,
					},
				}
				pod.Spec.Containers = append(pod.Spec.Containers, helperSidecar)
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
