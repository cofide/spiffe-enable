package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"text/template"

	"github.com/cofide/spiffe-enable/internal/helper"
	"github.com/cofide/spiffe-enable/internal/workload"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// Envoy-specific constants
var (
	IstioImage = "docker.io/istio/proxyv2:1.26.4"
)

const (
	EnvoySidecarContainerName    = "envoy-sidecar"
	EnvoyConfigVolumeName        = "envoy-config"
	EnvoyConfigMountPath         = "/etc/envoy"
	EnvoyConfigFileName          = "envoy.yaml"
	EnvoyConfigContentEnvVar     = "ENVOY_CONFIG_CONTENT"
	EnvoyConfigInitContainerName = "inject-envoy-config"
	EnvoyPort                    = 10000
	EnvoyUID                     = 1337
	DNSProxyPort                 = 15053
)

type NftablesParams struct {
	EnvoyUID     int
	EnvoyPort    int
	DNSProxyPort int
}

const nftablesSetupScript = `
if ! command -v nft &> /dev/null; then
    echo "nftables (nft) is not installed"
    exit 1
fi

# These nftables rules intercept DNS requests (UDP+TCP)
# and redirect to a DNS proxy provided by Envoy
cat <<EOF > /tmp/dns_redirect.nft
table inet envoy_proxy {
	chain envoy_output {
        type nat hook output priority dstnat; policy accept;

        # Skip Envoy's own traffic
        meta skuid == {{.EnvoyUID}} return

        # DNS redirection
        udp dport 53 counter redirect to :{{.DNSProxyPort}} comment "DNS UDP to Envoy"
        tcp dport 53 counter redirect to :{{.DNSProxyPort}} comment "DNS TCP to Envoy"

        # Skip traffic already going to Envoy port
        tcp dport {{.EnvoyPort}} return
        tcp dport 9901 return

        # Redirect loopback TCP traffic (using tcp dport range to match all TCP)
        ip daddr 127.0.0.1/8 tcp dport 1-65535 counter redirect to :{{.EnvoyPort}} comment "Loopback IPv4 to Envoy"
        ip6 daddr ::1/128 tcp dport 1-65535 counter redirect to :{{.EnvoyPort}} comment "Loopback IPv6 to Envoy"
    }
}
EOF

# Apply the nftables rules from the created file
nft -f /tmp/dns_redirect.nft
echo "nftables DNS redirection rules applied."

echo "Applied rules:"
nft list table inet envoy_proxy
`

type EnvoyConfigParams struct {
	NodeID          string
	ClusterName     string
	AdminAddress    string
	AdminPort       uint32
	AgentXDSService string
	AgentXDSPort    uint32
}

type Envoy struct {
	InitScript string
	Cfg        []byte
}

func NewEnvoy(params EnvoyConfigParams) (*Envoy, error) {
	if params.NodeID == "" {
		params.NodeID = "node"
	}
	if params.ClusterName == "" {
		params.ClusterName = "cluster"
	}
	if params.AdminAddress == "" {
		params.AdminAddress = "127.0.0.1"
	}
	if params.AdminPort == 0 {
		params.AdminPort = 9901
	}

	cfg := map[string]interface{}{
		"node": map[string]interface{}{
			"id":      params.NodeID,
			"cluster": params.ClusterName,
		},
		"admin": map[string]interface{}{
			"address": map[string]interface{}{
				"socket_address": map[string]interface{}{
					"address":    params.AdminAddress,
					"port_value": params.AdminPort,
				},
			},
		},
		"dynamic_resources": map[string]interface{}{
			"ads_config": map[string]interface{}{
				"api_type":              "GRPC",
				"transport_api_version": "V3",
				"grpc_services": []interface{}{
					map[string]interface{}{
						"envoy_grpc": map[string]interface{}{
							"cluster_name": "xds_cluster",
						},
					},
				},
				"set_node_on_first_message_only": true,
			},
			"cds_config": map[string]interface{}{
				"resource_api_version": "V3",
				"ads":                  map[string]interface{}{},
			},
			"lds_config": map[string]interface{}{
				"resource_api_version": "V3",
				"ads":                  map[string]interface{}{},
			},
		},
		"static_resources": map[string]interface{}{
			"clusters": []interface{}{
				map[string]interface{}{
					"name":            "xds_cluster",
					"type":            "LOGICAL_DNS",
					"connect_timeout": "5s",
					"typed_extension_protocol_options": map[string]interface{}{
						"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": map[string]interface{}{
							"@type": "type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions",
							"explicit_http_config": map[string]interface{}{
								"http2_protocol_options": map[string]interface{}{},
							},
						},
					},
					"load_assignment": map[string]interface{}{
						"cluster_name": "xds_cluster",
						"endpoints": []interface{}{
							map[string]interface{}{
								"lb_endpoints": []interface{}{
									map[string]interface{}{
										"endpoint": map[string]interface{}{
											"address": map[string]interface{}{
												"socket_address": map[string]interface{}{
													"address":    params.AgentXDSService,
													"port_value": params.AgentXDSPort,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	nftTablesParams := NftablesParams{
		EnvoyUID:     EnvoyUID,
		EnvoyPort:    EnvoyPort,
		DNSProxyPort: DNSProxyPort,
	}

	tmpl, err := template.New("initScript").Parse(nftablesSetupScript)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nftables init script template: %w", err)
	}

	var renderedScript bytes.Buffer
	if err := tmpl.Execute(&renderedScript, nftTablesParams); err != nil {
		return nil, fmt.Errorf("failed to render nftables init script template with params: %w", err)
	}

	envoyConfigJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("error marshalling proxy config to JSON")
	}

	return &Envoy{InitScript: renderedScript.String(), Cfg: envoyConfigJSON}, nil
}

func (e *Envoy) GetConfigVolume() corev1.Volume {
	return corev1.Volume{
		Name:         EnvoyConfigVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

func (e *Envoy) GetInitContainer() corev1.Container {
	configFilePath := filepath.Join(EnvoyConfigMountPath, EnvoyConfigFileName)

	// This command writes out an Envoy config file based on the contents of the environment variable
	envoyConfigCmd := fmt.Sprintf("mkdir -p %s && printf '%%s' \"${%s}\" > %s",
		filepath.Dir(configFilePath),
		EnvoyConfigContentEnvVar,
		configFilePath)

	cmd := fmt.Sprintf("set -e; %s && %s", envoyConfigCmd, e.InitScript)

	return corev1.Container{
		Name:            EnvoyConfigInitContainerName,
		Image:           helper.InitHelperImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-c"},
		Args:            []string{cmd},
		Env:             []corev1.EnvVar{{Name: EnvoyConfigContentEnvVar, Value: string(e.Cfg)}},
		VolumeMounts:    []corev1.VolumeMount{{Name: EnvoyConfigVolumeName, MountPath: filepath.Dir(configFilePath)}},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"NET_ADMIN", "NET_RAW"}, // # Additional capabilities required to apply nftables rules

			},
			RunAsUser:    ptr.To(int64(0)), // # Run as root in order to apply nftables rules
			RunAsNonRoot: ptr.To(false),
		},
	}
}

func (e *Envoy) GetSidecarContainer() corev1.Container {
	configFilePath := filepath.Join(EnvoyConfigMountPath, EnvoyConfigFileName)

	return corev1.Container{
		Name:            EnvoySidecarContainerName,
		Image:           IstioImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"envoy"},
		Args:            []string{"-c", configFilePath},
		VolumeMounts: []corev1.VolumeMount{
			{Name: EnvoyConfigVolumeName, MountPath: EnvoyConfigMountPath},
			workload.GetSPIFFEVolumeMount(),
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			RunAsUser:                ptr.To(int64(EnvoyUID)), // # Run as non-root user
			RunAsGroup:               ptr.To(int64(EnvoyUID)), // # Run as non-root group
			RunAsNonRoot:             ptr.To(true),
			Privileged:               ptr.To(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"all"}},
		},
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: EnvoyPort,
			},
		},
	}
}
