package helper

import (
	"bytes"
	"fmt"
	"html/template"
)

// SPIFFE Helper constants
var (
	SPIFFEHelperImage = "ghcr.io/spiffe/spiffe-helper:0.10.0"
	InitHelperImage   = "010438484483.dkr.ecr.eu-west-1.amazonaws.com/cofide/spiffe-enable-init:v0.1.0-alpha"
)

const (
	SPIFFEHelperIncIntermediateAnnotation = "spiffe.cofide.io/spiffe-helper-include-intermediate-bundle"
	SPIFFEHelperConfigVolumeName          = "spiffe-helper-config"
	SPIFFEHelperSidecarContainerName      = "spiffe-helper"
	SPIFFEHelperConfigContentEnvVar       = "SPIFFE_HELPER_CONFIG"
	SPIFFEHelperConfigMountPath           = "/etc/spiffe-helper"
	SPIFFEHelperConfigFileName            = "config.conf"
	SPIFFEHelperInitContainerName         = "inject-spiffe-helper-config"
)

var spiffeHelperConfigTmpl = `
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

type SPIFFEHelperConfigParams struct {
	AgentAddress              string
	CertPath                  string
	IncludeIntermediateBundle bool
}

func NewSPIFFEHelperConfig(params SPIFFEHelperConfigParams) (*SPIFFEHelperConfig, error) {
	tmpl, err := template.New("spiffeHelperConfig").Parse(spiffeHelperConfigTmpl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse spiffe-helper config template: %w", err)
	}

	var renderedCfg bytes.Buffer
	if err := tmpl.Execute(&renderedCfg, params); err != nil {
		return nil, fmt.Errorf("failed to render spiffe-helper config template with params: %w", err)
	}

	return &SPIFFEHelperConfig{Cfg: renderedCfg.String()}, nil
}

type SPIFFEHelperConfig struct {
	Cfg string
}
