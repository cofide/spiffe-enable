# spiffe-enable: enabling SPIFFE for workloads

`spiffe-enable` is a Kubernetes admission webhook to auto-inject components that enable SPIFFE for workloads, including application workloads that are not SPIFFE-native. The purpose of the project is to provide seamless automation and easily onboard workloads to a SPIFFE-enabled enviroment (eg [SPIRE](https://github.com/spiffe/spire) or [Cofide's Connect](#production-use-cases)).

- [spiffe-helper](https://github.com/spiffe/spiffe-helper)
- [Envoy proxy](https://github.com/envoyproxy/envoy)

## How to use

In order to use the admission webhook:

- the workload's namespace requires a `spiffe.cofide.io/enabled: true` label to 'opt in' to the auto-injection;
- each pod in the namespace will see a SPIFFE CSI volume and environment variable automatically injected on admission;
- additional components can also be auto-injected on a per-pod basis using the `spiffe.cofide.io/mode` annotation.

The modes that are currently available:

- `spiffe.cofide.io/mode: helper`: a `spiffe-helper` sidecar container will be injected 
- `spiffe.cofide.io/mode: proxy`: an Envoy sidecar container will be injected  (note: used in conjuction with [Cofide's Connect Agent](#production-use-cases))

## Installation

`spiffe-enable` is a Kubernetes mutating admission webhook. The easiest method to install `spiffe-enable` in a cluster is to use the [Helm chart](https://github.com/cofide/helm-charts) provided by Cofide.

## Development

### Prerequisites
- go version v1.22.0+
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

