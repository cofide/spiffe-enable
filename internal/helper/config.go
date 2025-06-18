package helper

import (
	"fmt"
	"path/filepath"

	constants "github.com/cofide/spiffe-enable/internal/const"
	"github.com/cofide/spiffe-enable/internal/workload"
	"github.com/hashicorp/hcl/v2/hclwrite"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/hashicorp/hcl/v2/gohcl"
)

// Images
var (
	SPIFFEHelperImage = "ghcr.io/spiffe/spiffe-helper:0.10.0"
	InitHelperImage   = "010438484483.dkr.ecr.eu-west-1.amazonaws.com/cofide/spiffe-enable-init:v0.1.0-alpha"
)

// Constants
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

// Structs from github.com/spiffe/spiffe-helper/cmd/spiffe-helper/config
// Copied for now as the upstream structs are designed for decoding, not encoding to HCL (our case case)
type SPIFFEHelperConfig struct {
	AddIntermediatesToBundle bool                     `hcl:"add_intermediates_to_bundle"`
	AgentAddress             string                   `hcl:"agent_address"`
	Cmd                      string                   `hcl:"cmd"`
	CmdArgs                  string                   `hcl:"cmd_args"`
	PIDFilename              string                   `hcl:"pid_file_name"`
	CertDir                  string                   `hcl:"cert_dir"`
	CertFileMode             int                      `hcl:"cert_file_mode"`
	KeyFileMode              int                      `hcl:"key_file_mode"`
	JWTBundleFileMode        int                      `hcl:"jwt_bundle_file_mode"`
	JWTSVIDFileMode          int                      `hcl:"jwt_svid_file_mode"`
	IncludeFederatedDomains  bool                     `hcl:"include_federated_domains"`
	RenewSignal              string                   `hcl:"renew_signal"`
	DaemonMode               *bool                    `hcl:"daemon_mode"`
	HealthCheck              SPIFFEHelperHealthConfig `hcl:"health_check,block"`
	Hint                     string                   `hcl:"hint"`

	// x509 configuration
	SVIDFilename       string `hcl:"svid_file_name"`
	SVIDKeyFilename    string `hcl:"svid_key_file_name"`
	SVIDBundleFilename string `hcl:"svid_bundle_file_name"`

	// JWT configuration
	JWTSVIDs          []SPIFFEHelperJWTConfig `hcl:"jwt_svids,block"`
	JWTBundleFilename string                  `hcl:"jwt_bundle_file_name"`
}

type SPIFFEHelperJWTConfig struct {
	JWTAudience       string   `hcl:"jwt_audience"`
	JWTExtraAudiences []string `hcl:"jwt_extra_audiences"`
	JWTSVIDFilename   string   `hcl:"jwt_svid_file_name"`
}

type SPIFFEHelperHealthConfig struct {
	ListenerEnabled bool   `hcl:"listener_enabled"`
	BindPort        int    `hcl:"bind_port"`
	LivenessPath    string `hcl:"liveness_path"`
	ReadinessPath   string `hcl:"readiness_path"`
}

type SPIFFEHelperConfigParams struct {
	AgentAddress              string
	CertPath                  string
	IncludeIntermediateBundle bool
}

func NewSPIFFEHelper(params SPIFFEHelperConfigParams) (*SPIFFEHelper, error) {
	if params.AgentAddress == "" || params.CertPath == "" {
		return nil, fmt.Errorf("missing spiffe-helper configuration parameters")
	}

	spiffeHelperCfg := &SPIFFEHelperConfig{
		CertDir:                  params.CertPath,
		DaemonMode:               BoolPtr(true),
		IncludeFederatedDomains:  true,
		AgentAddress:             params.AgentAddress,
		AddIntermediatesToBundle: params.IncludeIntermediateBundle,
		SVIDFilename:             "tls.crt",
		SVIDKeyFilename:          "tls.key",
		SVIDBundleFilename:       "ca.pem",
		HealthCheck: SPIFFEHelperHealthConfig{
			ListenerEnabled: true,
		},
	}

	// Marshal to an HCL-formatted string
	hclFile := hclwrite.NewEmptyFile()
	gohcl.EncodeIntoBody(spiffeHelperCfg, hclFile.Body())
	hclBytes := hclFile.Bytes()
	hclString := string(hclBytes)

	return &SPIFFEHelper{Config: hclString}, nil
}

func (h *SPIFFEHelper) GetConfigVolume() corev1.Volume {
	return corev1.Volume{
		Name:         SPIFFEHelperConfigVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

func (h *SPIFFEHelper) GetSidecarContainer() corev1.Container {
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

func (h *SPIFFEHelper) GetInitContainer() corev1.Container {
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
			Value: h.Config,
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
	Config string
}

func BoolPtr(b bool) *bool {
	return &b
}
