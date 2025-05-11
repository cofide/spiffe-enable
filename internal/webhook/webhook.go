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
const modeAnnotationHelper = "helper"
const modeAnnotationProxy = "proxy"

const spiffeWLVolume = "spiffe-workload-api"
const spiffeWLMountPath = "/spiffe-workload-api"
const spiffeWLSocketEnvName = "SPIFFE_ENDPOINT_SOCKET"
const spiffeWLSocket = "unix:///spiffe-workload-api/spire-agent.sock"
const spiffeWLSocketPath = "/spiffe-workload-api/spire-agent.sock"

const agentXDSPort = 18001
const envoyProxyPort = 10000
const envoySidecarContainerName = "envoy-sidecar"
const envoyConfigVolumeName = "envoy-config"
const envoyConfigMountPath = "/etc/envoy"
const envoyConfigFileName = "envoy.yaml"
const envoyConfigContentEnvVar = "ENVOY_CONFIG_CONTENT"

const envoyConfigInitContainerName = "inject-envoy-config"

const spiffeHelperConfigVolumeName = "spiffe-helper-config"
const spiffeHelperSidecarContainerName = "spiffe-helper"
const spiffeHelperConfigContentEnvVar = "SPIFFE_HELPER_CONFIG"
const spiffeHelperConfigMountPath = "/etc/spiffe-helper"
const spiffeHelperConfigFileName = "config.conf"
const spiffeHelperInitContainerName = "inject-spiffe-helper-config"

var envoyImage = "envoyproxy/envoy:v1.33-latest"
var spiffeHelperImage = "ghcr.io/spiffe/spiffe-helper:0.10.0"
var initHelperImage = "cofide/spiffe-enable-init:latest"

var spiffeHelperConfigTemplate = `
    agent_address = {{ .AgentAddress }}
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

type spiffeHelperTemplateData struct {
	AgentAddress string
}

var envoyConfigTemplate = `
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
                      port_value: {{ .AgentXDSPort }}
`

var nftablesSetupScript = `
cat <<EOF > /tmp/dns_redirect.nft
#!/usr/sbin/nft -f
define ENVOY_PROXY_UID = 1337
define ENVOY_DNS_LISTEN_PORT = 15053
define K8S_DNS_PORT = 53
define DNS_TABLE_NAME = envoy_dns_interception_webhook # Using a distinct table name

# Delete the table if it exists to ensure a clean state for these specific rules.
# "2>/dev/null || true" suppresses errors if the table doesn't exist.
delete table inet \$DNS_TABLE_NAME 2>/dev/null || true

# Add (create) our dedicated table.
add table inet \$DNS_TABLE_NAME

# Add the chain to our table for NAT output redirection.
add chain inet \$DNS_TABLE_NAME redirect_dns_output {
    type nat hook output priority -100; policy accept;
}

# Add the DNS redirection rules.
add rule inet \$DNS_TABLE_NAME redirect_dns_output meta skuid != \$ENVOY_PROXY_UID udp dport \$K8S_DNS_PORT redirect to :\$ENVOY_DNS_LISTEN_PORT comment "Webhook: UDP DNS to Envoy"
add rule inet \$DNS_TABLE_NAME redirect_dns_output meta skuid != \$ENVOY_PROXY_UID tcp dport \$K8S_DNS_PORT redirect to :\$ENVOY_DNS_LISTEN_PORT comment "Webhook: TCP DNS to Envoy"
EOF
# Apply the nftables rules from the created file
nft -f /tmp/dns_redirect.nft
echo "nftables DNS redirection rules applied."
`

type envoyTemplateData struct {
	AgentXDSPort int
}

type spiffeEnableWebhook struct {
	Client                 client.Client
	decoder                admission.Decoder
	Log                    logr.Logger
	spiffeHelperConfigTmpl *template.Template
	envoyConfigTmpl        *template.Template
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

	envoyTmpl, err := template.New("envoyConfig").Parse(envoyConfigTemplate)
	if err != nil {
		log.Error(err, "Failed to parse Envoy config template")
		return nil, fmt.Errorf("failed to parse Envoy config template: %w", err)
	}

	return &spiffeEnableWebhook{
		Client:                 client,
		Log:                    log,
		decoder:                decoder,
		spiffeHelperConfigTmpl: spiffeHelperTmpl,
		envoyConfigTmpl:        envoyTmpl,
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
		// Inject an Envoy proxy sidecar container
		case modeAnnotationProxy:
			templateData := envoyTemplateData{
				AgentXDSPort: agentXDSPort,
			}

			var configBuf bytes.Buffer
			if err := a.envoyConfigTmpl.Execute(&configBuf, templateData); err != nil {
				logger.Error(err, "Failed to execute Envoy config template")
				return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to template Envoy config: %w", err))
			}
			envoyConfig := configBuf.String()

			// Add an init container to write out the Envoy config to a file
			if !initContainerExists(pod, envoyConfigInitContainerName) {
				logger.Info("Adding init container to inject Envoy config", "initContainerName", envoyConfigInitContainerName)
				configFilePath := filepath.Join(envoyConfigMountPath, envoyConfigFileName)

				// This command writes out an Envoy config file based on the contents of the environment variable
				envoyConfigCmd := fmt.Sprintf("mkdir -p %s && printf '%%s' \"${%s}\" > %s",
					filepath.Dir(configFilePath),
					envoyConfigContentEnvVar,
					configFilePath)

				cmd := fmt.Sprintf("set -e; %s && %s", envoyConfigCmd, nftablesSetupScript)

				initContainer := corev1.Container{
					Name:            envoyConfigInitContainerName,
					Image:           initHelperImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"/bin/sh", "-c"},
					Args:            []string{cmd},
					Env:             []corev1.EnvVar{{Name: envoyConfigContentEnvVar, Value: envoyConfig}},
					VolumeMounts:    []corev1.VolumeMount{{Name: envoyConfigVolumeName, MountPath: filepath.Dir(configFilePath)}},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{"NET_ADMIN"},
						},
						RunAsUser: ptr.To(int64(0)), // # Run as root to apply nftables rules
					},
				}
				pod.Spec.InitContainers = append([]corev1.Container{initContainer}, pod.Spec.InitContainers...)
			}

			// Add the Envoy container as a sidecar
			if !containerExists(pod.Spec.Containers, envoySidecarContainerName) {
				logger.Info("Adding Envoy proxy sidecar container", "containerName", envoySidecarContainerName)
				envoySidecar := corev1.Container{
					Name:            envoySidecarContainerName,
					Image:           envoyImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"envoy"},
					Args:            []string{"-c", "/etc/envoy/envoy.yaml"},
					VolumeMounts:    []corev1.VolumeMount{{Name: envoyConfigVolumeName, MountPath: envoyConfigMountPath}},
					Ports: []corev1.ContainerPort{
						{ContainerPort: envoyProxyPort},
					},
				}
				pod.Spec.Containers = append(pod.Spec.Containers, envoySidecar)
			}

		// Inject a spiffe-helper sidecar container
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
				AgentAddress: spiffeWLSocketPath,
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

func ensureEnvVar(container *corev1.Container, envVar corev1.EnvVar, logger logr.Logger) {
	if !envVarExists(container, envVar.Name) {
		container.Env = append(container.Env, envVar)
	}
}
