package constants

// Pod annotations
const (
	InjectAnnotation        = "spiffe.cofide.io/inject"
	DebugAnnotation         = "spiffe.cofide.io/debug"
	EnvoyLogLevelAnnotation = "spiffe.cofide.io/envoy-log-level"
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
	AgentXDSService = "cofide-agent-xds.cofide.svc.cluster.local"
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
	DefaultDebugUIImage  = "ghcr.io/cofide/spiffe-enable-ui:v0.3.0"
	EnvVarUIImage        = "SPIFFE_ENABLE_UI_IMAGE"
)
