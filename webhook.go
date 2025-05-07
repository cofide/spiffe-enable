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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const enabledAnnotation = "spiffe.cofide.io/enabled"
const modeAnnotation = "spiffe.cofide.io/mode"
const modeAnnotationDefault = modeAnnotationHelper
const modeAnnotationHelper = "helper"
const modeAnnotationProxy = "proxy"

//const awsRoleArnAnnotation = "spiffe.cofide.io/aws-role-arn"

const agentXDSPort = 18001
const envoyProxyPort = 10000

const spiffeWLVolume = "spiffe-workload-api"
const spiffeWLMountPath = ""
const spiffeWLSocketEnvName = "SPIFFE_ENDPOINT_SOCKET"
const spiffeWLSocket = "unix:///spiffe-workload-api/spire-agent.sock"
const spiffeWLSocketPath = "/spiffe-workload-api/spire-agent.sock"

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

const envoyConfigTemplate = `
node:
  id: node
  cluster: cluster

# Dynamic resource configuration
dynamic_resources:
  # Configure ADS (Aggregated Discovery Service)
  ads_config:
    api_type: GRPC
    transport_api_version: V3 # Use the v3 xDS API
    grpc_services:
      - envoy_grpc:
          cluster_name: xds_cluster
    set_node_on_first_message_only: true # Optimization for ADS

  # Configure CDS (Cluster Discovery Service) to use ADS
  cds_config:
    resource_api_version: V3
    ads: {} 

  lds_config:
    resource_api_version: V3
    ads: {} 

static_resources:
  clusters:
    - name: xds_cluster 
      type: LOGICAL_DNS 
      connect_timeout: 5s
      # xDS uses gRPC, which requires HTTP/2
      typed_extension_protocol_options:
        envoy.extensions.upstreams.http.v3.HttpProtocolOptions:
          "@type": type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions
          explicit_http_config:
            http2_protocol_options: {} # Enable HTTP/2

      load_assignment:
        cluster_name: xds_cluster
        endpoints:
          - lb_endpoints:
              - endpoint:
                  address:
                    socket_address:
                      address: 127.0.0.1
                      port_value: {{ AgentXDSPort }}
`

type spiffeHelperTemplateData struct {
	AgentAddresss string
}

type spiffeEnable struct {
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

func NewSpiffeEnable(client client.Client, log logr.Logger) (*spiffeEnable, error) {
	tmpl, err := template.New("spiffeHelperConfig").Parse(spiffeHelperConfigTemplate)
	if err != nil {
		log.Error(err, "Failed to parse spiffe-helper config template")
		return nil, fmt.Errorf("failed to parse spiffe-helper config template: %w", err)
	}
	return &spiffeEnable{
		Client:                 client,
		Log:                    log,
		spiffeHelperConfigTmpl: tmpl,
	}, nil
}

func (a *spiffeEnable) Handle(ctx context.Context, req admission.Request) admission.Response {
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

	// Process each (standard) container in the pod
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		// Add CSI volume mounts
		ensureCSIVolumeMount(container, spiffeVolumeMount, logger)
		// Add SPIFFE socket environment variable
		ensureEnvVar(container, spiffeSocketEnvVar, logger)
	}

	// Check for a mode annotation and process based on the value
	modeAnnotationValue, modeAnnotationExists := pod.Annotations[modeAnnotation]

	if modeAnnotationExists {
		if modeAnnotationValue != modeAnnotationHelper && modeAnnotationValue != modeAnnotationProxy {
			err := fmt.Errorf(
				"invalid value %q for annotation %q; allowed values are %q or %q",
				modeAnnotationValue,
				modeAnnotation,
				modeAnnotationHelper,
				modeAnnotationProxy,
			)
			logger.Error(err, "Pod rejected due to invalid annotation value", "annotationKey", modeAnnotation, "providedValue", modeAnnotationValue)
			return admission.Denied(err.Error())
		}

		switch modeAnnotationValue {
		case modeAnnotationHelper:
			logger.Info("Applying 'helper' mode mutations")

			// Add an emptyDir volume for the SPIFFE Helper configiuration if it doesn't already exist
			if !volumeExists(pod, spiffeHelperConfigVolumeName) {
				logger.Info("Adding SPIFFE helper config volume", "volumeName", spiffeHelperConfigVolumeName)
				pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
					Name:         spiffeHelperConfigVolumeName,
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				})
			}

			templateData := spiffeHelperTemplateData{
				AgentAddresss: spiffeWLSocketPath,
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

			if !containerExists(pod.Spec.Containers, spiffeHelperSidecarContainerName) {
				logger.Info("Adding SPIFFE Helper sidecar container", "containerName", spiffeHelperSidecarContainerName)
				helperSidecar := corev1.Container{
					Name:            spiffeHelperSidecarContainerName,
					Image:           spiffeHelperImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            []string{"-config", filepath.Join(spiffeHelperConfigMountPath, spiffeHelperConfigFileName)},
					VolumeMounts: []corev1.VolumeMount{
						{Name: spiffeHelperConfigVolumeName, MountPath: spiffeHelperConfigMountPath, ReadOnly: true},
						spiffeVolumeMount,
					},
				}
				pod.Spec.Containers = append(pod.Spec.Containers, helperSidecar)
			}

			/*

				if injectAnnotationExists && injectAnnotationValue == "true" &&
					roleArnExists && roleArValue != "" {s

					/ze/ add the AWS SDK env var
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

					// Add the new container to the pod
					pod.Spec.Containers = append(pod.Spec.Containers, sidecarContainer)
				}
			*/
		}
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "Failed to marshal modified pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (a *spiffeEnable) InjectDecoder(d admission.Decoder) error {
	a.decoder = d
	return nil
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

func ensureEnvVar(container *corev1.Container, envVar corev1.EnvVar, logger logr.Logger) bool {
	container.Env = append(container.Env, envVar)
	return true
}
