package constants

// Pod annotations
const (
	InjectAnnotation = "spiffe.cofide.io/inject"
	DebugAnnotation  = "spiffe.cofide.io/debug"
)

// Components that can be injected
const (
	InjectAnnotationHelper = "helper"
	InjectAnnotationProxy  = "proxy"
	InjectCSIVolume        = "csi"
)

// SPIFFE Workload API
const (
	SPIFFEWLVolume        = "spiffe-workload-api"
	SPIFFEWLMountPath     = "/spiffe-workload-api"
	SPIFFEWLSocketEnvName = "SPIFFE_ENDPOINT_SOCKET"
	SPIFFEWLSocket        = "unix:///spiffe-workload-api/spire-agent.sock"
	SPIFFEWLSocketPath    = "/spiffe-workload-api/spire-agent.sock"
)

// Cofide Agent
const (
	AgentXDSPort    = 18001
	AgentXDSService = "cofide-agent.cofide.svc.cluster.local"
)

// SPIFFE Enable
const (
	SPIFFEEnableCertVolumeName = "spiffe-enable-certs"
	SPIFFEEnableCertDirectory  = "/spiffe-enable"
)

// Debug UI constants
const (
	DebugUIContainerName = "spiffe-enable-ui"
	DebugUIPort          = 8000
	DefaultDebugUIImage  = "010438484483.dkr.ecr.eu-west-1.amazonaws.com/cofide/spiffe-enable-ui:v0.1.0-alpha"
	EnvVarUIImage        = "SPIFFE_ENABLE_UI_IMAGE"
)
