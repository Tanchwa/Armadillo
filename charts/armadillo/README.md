# armadillo

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: latest](https://img.shields.io/badge/AppVersion-latest-informational?style=flat-square)

Distributes and promotes AppArmor profiles learned by apparmor-profile-action

## Install

```bash
helm install armadillo ./charts/armadillo --namespace armadillo-system --create-namespace
```

Opt a namespace in to startup gating:

```bash
kubectl label namespace <namespace> armadillo.andrewsutliff.com/injection=enabled
```

## What gets installed

| Component | Kind | Does |
|---|---|---|
| manager | Deployment | Decides promotion and demotion cluster-wide |
| webhook | Deployment | Injects the startup gate into bound pods |
| loader | DaemonSet | Loads profiles on every node and reports what is loaded |

The gate image is not run by the chart: the webhook injects it into
application pods, so every opted-in namespace must be able to pull it.

## Webhook certificate

`webhook.certificate.source=helm` (the default) generates a self-signed CA at
install and reuses it on every upgrade. To rotate it, delete the
`<release>-webhook-cert` Secret and run `helm upgrade`. Set it to `certManager`
to have cert-manager issue and rotate the certificate instead.

## CRDs

CRDs are templates, not files in `crds/`, so `helm upgrade` upgrades them. By
default they are annotated `helm.sh/resource-policy: keep` and survive
`helm uninstall`; deleting them deletes every Armadillo object in the cluster.

## Developing the chart

The CRDs, the manager's RBAC rules and the webhook's rules are generated from
kubebuilder markers. Do not edit `files/`; run:

```bash
make helm-sync   # copy generated manifests into the chart
make helm-docs   # regenerate this README from values.yaml
make helm-lint   # lint and render the chart
make helm-deploy IMG=... LOADER_IMG=... GATE_IMG=...   # install from local images
```

## Requirements

Kubernetes: `>=1.30.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| commonLabels | object | `{}` | Labels added to every resource. |
| crds.enabled | bool | `true` | Install and upgrade the CRDs with the chart. |
| crds.keep | bool | `true` | Keep the CRDs when the release is uninstalled. Deleting them deletes every ManagedProfile and ProfileBinding in the cluster. |
| fullnameOverride | string | `""` | Override the full resource name prefix. |
| gate.image.repository | string | `"docker.io/tanchwa/armadillo-gate"` | Gate image. Every opted-in namespace must be able to pull it. |
| gate.image.tag | string | `""` | Defaults to the chart's appVersion. |
| imagePullSecrets | list | `[]` | Secrets used to pull the manager, webhook and loader images. The gate image is pulled in application namespaces, so these do not apply to it. |
| loader.affinity | object | `{}` |  |
| loader.extraArgs | list | `[]` |  |
| loader.gatePort | int | `9099` | Host port of the gate endpoint the startup gate polls. The webhook is configured with the same value. |
| loader.image.pullPolicy | string | `"IfNotPresent"` |  |
| loader.image.repository | string | `"docker.io/tanchwa/armadillo-loader"` |  |
| loader.image.tag | string | `""` | Defaults to the chart's appVersion. |
| loader.nodeRestriction.enabled | bool | `true` | Install a ValidatingAdmissionPolicy that limits each loader to its own node's ProfileNodeStatus objects. |
| loader.nodeSelector."kubernetes.io/os" | string | `"linux"` |  |
| loader.podAnnotations | object | `{}` |  |
| loader.podLabels | object | `{}` |  |
| loader.priorityClassName | string | `"system-node-critical"` |  |
| loader.profileDir | string | `"/var/lib/armadillo/profiles"` | Host directory profiles are written to. Kept apart from /etc/apparmor.d so the loader cannot overwrite the host's own profiles. |
| loader.resources.limits.cpu | string | `"200m"` |  |
| loader.resources.limits.memory | string | `"128Mi"` |  |
| loader.resources.requests.cpu | string | `"10m"` |  |
| loader.resources.requests.memory | string | `"64Mi"` |  |
| loader.securityfsDir | string | `"/sys/kernel/security/apparmor"` | Host path of the AppArmor securityfs directory. |
| loader.serviceAccount.annotations | object | `{}` |  |
| loader.tolerations | list | `[{"operator":"Exists"}]` | A node without a loader cannot run any bound pod, so tolerate every taint. |
| manager.affinity | object | `{}` |  |
| manager.extraArgs | list | `[]` | Extra command-line arguments. |
| manager.git.allowedHosts | string | `"github.com"` | Comma-separated hosts the manager may fetch profile repositories from. |
| manager.git.syncInterval | string | `"5m"` | How often profile repositories are re-fetched. |
| manager.image.pullPolicy | string | `"IfNotPresent"` |  |
| manager.image.repository | string | `"docker.io/tanchwa/armadillo-manager"` | Manager image. The webhook runs from the same image. |
| manager.image.tag | string | `""` | Defaults to the chart's appVersion. |
| manager.nodeSelector."kubernetes.io/os" | string | `"linux"` |  |
| manager.podAnnotations | object | `{}` |  |
| manager.podLabels | object | `{}` |  |
| manager.priorityClassName | string | `""` |  |
| manager.replicas | int | `1` | Replicas. Only the leader reconciles; more replicas only shorten failover. |
| manager.resources.limits.cpu | string | `"500m"` |  |
| manager.resources.limits.memory | string | `"128Mi"` |  |
| manager.resources.requests.cpu | string | `"10m"` |  |
| manager.resources.requests.memory | string | `"64Mi"` |  |
| manager.serviceAccount.annotations | object | `{}` |  |
| manager.tolerations | list | `[]` |  |
| metrics.enabled | bool | `true` | Serve the manager's metrics over HTTPS on port 8443. |
| metrics.serviceMonitor.enabled | bool | `false` | Create a Prometheus Operator ServiceMonitor. |
| metrics.serviceMonitor.interval | string | `""` |  |
| metrics.serviceMonitor.labels | object | `{}` |  |
| nameOverride | string | `""` | Override the chart name used in resource names. |
| networkPolicy.enabled | bool | `false` | Restrict ingress to the webhook and metrics ports. |
| networkPolicy.metricsNamespaceSelector | object | `{"matchLabels":{"metrics":"enabled"}}` | Namespaces allowed to scrape metrics. |
| webhook.affinity | object | `{}` | Defaults to spreading replicas across nodes. |
| webhook.certificate.duration | int | `3650` | Validity of a "helm" certificate, in days. |
| webhook.certificate.source | string | `"helm"` | Where the serving certificate comes from. "certManager" needs cert-manager installed. "helm" generates a self-signed CA at install and reuses it on upgrade. |
| webhook.extraArgs | list | `[]` | Extra command-line arguments. The webhook runs from manager.image. |
| webhook.failurePolicy | string | `"Fail"` | What the API server does when the webhook is unreachable. Ignore lets pods start without a gate, and so without a guarantee their profile is loaded. |
| webhook.namespaceSelector | object | `{"matchLabels":{"armadillo.andrewsutliff.com/injection":"enabled"}}` | Namespaces whose pods the webhook sees. Opt-in by label by default. |
| webhook.nodeSelector."kubernetes.io/os" | string | `"linux"` |  |
| webhook.objectSelector | object | `{}` | Narrow further to pods matching this selector. |
| webhook.podAnnotations | object | `{}` |  |
| webhook.podDisruptionBudget.enabled | bool | `true` | Keep at least one webhook replica through voluntary disruptions. |
| webhook.podDisruptionBudget.minAvailable | int | `1` |  |
| webhook.podLabels | object | `{}` |  |
| webhook.priorityClassName | string | `""` |  |
| webhook.replicas | int | `2` | Replicas. Pod creation in opted-in namespaces fails while none is up. |
| webhook.resources.limits.cpu | string | `"500m"` |  |
| webhook.resources.limits.memory | string | `"128Mi"` |  |
| webhook.resources.requests.cpu | string | `"10m"` |  |
| webhook.resources.requests.memory | string | `"64Mi"` |  |
| webhook.serviceAccount.annotations | object | `{}` |  |
| webhook.timeoutSeconds | int | `5` |  |
| webhook.tolerations | list | `[]` |  |
