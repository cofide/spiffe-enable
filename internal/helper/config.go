package helper

import (
	"bytes"
	"fmt"
	"html/template"
	"path/filepath"

	constants "github.com/cofide/spiffe-enable/internal/const"
	"github.com/cofide/spiffe-enable/internal/workload"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	SPIFFEHelperHealthCheckReadinessPath  = "/ready"
	SPIFFEHelperHealthCheckLivenessPath   = "/live"
	SPIFFEHelperHealthCheckPort           = 8081
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
health_checks.listener_enabled = true
`

type SPIFFEHelperConfigParams struct {
	AgentAddress              string
	CertPath                  string
	IncludeIntermediateBundle bool
}

func NewSPIFFEHelper(params SPIFFEHelperConfigParams) (*SPIFFEHelper, error) {
	tmpl, err := template.New("spiffeHelperConfig").Parse(spiffeHelperConfigTmpl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse spiffe-helper config template: %w", err)
	}

	var renderedCfg bytes.Buffer
	if err := tmpl.Execute(&renderedCfg, params); err != nil {
		return nil, fmt.Errorf("failed to render spiffe-helper config template with params: %w", err)
	}

	return &SPIFFEHelper{Cfg: renderedCfg.String()}, nil
}

func (h SPIFFEHelper) GetConfigVolume() corev1.Volume {
	return corev1.Volume{
		Name:         SPIFFEHelperConfigVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

func (h SPIFFEHelper) GetSidecarContainer() corev1.Container {
	// Required in order for this sidecar to be native
	var restartPolicyAlways = corev1.ContainerRestartPolicyAlways

	return corev1.Container{
		Name:            SPIFFEHelperSidecarContainerName,
		Image:           SPIFFEHelperImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		RestartPolicy:   &restartPolicyAlways,
		Args:            []string{"-config", filepath.Join(SPIFFEHelperConfigMountPath, SPIFFEHelperConfigFileName)},
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   SPIFFEHelperHealthCheckReadinessPath,
					Port:   intstr.FromInt(SPIFFEHelperHealthCheckPort),
					Scheme: corev1.URISchemeHTTP,
				},
			},
			InitialDelaySeconds: 5,  // Start probing 5 seconds after the container starts
			PeriodSeconds:       5,  // Check every 5 seconds
			FailureThreshold:    10, // Consider the startup failed after 10 consecutive failures (ie 10 * 5s = 50s)
			SuccessThreshold:    1,  // How long to wait for the command to complete
			TimeoutSeconds:      2,  // How long to wait for the command to completes
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   SPIFFEHelperHealthCheckLivenessPath,
					Port:   intstr.FromInt(SPIFFEHelperHealthCheckPort),
					Scheme: corev1.URISchemeHTTP,
				},
			},
			InitialDelaySeconds: 60, // Start after startup probe likely succeeded and app stabilized
			PeriodSeconds:       15, // Check periodically
			FailureThreshold:    3,  // Consider failed after 3 consecutive failures
			SuccessThreshold:    1,
			TimeoutSeconds:      5,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   SPIFFEHelperHealthCheckReadinessPath,
					Port:   intstr.FromInt(SPIFFEHelperHealthCheckPort),
					Scheme: corev1.URISchemeHTTP,
				},
			},
			InitialDelaySeconds: 15, // Start checking readiness shortly after startup likely succeeded
			PeriodSeconds:       10, // Check periodically
			FailureThreshold:    3,  // Consider not ready after 3 consecutive failures
			SuccessThreshold:    1,
			TimeoutSeconds:      5,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      SPIFFEHelperConfigVolumeName,
				MountPath: SPIFFEHelperConfigMountPath,
				ReadOnly:  true,
			},
			{
				Name:      constants.SPIFFEEnableCertVolumeName,
				MountPath: constants.SPIFFEEnableCertDirectory,
			},
			workload.GetSPIFFEVolumeMount(),
		},
	}
}

func (h SPIFFEHelper) GetInitContainer() corev1.Container {
	configFilePath := filepath.Join(SPIFFEHelperConfigMountPath, SPIFFEHelperConfigFileName)
	writeCmd := fmt.Sprintf("mkdir -p %s && printf %%s \"$${%s}\" > %s && echo -e \"\\n=== SPIFFE Helper Config ===\" && cat %s && echo -e \"\\n===========================\"",
		filepath.Dir(configFilePath),
		SPIFFEHelperConfigContentEnvVar,
		configFilePath,
		configFilePath)

	return corev1.Container{
		Name:            SPIFFEHelperInitContainerName,
		Image:           InitHelperImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-c"},
		Args:            []string{writeCmd},
		Env: []corev1.EnvVar{{
			Name:  SPIFFEHelperConfigContentEnvVar,
			Value: h.Cfg,
		}},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name: SPIFFEHelperConfigVolumeName, MountPath: filepath.Dir(configFilePath),
			},
			{
				Name: constants.SPIFFEEnableCertVolumeName, MountPath: constants.SPIFFEEnableCertDirectory,
			},
		},
	}
}

type SPIFFEHelper struct {
	Cfg string
}
