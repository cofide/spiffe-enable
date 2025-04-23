package main

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
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const enableAnnotation = "cofide.io/enable"
const modeAnnotation = "cofide.io/mode"
const awsRoleArnAnnotation = "cofide.io/aws-role-arn"

const spiffeWLVolume = "spiffe-workload-api"
const spiffeHelperConfigVolumeName = "spiffe-helper-config"
const spiffeHelperSidecarContainerName = "spiffe-helper"
const spiffeHelperConfigContentEnvVar = "SPIFFE_HELPER_CONFIG"
const spiffeHelperConfigMountPath = "/etc/spiffe-helper"
const spiffeHelperConfigFileName = "config.conf"
const spiffeHelperInitContainerName = "inject-spiffe-helper-config"

var spiffeHelperImage = "ghcr.io/spiffe/spiffe-helper:0.10.0"
var initHelperImage = "cgr.dev/chainguard/busybox:latest"

const spiffeHelperConfigTemplate = `
    agent_address = {{ AgentAddress }}
    include_federated_domains = true
    add_intermediates_to_bundle = true
    cmd = ""
    cmd_args = ""
    cert_dir = "/tmp"
    renew_signal = ""
    svid_file_name = "tls.crt"
    svid_key_file_name = "tls.key"
    svid_bundle_file_name = "ca.pem"
    jwt_bundle_file_name = "cert.jwt"
    jwt_svids = [{jwt_audience="test", jwt_svid_file_name="jwt_svid.token"}]
    daemon_mode = true
`

type spiffeHelperTemplateData struct {
	AgentAddresss string
}

type podAnnotator struct {
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

func NewPodAnnotator(client client.Client, log logr.Logger) (*podAnnotator, error) {
	tmpl, err := template.New("spiffeHelperConfig").Parse(spiffeHelperConfigTemplate)
	if err != nil {
		log.Error(err, "Failed to parse spiffe-helper config template")
		return nil, fmt.Errorf("failed to parse spiffe-helper config template: %w", err)
	}
	return &podAnnotator{
		Client:                 client,
		Log:                    log,
		spiffeHelperConfigTmpl: tmpl,
	}, nil
}

func (a *podAnnotator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := a.decoder.Decode(req, pod); err != nil {
		a.Log.Error(err, "Failed to decode pod", "request", req.UID)
		return admission.Errored(http.StatusBadRequest, err)
	}

	logger := a.Log.WithValues("podNamespace", pod.Namespace, "podName", pod.Name, "request", req.UID)

	injectAnnotationValue, injectAnnotationExists := pod.Annotations[enableAnnotation]
	spiffeInjectionEnabled := injectAnnotationExists && injectAnnotationValue == "true"

	if !spiffeInjectionEnabled {
		logger.Info("Skipping all injections, annotation not set or disabled", "annotation", enableAnnotation)
		return admission.Allowed("Injection criteria not met")
	}

	if !volumeExists(pod, spiffeWLVolume) {
		logger.Info("Adding SPIFFE CSI volume", "volumeName", spiffeWLVolume)
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name:         spiffeWLVolume,
			VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{Driver: "csi.spiffe.io", ReadOnly: pointer.Bool(true)}},
		})
	}

	if !volumeExists(pod, spiffeHelperConfigVolumeName) {
		logger.Info("Adding SPIFFE helper config volume", "volumeName", spiffeHelperConfigVolumeName)
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name:         spiffeHelperConfigVolumeName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	templateData := spiffeHelperTemplateData{
		AgentAddresss: "/spiffe-workload-api/spire-agent.sock",
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
		writeCmd := fmt.Sprintf("mkdir -p %s && printf %%s \"$${%s}\" > %s", filepath.Dir(configFilePath), spiffeHelperConfigContentEnvVar, configFilePath)

		initContainer := corev1.Container{
			Name:            spiffeHelperInitContainerName,
			Image:           initHelperImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-c"},
			Args:            []string{writeCmd},
			Env:             []corev1.EnvVar{{Name: spiffeHelperConfigContentEnvVar, Value: spiffeHelperConfig}},
			VolumeMounts:    []corev1.VolumeMount{{Name: spiffeHelperConfigVolumeName, MountPath: filepath.Dir(configFilePath)}},
		}
		pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)
	}

	/*
		if !containerExists(pod.Spec.Containers, helperSidecarContainerName) {
			logger.Info("Adding SPIFFE Helper sidecar container", "containerName", helperSidecarContainerName)
			helperSidecar := corev1.Container{
				Name:            helperSidecarContainerName,
				Image:           helperSidecarImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"-config", filepath.Join(helperConfigMountPath, helperConfigFileName)},
				VolumeMounts: []corev1.VolumeMount{
					{Name: helperConfigVolumeName, MountPath: helperConfigMountPath, ReadOnly: true},
					{Name: spiffeVolumeName, MountPath: "/spiffe-workload-api", ReadOnly: true},
				},
			}
			pod.Spec.Containers = append(pod.Spec.Containers, helperSidecar)
		}

			if injectAnnotationExists && injectAnnotationValue == "true" &&
				roleArnExists && roleArValue != "" {

				// add the AWS SDK env var
				credsURIEnvVar := corev1.EnvVar{
					Name:  "AWS_CONTAINER_CREDENTIALS_FULL_URI",
					Value: "http://127.0.0.1:8080/v1/credentials",
				}
				// add to the first container (naive for now..)
				pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, credsURIEnvVar)

				// Create the new container
				sidecarContainer := corev1.Container{
					Name:            "cofide-spiffe-iam-sidecar",
					Image:           "kind.local/aws-spiffe-iam-sidecar-effa1e319e451573b9fc06478801e519:latest",
					ImagePullPolicy: "IfNotPresent",
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 8080,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "AWS_ROLE_ARN",
							Value: roleArValue,
						},
						{
							Name:  "AWS_SESSION_NAME",
							Value: "consumer-workload-session",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "spiffe-workload-api",
							MountPath: "/spiffe-workload-api",
							ReadOnly:  true,
						},
						{
							Name:      "temp-token-volume",
							MountPath: "/token",
						},
					},
				}
				// add the temp in-memory volume
				pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
					Name: "temp-token-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							Medium: corev1.StorageMediumMemory,
						},
					},
				})
				// add the spiffe workload socket
				pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
					Name: "spiffe-workload-api",
					VolumeSource: corev1.VolumeSource{
						CSI: &corev1.CSIVolumeSource{
							Driver:   "csi.spiffe.io",
							ReadOnly: pointer.Bool(true),
						},
					},
				})
				// Add the new container to the pod
				pod.Spec.Containers = append(pod.Spec.Containers, sidecarContainer)
			}
	*/

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "Failed to marshal modified pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (a *podAnnotator) InjectDecoder(d admission.Decoder) error {
	a.decoder = d
	return nil
}
