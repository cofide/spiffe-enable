package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	constants "github.com/cofide/spiffe-enable/internal/const"
	"github.com/cofide/spiffe-enable/internal/helper"
	"github.com/cofide/spiffe-enable/internal/proxy"
	"github.com/cofide/spiffe-enable/internal/workload"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	jsonpatch "github.com/evanphx/json-patch"
)

func newTestWebhook(t *testing.T) *spiffeEnableWebhook {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	decoder := admission.NewDecoder(scheme)
	require.NotNil(t, decoder)

	webhook, err := NewSpiffeEnableWebhook(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		testr.New(t),
		decoder)
	require.NoError(t, err)

	return webhook
}

func newAdmissionRequest(t *testing.T, pod *corev1.Pod) (admission.Request, []byte) {
	rawPod, err := json.Marshal(pod)
	require.NoError(t, err)
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID: "test-uid",
			Object: runtime.RawExtension{
				Raw: rawPod,
			},
			Kind: metav1.GroupVersionKind{Kind: "Pod", Version: "v1"},
		},
	}, rawPod
}

func TestSpiffeEnableWebhook_Handle(t *testing.T) {
	basePod := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-pod",
				Namespace:   "default",
				Annotations: make(map[string]string),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app-container", Image: "nginx"},
				},
			},
		}
	}

	tests := []struct {
		name            string
		podAnnotations  map[string]string
		initialPod      func() *corev1.Pod
		expectedAllowed bool
		expectedPatched bool
		expectedStatus  *metav1.Status
		validatePod     func(t *testing.T, mutatedPod *corev1.Pod)
	}{
		{
			name:            "No pod annotations; no injection",
			podAnnotations:  map[string]string{},
			initialPod:      basePod,
			expectedAllowed: true,
			expectedPatched: false,
			validatePod: func(t *testing.T, mutatedPod *corev1.Pod) {
				require.Len(t, mutatedPod.Spec.Volumes, 0)
				require.Len(t, mutatedPod.Spec.Containers, 1) // app
				require.Len(t, mutatedPod.Spec.InitContainers, 0)
				appContainer := mutatedPod.Spec.Containers[0]
				assert.Equal(t, "app-container", appContainer.Name)
				require.Len(t, appContainer.VolumeMounts, 0)
			},
		},
		{
			name:            "spiffe.cofide.io/inject: csi",
			podAnnotations:  map[string]string{constants.InjectAnnotation: constants.InjectCSIVolume},
			initialPod:      basePod,
			expectedAllowed: true,
			expectedPatched: true,
			validatePod: func(t *testing.T, mutatedPod *corev1.Pod) {
				require.Len(t, mutatedPod.Spec.Volumes, 1)
				assert.Equal(t, constants.SPIFFEWLVolume, mutatedPod.Spec.Volumes[0].Name)
				assert.NotNil(t, mutatedPod.Spec.Volumes[0].VolumeSource.CSI)
				assert.Equal(t, "csi.spiffe.io", mutatedPod.Spec.Volumes[0].VolumeSource.CSI.Driver)

				require.Len(t, mutatedPod.Spec.Containers, 1)
				appContainer := mutatedPod.Spec.Containers[0]
				assert.Equal(t, "app-container", appContainer.Name)
				require.Len(t, appContainer.VolumeMounts, 1)
				assert.Equal(t, constants.SPIFFEWLVolume, appContainer.VolumeMounts[0].Name)
				assert.Equal(t, constants.SPIFFEWLMountPath, appContainer.VolumeMounts[0].MountPath)
				assert.True(t, appContainer.VolumeMounts[0].ReadOnly)

				foundEnv := false
				for _, env := range appContainer.Env {
					if env.Name == constants.SPIFFEWLSocketEnvName {
						assert.Equal(t, constants.SPIFFEWLSocket, env.Value)
						foundEnv = true
						break
					}
				}
				assert.True(t, foundEnv, "SPIFFE_ENDPOINT_SOCKET env var not found")
			},
		},
		{
			name:            "spiffe.cofide.io/debug: true",
			podAnnotations:  map[string]string{constants.DebugAnnotation: "true"},
			initialPod:      basePod,
			expectedAllowed: true,
			expectedPatched: true,
			validatePod: func(t *testing.T, mutatedPod *corev1.Pod) {
				// Basic CSI checks
				require.True(t, workload.VolumeExists(mutatedPod, constants.SPIFFEWLVolume), "SPIFFE CSI Volume missing")

				// Debug container checks
				foundDebugUI := false
				for _, c := range mutatedPod.Spec.Containers {
					if c.Name == constants.DebugUIContainerName {
						foundDebugUI = true
						assert.Equal(t, constants.DefaultDebugUIImage, c.Image)
						require.Len(t, c.Ports, 1)
						assert.Equal(t, int32(constants.DebugUIPort), c.Ports[0].ContainerPort)
						break
					}
				}
				assert.True(t, foundDebugUI, "Debug UI container not found")
				assert.Len(t, mutatedPod.Spec.Containers, 2) // app + debug UI
			},
		},
		{
			name:            "spiffe.cofide.io/inject: helper",
			podAnnotations:  map[string]string{constants.InjectAnnotation: constants.InjectAnnotationHelper},
			initialPod:      basePod,
			expectedAllowed: true,
			expectedPatched: true,
			validatePod: func(t *testing.T, mutatedPod *corev1.Pod) {
				// Basic CSI
				require.True(t, workload.VolumeExists(mutatedPod, constants.SPIFFEWLVolume), "SPIFFE CSI Volume missing")

				// Helper specific volumes
				assert.True(t, workload.VolumeExists(mutatedPod, helper.SPIFFEHelperConfigVolumeName))
				assert.True(t, workload.VolumeExists(mutatedPod, constants.SPIFFEEnableCertVolumeName))

				// Helper Init Container
				foundHelperInit := false
				for _, ic := range mutatedPod.Spec.InitContainers {
					if ic.Name == helper.SPIFFEHelperInitContainerName {
						foundHelperInit = true
						assert.Equal(t, helper.InitHelperImage, ic.Image) // Use exported var from helper
						// Check command, env, mounts for init container
						assert.Len(t, ic.VolumeMounts, 2)
						break
					}
				}
				assert.True(t, foundHelperInit, "SPIFFE Helper init container not found")

				// Helper Sidecar Container
				foundHelperSidecar := false
				for _, c := range mutatedPod.Spec.InitContainers {
					if c.Name == helper.SPIFFEHelperSidecarContainerName {
						foundHelperSidecar = true
						assert.Equal(t, helper.SPIFFEHelperImage, c.Image)
						// Check args, mounts for sidecar
						assert.Len(t, c.VolumeMounts, 3) // config, certs, spiffe-workload-api
						break
					}
				}
				assert.True(t, foundHelperSidecar, "SPIFFE Helper sidecar container not found")

				assert.Len(t, mutatedPod.Spec.Containers, 1)     // app
				assert.Len(t, mutatedPod.Spec.InitContainers, 2) // init + helper
			},
		},
		{
			name:            "spiffe.cofide.io/inject: proxy",
			podAnnotations:  map[string]string{constants.InjectAnnotation: constants.InjectAnnotationProxy},
			initialPod:      basePod,
			expectedAllowed: true,
			expectedPatched: true,
			validatePod: func(t *testing.T, mutatedPod *corev1.Pod) {
				// Basic CSI
				require.True(t, workload.VolumeExists(mutatedPod, constants.SPIFFEWLVolume), "SPIFFE CSI Volume missing")

				// Proxy specific volumes
				assert.True(t, workload.VolumeExists(mutatedPod, proxy.EnvoyConfigVolumeName))

				// Proxy Init Container
				foundProxyInit := false
				for _, ic := range mutatedPod.Spec.InitContainers {
					if ic.Name == proxy.EnvoyConfigInitContainerName {
						foundProxyInit = true
						assert.Equal(t, helper.InitHelperImage, ic.Image)
						// Check command, env, mounts, security context for init container
						require.NotNil(t, ic.SecurityContext)
						require.NotNil(t, ic.SecurityContext.Capabilities)
						assert.Contains(t, ic.SecurityContext.Capabilities.Add, corev1.Capability("NET_ADMIN"))
						assert.Equal(t, ptr.To(int64(0)), ic.SecurityContext.RunAsUser)
						break
					}
				}
				assert.True(t, foundProxyInit, "Envoy Proxy init container not found")

				// Proxy Sidecar Container
				foundProxySidecar := false
				for _, c := range mutatedPod.Spec.Containers {
					if c.Name == proxy.EnvoySidecarContainerName {
						foundProxySidecar = true
						assert.Equal(t, proxy.EnvoyImage, c.Image)
						// Check args, mounts, security context, ports for sidecar
						require.NotNil(t, c.SecurityContext)
						assert.Equal(t, ptr.To(int64(101)), c.SecurityContext.RunAsUser)
						assert.Equal(t, ptr.To(true), c.SecurityContext.RunAsNonRoot)
						require.Len(t, c.Ports, 1)
						assert.Equal(t, int32(proxy.EnvoyPort), c.Ports[0].ContainerPort)
						break
					}
				}
				assert.True(t, foundProxySidecar, "Envoy Proxy sidecar container not found")
				assert.Len(t, mutatedPod.Spec.Containers, 2) // app + proxy
			},
		},
		{
			name:            "spiffe.cofide.io/inject: helper,proxy",
			podAnnotations:  map[string]string{constants.InjectAnnotation: constants.InjectAnnotationHelper + "," + constants.InjectAnnotationProxy},
			initialPod:      basePod,
			expectedAllowed: true,
			expectedPatched: true,
			validatePod: func(t *testing.T, mutatedPod *corev1.Pod) {
				assert.Len(t, mutatedPod.Spec.Containers, 2)     // app + proxy
				assert.Len(t, mutatedPod.Spec.InitContainers, 3) // helper-init + helper + proxy-init
			},
		},
		{
			name:            "spiffe.cofide.io/inject: invalid_mode",
			podAnnotations:  map[string]string{constants.InjectAnnotation: "invalid_mode"},
			initialPod:      basePod,
			expectedAllowed: false, // Denied
			expectedPatched: false,
			expectedStatus: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: "invalid mode(s) found in injection list: invalid_mode. Allowed modes are: helper, proxy",
			},
			validatePod: nil,
		},
		{
			name:           "No pod annotation, CSI volume already exists",
			podAnnotations: map[string]string{},
			initialPod: func() *corev1.Pod {
				p := basePod()
				p.Spec.Volumes = append(p.Spec.Volumes, workload.GetSPIFFEVolume())
				return p
			},
			expectedAllowed: true,
			expectedPatched: false,
			validatePod: func(t *testing.T, mutatedPod *corev1.Pod) {
				assert.Len(t, mutatedPod.Spec.Volumes, 1, "CSI Volume should not be duplicated")
			},
		},
		{
			name:           "spiffe.cofide.io/inject: csi, CSI volume already exists, unmounted",
			podAnnotations: map[string]string{constants.InjectAnnotation: constants.InjectCSIVolume},
			initialPod: func() *corev1.Pod {
				p := basePod()
				p.Spec.Volumes = append(p.Spec.Volumes, workload.GetSPIFFEVolume())
				return p
			},
			expectedAllowed: true,
			expectedPatched: true,
			validatePod: func(t *testing.T, mutatedPod *corev1.Pod) {
				assert.Len(t, mutatedPod.Spec.Volumes, 1, "CSI Volume should not be duplicated")

				// Ensure the CSI volume is mounted into the container
				require.Len(t, mutatedPod.Spec.Containers, 1)
				appContainer := mutatedPod.Spec.Containers[0]
				assert.Equal(t, "app-container", appContainer.Name)
				require.Len(t, appContainer.VolumeMounts, 1)
				assert.Equal(t, constants.SPIFFEWLVolume, appContainer.VolumeMounts[0].Name)
				assert.Equal(t, constants.SPIFFEWLMountPath, appContainer.VolumeMounts[0].MountPath)
				assert.True(t, appContainer.VolumeMounts[0].ReadOnly)

				// Ensure the environment variable is set
				foundEnv := false
				for _, env := range appContainer.Env {
					if env.Name == constants.SPIFFEWLSocketEnvName {
						assert.Equal(t, constants.SPIFFEWLSocket, env.Value)
						foundEnv = true
						break
					}
				}
				assert.True(t, foundEnv, "SPIFFE_ENDPOINT_SOCKET env var not found")
			},
		},
		// TODO: Add tests for idempotency of helper and proxy components if they already exist.
		// TODO: Add test for existing CSI volume mount with different ReadOnly (should be updated by ensureCSIVolumeMount)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wh := newTestWebhook(t)
			pod := tt.initialPod()
			if pod.Annotations == nil && len(tt.podAnnotations) > 0 { // Ensure annotations map exists
				pod.Annotations = make(map[string]string)
			}
			for k, v := range tt.podAnnotations {
				pod.Annotations[k] = v
			}

			req, podBytes := newAdmissionRequest(t, pod)
			resp := wh.Handle(context.Background(), req)

			assert.Equal(t, tt.expectedAllowed, resp.Allowed, "Response Allowed mismatch")

			if !tt.expectedAllowed && tt.expectedStatus != nil {
				require.NotNil(t, resp.Result)
				// Check parts of the message because allowed modes order might change
				assert.Contains(t, resp.Result.Message, "invalid mode(s) found")
				assert.Contains(t, resp.Result.Message, "invalid_mode")
				assert.Equal(t, int32(http.StatusBadRequest), resp.Result.Code)
			}

			if tt.expectedPatched {
				// Check either resp.Patch or resp.Patches
				hasPatch := (len(resp.Patch) > 0) || len(resp.Patches) > 0
				assert.True(t, hasPatch, "Expected patch(es)")

				if tt.validatePod != nil && resp.Allowed {
					modifiedJSON := podBytes
					for _, p := range resp.Patches {
						patchBytes, err := p.MarshalJSON()
						if err != nil {
							t.Fatalf("Failed to marshal patch: %v", err)
						}

						patchArrayBytes := append([]byte("["), patchBytes...)
						patchArrayBytes = append(patchArrayBytes, []byte("]")...)

						patch, err := jsonpatch.DecodePatch(patchArrayBytes)
						if err != nil {
							t.Fatalf("Failed to decode patch: %v", err)
						}

						modifiedJSON, err = patch.Apply(modifiedJSON)
						if err != nil {
							panic(err)
						}
					}

					// Decode the result
					var modifiedPod corev1.Pod
					if err := json.Unmarshal(modifiedJSON, &modifiedPod); err != nil {
						t.Fatalf("Failed to unmarshal modified pod: %v", err)
					}

					// Validate the modified pod matches the expected pod
					tt.validatePod(t, &modifiedPod)
				}
			} else {
				// Check for no patches when not expected
				assert.Empty(t, resp.Patch)
				assert.Empty(t, resp.Patches)
			}
		})
	}
}
