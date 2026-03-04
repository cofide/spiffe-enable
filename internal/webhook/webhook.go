package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	constants "github.com/cofide/spiffe-enable/internal/const"
	"github.com/cofide/spiffe-enable/internal/helper"
	"github.com/cofide/spiffe-enable/internal/proxy"
	"github.com/cofide/spiffe-enable/internal/workload"
	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type spiffeEnableWebhook struct {
	Client  client.Client
	decoder admission.Decoder
	Log     logr.Logger
}

var (
	debugUIImage string
)

func NewSpiffeEnableWebhook(client client.Client, log logr.Logger, decoder admission.Decoder) (*spiffeEnableWebhook, error) {
	debugUIImage = getEnvWithDefault(constants.EnvVarUIImage, constants.DefaultDebugUIImage)

	log.Info(debugUIImage)

	return &spiffeEnableWebhook{
		Client:  client,
		Log:     log,
		decoder: decoder,
	}, nil
}

func (a *spiffeEnableWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := a.decoder.Decode(req, pod); err != nil {
		a.Log.Error(err, "Failed to decode pod", "request", req.UID)
		return admission.Errored(http.StatusBadRequest, err)
	}

	logger := a.Log.WithValues("podNamespace", pod.Namespace, "podName", pod.Name, "request", req.UID)

	handleDebugAnnotation(pod, logger)

	if errResp, err := applyInjections(pod, logger); err != nil {
		return errResp
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "Failed to marshal modified pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func handleDebugAnnotation(pod *corev1.Pod, logger logr.Logger) {
	debugAnnotationValue, debugAnnotationExists := pod.Annotations[constants.DebugAnnotation]

	if debugAnnotationExists && debugAnnotationValue == "true" {
		ensureCSIVolumeAndMount(pod, logger)

		if !workload.ContainerExists(pod.Spec.Containers, constants.DebugUIContainerName) {
			logger.Info("Adding SPIFFE Enable debug UI container", "containerName", constants.DebugUIContainerName)
			debugSidecar := corev1.Container{
				Name:            constants.DebugUIContainerName,
				Image:           debugUIImage,
				ImagePullPolicy: corev1.PullAlways,
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: constants.DebugUIPort,
					},
				},
			}
			pod.Spec.Containers = append(pod.Spec.Containers, debugSidecar)
		}
	}
}

func applyInjections(pod *corev1.Pod, logger logr.Logger) (admission.Response, error) {
	injectAnnotationValue, injectAnnotationExists := pod.Annotations[constants.InjectAnnotation]
	if !injectAnnotationExists {
		return admission.Response{}, nil
	}

	allowedModes := map[string]bool{
		constants.InjectAnnotationHelper: true,
		constants.InjectAnnotationProxy:  true,
		constants.InjectCSIVolume:        true,
	}

	toInject := strings.Split(injectAnnotationValue, ",")

	var invalidModes []string
	for _, mode := range toInject {
		trimmedMode := strings.TrimSpace(mode)
		if trimmedMode == "" {
			continue
		}
		if _, isValid := allowedModes[trimmedMode]; !isValid {
			invalidModes = append(invalidModes, trimmedMode)
		}
	}

	if len(invalidModes) > 0 {
		err := fmt.Errorf(
			"invalid mode(s) found in injection list: %v. Allowed modes are: %v",
			strings.Join(invalidModes, ", "),
			getKeys(allowedModes),
		)
		logger.Error(err, "Pod rejected due to invalid injection modes", "providedModes", injectAnnotationValue, "invalidFound", invalidModes)
		return admission.Errored(http.StatusBadRequest, err), err
	}

	for _, mode := range toInject {
		switch mode {
		case constants.InjectCSIVolume:
			ensureCSIVolumeAndMount(pod, logger)

		case constants.InjectAnnotationProxy:
			if err := applyProxyInjection(pod, logger); err != nil {
				return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error creating proxy config: %w", err)), err
			}

		case constants.InjectAnnotationHelper:
			if err := applyHelperInjection(pod, logger); err != nil {
				return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error creating spiffe-helper config: %w", err)), err
			}
		}
	}

	return admission.Response{}, nil
}

func applyProxyInjection(pod *corev1.Pod, logger logr.Logger) error {
	ensureCSIVolumeAndMount(pod, logger)

	configParams := proxy.EnvoyConfigParams{
		NodeID:          "node",
		ClusterName:     "cluster",
		AdminPort:       9901,
		AgentXDSService: constants.AgentXDSService,
		AgentXDSPort:    constants.AgentXDSPort,
	}

	envoy, err := proxy.NewEnvoy(configParams)
	if err != nil {
		logger.Error(err, "Error creating proxy config")
		return err
	}

	if !workload.VolumeExists(pod, proxy.EnvoyConfigVolumeName) {
		logger.Info("Adding Envoy config volume", "volumeName", proxy.EnvoyConfigVolumeName)
		pod.Spec.Volumes = append(pod.Spec.Volumes, envoy.GetConfigVolume())
	}

	if !workload.InitContainerExists(pod, proxy.EnvoyConfigInitContainerName) {
		logger.Info("Adding init container to inject Envoy config", "initContainerName", proxy.EnvoyConfigInitContainerName)
		pod.Spec.InitContainers = append([]corev1.Container{envoy.GetInitContainer()}, pod.Spec.InitContainers...)
	}

	if !workload.ContainerExists(pod.Spec.Containers, proxy.EnvoySidecarContainerName) {
		logger.Info("Adding Envoy proxy sidecar container", "containerName", proxy.EnvoySidecarContainerName)

		logLevel := pod.Annotations[constants.EnvoyLogLevelAnnotation]
		if logLevel == "" {
			logLevel = "info"
		}

		pod.Spec.Containers = append(pod.Spec.Containers, envoy.GetSidecarContainer(logLevel))
	}

	return nil
}

func applyHelperInjection(pod *corev1.Pod, logger logr.Logger) error {
	ensureCSIVolumeAndMount(pod, logger)

	logger.Info("Applying 'helper' mode mutations")

	incIntermediateBundle := false
	incIntermediateValue, incIntermediateExists := pod.Annotations[helper.SPIFFEHelperIncIntermediateAnnotation]
	if incIntermediateExists && incIntermediateValue == "true" {
		incIntermediateBundle = true
	}

	healthCheckPort := 0
	if portStr, ok := pod.Annotations[constants.SPIFFEHelperHealthPortAnnotation]; ok {
		if p, err := strconv.Atoi(portStr); err != nil || p <= 0 || p > 65535 {
			logger.Error(fmt.Errorf("invalid annotation value %q", portStr),
				"Ignoring invalid helper-health-port annotation, using default",
				"annotation", constants.SPIFFEHelperHealthPortAnnotation)
		} else {
			healthCheckPort = p
		}
	}

	configParams := helper.SPIFFEHelperConfigParams{
		AgentAddress:              constants.SPIFFEWLSocketPath,
		CertPath:                  constants.SPIFFEEnableCertDirectory,
		IncludeIntermediateBundle: incIntermediateBundle,
		HealthCheckPort:           healthCheckPort,
	}

	spiffeHelper, err := helper.NewSPIFFEHelper(configParams)
	if err != nil {
		logger.Error(err, "Error creating spiffe-helper config")
		return err
	}

	if !workload.VolumeExists(pod, helper.SPIFFEHelperConfigVolumeName) {
		logger.Info("Adding spiffe-helper config volume", "volumeName", helper.SPIFFEHelperConfigVolumeName)
		pod.Spec.Volumes = append(pod.Spec.Volumes, spiffeHelper.GetConfigVolume())
	}

	if !workload.VolumeExists(pod, constants.SPIFFEEnableCertVolumeName) {
		logger.Info("Adding spiffe-helper certs volume", "volumeName", constants.SPIFFEEnableCertVolumeName)
		pod.Spec.Volumes = append(pod.Spec.Volumes, getCertsVolume())
	}

	if !workload.InitContainerExists(pod, helper.SPIFFEHelperSidecarContainerName) {
		logger.Info("Adding spiffe-helper sidecar container", "initContainerName", helper.SPIFFEHelperSidecarContainerName)
		pod.Spec.InitContainers = append([]corev1.Container{spiffeHelper.GetSidecarContainer()}, pod.Spec.InitContainers...)
	}

	if !workload.InitContainerExists(pod, helper.SPIFFEHelperInitContainerName) {
		logger.Info("Adding init container to inject spiffe-helper config", "initContainerName", helper.SPIFFEHelperInitContainerName)
		pod.Spec.InitContainers = append([]corev1.Container{spiffeHelper.GetInitContainer()}, pod.Spec.InitContainers...)
	}

	return nil
}

func getCertsVolume() corev1.Volume {
	return corev1.Volume{
		Name: constants.SPIFFEEnableCertVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: corev1.StorageMediumMemory, // In-memory
			},
		},
	}
}

func getKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func ensureCSIVolumeAndMount(pod *corev1.Pod, logger logr.Logger) {
	// Add a CSI volume to the pod for the SPIFFE Workload API
	if !workload.VolumeExists(pod, constants.SPIFFEWLVolume) {
		logger.Info("Adding SPIFFE CSI volume", "volumeName", constants.SPIFFEWLVolume)
		pod.Spec.Volumes = append(pod.Spec.Volumes, workload.GetSPIFFEVolume())
	}

	// Process each (standard) container in the pod
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		// Add CSI volume mounts
		ensureCSIVolumeMount(container, workload.GetSPIFFEVolumeMount(), logger)
		// Add SPIFFE socket environment variable
		ensureEnvVar(container, workload.GetSPIFFEEnvVar())
	}
}

func ensureCSIVolumeMount(container *corev1.Container, targetMount corev1.VolumeMount, logger logr.Logger) bool {
	madeChange := false
	mountExists := false
	mountIndex := -1 // Index of the mount if found by name and path

	for i, vm := range container.VolumeMounts {
		if vm.Name == targetMount.Name && vm.MountPath == targetMount.MountPath {
			mountIndex = i
			if vm.ReadOnly == targetMount.ReadOnly {
				mountExists = true
			}
			break
		}
	}

	if !mountExists {
		if mountIndex != -1 {
			// Mount exists with the same name and path, but ReadOnly differs so we should update it
			logger.Info("Updating ReadOnly status for existing VolumeMount",
				"containerName", container.Name, "volumeMountName", targetMount.Name, "newReadOnly", targetMount.ReadOnly)
			container.VolumeMounts[mountIndex].ReadOnly = targetMount.ReadOnly
			madeChange = true
		} else {
			// Mount does not exist at all, append it
			logger.Info("Adding new VolumeMount to container",
				"containerName", container.Name, "volumeMountName", targetMount.Name)
			container.VolumeMounts = append(container.VolumeMounts, targetMount)
			madeChange = true
		}
	}
	return madeChange
}

func ensureEnvVar(container *corev1.Container, envVar corev1.EnvVar) {
	if !workload.EnvVarExists(container, envVar.Name) {
		container.Env = append(container.Env, envVar)
	}
}

func getEnvWithDefault(variable string, defaultValue string) string {
	v, ok := os.LookupEnv(variable)
	if !ok {
		return defaultValue
	}
	return v
}
