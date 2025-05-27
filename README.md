# spiffe-enable: enabling SPIFFE for Kubernetes workloads

`spiffe-enable` is a Kubernetes admission webhook to auto-inject components that enable SPIFFE for workloads, including application workloads that are not SPIFFE-native. The purpose of the project is to provide seamless automation and easily onboard workloads to a SPIFFE-enabled enviroment (eg [SPIRE](https://github.com/spiffe/spire) or [Cofide's Connect](#production-use-cases)) using components, including:

- [spiffe-helper](https://github.com/spiffe/spiffe-helper)
- [Envoy proxy](https://github.com/envoyproxy/envoy)
- A `spiffe-enable` UI to debug a workload's credentials

## How to use

### Admission webhook

In order to use the admission webhook:

- the workload's namespace requires a `spiffe.cofide.io/enabled: true` label to 'opt in' to the auto-injection;
- each pod in the namespace will see a SPIFFE CSI volume and environment variable automatically injected on admission;
- additional components can also be auto-injected on a per-pod basis using the `spiffe.cofide.io/inject` annotation in a comma-delimited list.

The modes that are currently available:

|  Mode     | Description |
| --------- | :--- |
| `helper`  | A `spiffe-helper` sidecar container will be injected to retrieve and automatically renew the SVID and bundle. |
| `proxy`   | An Envoy sidecar container will be injected. Note: this is used in conjuction with [Cofide's Connect Agent](#production-use-cases) |

### Debug UI

`spiffe-enable` also provides a basic UI to help user's debug the configuration and credentials that have been received by the workload identity provider - eg the SVID and the trust bundle. 

To use the debug UI. add the annotation `spiffe.cofide.io/debug: true` to the template of the pod you wish to debug. By default, the UI serves on the container port 8080; use `port-forward` to connect to it (you may wish to choose a different local port):

```sh
kubectl port-forward [pod-name] 8080 
```

You can now browse to `http://localhost:8080` to use the UI.

## Installation

`spiffe-enable` is a Kubernetes mutating admission webhook. The easiest method of installation in a cluster is to use the [Helm chart](https://github.com/cofide/helm-charts) provided by Cofide:

```sh
helm repo add cofide https://charts.cofide.dev
helm install \
  spiffe-enable cofide/spiffe-enable \
  --namespace cofide \
  --create-namespace \
  --version v0.1.0
```

## Development

`spiffe-enable` is a Kubernetes mutating admission webhook that is built on [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime). The webhook is implemented in [`webhook`](webhook/webhook.go) and the `spiffe-helper` and `proxy` injection in [`internal/helper`](internal/helper/config.go) and [`internal/proxy`](internal/proxy/config.go), respectively.

### Prerequisites

- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

## Production use cases

<div style="float: left; margin-right: 10px;">
    <a href="https://www.cofide.io">
        <img src="docs/img/cofide-colour-blue.svg" width="40" alt="Cofide">
    </a>
</div>

`spiffe-enable` is a project developed and maintained by [Cofide](https://www.cofide.io). We're building a workload identity platform that is seamless and secure for multi and hybrid cloud environments. If you have a production use case with need for greater flexibility, control and visibility, with enterprise-level support, please [speak with us](mailto:hello@cofide.io) to find out more about the [Cofide](https://www.cofide.io) early access programme 👀.

