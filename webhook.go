package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const injectAnnotation = "cofide.io/sidecar-inject"
const awsRoleArnAnnotation = "cofide.io/aws-role-arn"

type podAnnotator struct {
	Client  client.Client
	decoder admission.Decoder
	Log     logr.Logger
}

func (a *podAnnotator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := a.decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	injectAnnotationValue, injectAnnotationExists := pod.Annotations[injectAnnotation]
	roleArValue, roleArnExists := pod.Annotations[awsRoleArnAnnotation]

	if injectAnnotationExists && injectAnnotationValue == "true" &&
		roleArnExists && roleArValue != "" {

		// add the AWS SDK env var
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
		// add the spiffe workload socket
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "spiffe-workload-api",
			VolumeSource: corev1.VolumeSource{
				CSI: &corev1.CSIVolumeSource{
					Driver:   "csi.spiffe.io",
					ReadOnly: pointer.Bool(true),
				},
			},
		})
		// Add the new container to the pod
		pod.Spec.Containers = append(pod.Spec.Containers, sidecarContainer)
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (a *podAnnotator) InjectDecoder(d admission.Decoder) error {
	a.decoder = d
	return nil
}
