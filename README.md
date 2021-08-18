# Helm 3: CRD update support library

This repository contains a helm library chart that can be used to emulate the old Helm 2 behavior of updating CustomResourceDefinitions on the target cluster during `helm upgrade` commands.

# Background: Helm 3 vs. CRDs

[`CustomResourceDefinitions`](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) (`CRDs` for short) can be used to define your Kubernetes objects and is widely used by Kubernetes based projects relying on the [Operator Pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

With the release of Helm 3 the Helm maintainers choose to exclude [CRD update support from Helm](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/).

There are quite many reasons for the Helm maintainers to reach this conclusion, however, I need to mention one corner case that can be extremely painful when using CRDs.

The main issue is where a `CustomResourceDefinition` changes. It's quite common to include non-breaking (new fields etc.) into a CRD without creating a new version of the CRD. The issue is when the update contains such changes that cannot be patched into the existing CRD or if an immutable field changes.

For any Resource (not just CRDS) usually, these conflicts are handled by deleting and creating a new Resource. The drawback of this approach is that in the case of a `Pod` or `Deployment` this might cause a micro-outage during deployment, but depending on the criticality and redundancy (multi-cluster) properties of the service this might be acceptable.

In case of deletion of a CRD, however, there's a side-effect: all of the `CustomResources` defined by the given `CustomResourceDefinition` will be removed from Kubernetes. This - for example - means that if you are upgrading your Istio deployment, and the DestinationRules CRD gets deleted, all of your DestinationRules settings will be lost on the cluster.

Please bear in mind this corner case when managing CRDs. The `crd-updater` takes a different approach to this issue: it will not delete any CRDs, but instead, it will fail the upgrade if the patch cannot be applied.

This saves the system from the previous issue but will fail any helm deployments when this issue is present.

# Why not use this library?

The removal of the CRD update from Helm 3 is not as big of an issue as it seems, as most deployments rely on some kind of (CI)/CD solution such as ArgoCD for managing clusters. These tools are mostly using Helm for what it's great for: as a templating language, then they rely on their implementation (instead of Helm's) to reconcile the cluster into the expected state.

For developers experimenting with new tools, the current approach is also fine: they install the given helm chart experiment with it, and get rid of Helm release or the whole cluster at some point in time. They are usually not concerned about upgradeability due to these reasons.

So when to use this library? Only if you are forced (due to technical requirements, etc.) to rely on Helm's capabilities to upgrade a given `Release` over an extended period. But please consider using a proper CI/CD tool such as ArgoCD.

# Usage

To use this library chart, you need to have a Helm 3 chart (`version: v2` in `Chart.yaml`). First of all, you will need to add a dependency into your existing Helm chart to `crd-updater`:

```yaml
dependencies:
  - name: "crd-updater"
    version: "0.0.3"
    repository: "https://kubernetes-charts.banzaicloud.com"
```

Then please update the charts folder by executing the following command in your chart directory:

```sh
helm dependency update
```

This exposes a new Helm template called `crd-update.tpl` to your existing chart. To start utilizing this new template first please add the following to you values.yaml:

```yaml
crd:
  manage: false
```

As you see we recommend setting the `.crd.manage` to false. The reason behind this is that if a proper CI/CD system is used to manage the Helm chart the `crd-updater` is not needed and it's better if the CI/CD system reconciles the CRDs too.

Then create a new file called `crd-updater.yaml` in the template folder:

```yaml
{{- if .Values.crd.manage -}}

{{{ $currentScope := . }}
{{ $crdManifests := list }}
{{ range $path, $_ := .Files.Glob "crds/*.yaml" }}
  {{ with $currentScope}}
    {{ $crdManifests = append $crdManifests (.Files.Get $path) }}
  {{ end }}
{{ end }}

include "crd-update.tpl" (list . .Values.crd (dict
      "pre-install,pre-upgrade,pre-rollback" $crdManifests
))
{{- end -}}
```

Assuming that CRDs are stored in the `crds` folder at the root of your chart, your CRDs will be automatically updated on every Helm upgrade.

# Usage: adding custom resources

For certain operators, it makes sense to not just install/update the CRDs, but also create a custom resource as part of the installation. For example, if you are installing the `prometheus-operator` chart, it will install the `Prometheus` CRD and it will automatically create a new `Prometheus` custom resource so that a Prometheus instance gets preconfigured on the cluster.

When thinking about the Helm upgrade flow, this causes an issue a new field/property is added to the CRD and the CustomResource provisioned automatically by the Helm chart would include that field too. What will happen, if an end-user tries to upgrade to this new release of the helm chart is:
- Helm does not update the CRDs from the chart
- Helm loads the CRDs from the Kubernetes API server (but those will be the old version of the CRD without the field, due to the previous item)
- Helm tries to validate the CustomResource against the old version of the CRD and reports an error

In case you would like to support such upgrades, `crd-updater` got you covered. Let me use the [CronTab](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/) from the Kubernetes handbook as an example. If a chart would like to provision this custom resource it will most like contain such a YAML template in the `templates` folder:

```yaml
apiVersion: "stable.example.com/v1"
kind: CronTab
metadata:
  name: {{ .Release.Name }}
spec:
  cronSpec: "* * * * */5"
  image: my-awesome-cron-image
```

For `crd-updater` to be able to update this resource, the chart maintainer needs to modify this file to prevent it from Helm to reconcile this Resource and make `crd-updater` do the reconciliation. The first step is to rename the file from `crontab.yaml` to `crontab.tpl`. Then the contents needs to be changed like this:

```yaml
{{ define "crontab.tpl" }}
apiVersion: "stable.example.com/v1"
kind: CronTab
metadata:
  name: {{ .Release.Name }}
spec:
  cronSpec: "* * * * */5"
  image: my-awesome-cron-image
{{ end }}
```

What happened is that now a `crontab.tpl` template is defined, for which we can use the [`include` command](https://helm.sh/docs/chart_template_guide/named_templates/) to render the YAML manifest.

Next we need to change the `crd-updater.yaml` to this:
```yaml
{{- if .Values.crd.manage -}}

{{ $currentScope := . }}
{{ $crdManifests := list }}
{{ range $path, $_ := .Files.Glob "crds/*.yaml" }}
  {{ with $currentScope}}
    {{ $crdManifests = append $crdManifests (.Files.Get $path) }}
  {{ end }}
{{ end }}

{{- include "crd-update.tpl" (list . .Values.crd (dict
      "pre-install,pre-upgrade,pre-rollback" $crdManifests
      "post-install,post-upgrade,post-rollback,pre-delete" (list (include "crontab.tpl" .))
    )) -}}
{{- else -}}
{{ include "crontab.tpl" . }}
{{- end -}}
```

There are two changes compared to the previous one: when invoking the `crd-update.tpl` we add a new line stating that when Helm is executing `post-install,post-upgrade,post-rollback,pre-delete` [hooks](https://helm.sh/docs/topics/charts_hooks/) it should also reconcile the `crontab.tpl`.

As it is visible, the CR is added after the installation/upgrade (`post-install` hook). The reason behind is that if a helm user changes the `.crd.mange` value from false to true, then helm will remove the CronTab custom resource and then after intallation `crd-updater` will create the new one. Please note that this might mean traffic disruption, so it's not recommended to do so.

The inclusion of `pre-delete` hook ensures that the CR will be removed when uninstalling the release. In the case of `pre-delete` and `post-delete` hooks, crd-updater automatically changes into delete mode. Finalizers are honored during this deletion: crd-updater will wait for the resource to completely get removed from the target cluster, before continuing the execution.

The second change is the inclusion of the `{{- else -}}` branch in the condition: if the current release is not managing CRDs, then we should output our CustomResources for Helm (or any other CICD system) to handle.

Please note: when including multiple manifests (such as CRDs) in a list the order of resource creation is not based on the argument position: CRD Updater will determine the [optimal installation order](https://github.com/banzaicloud/operator-tools/blob/v0.24.0/pkg/utils/sort.go#L29).

Last but not least we need to grant access for `crd-updater` to manage these resources. For namespaced resources add the following to your values.yaml (under the existing `crd` key):

```yaml
crd:
  roleRules:
  - apiGroups: ["stable.example.com"]
    resources: ["crontabs"]
    verbs: [ "create", "get", "watch", "list", "delete", "update" ]
```

For non-namespaced resources please use the following keys:

```yaml
crd:
  clusterRoleRules:
  - apiGroups: ["stable.example.com"]
    resources: ["crontabs"]
    verbs: [ "create", "get", "watch", "list", "delete", "update" ]
```


# Values.yaml settings

CRD updater supports the following settings from values.yaml:

| Key | Type | Usage |
|---|---|---|
| `.crd.image.repository` | string | repository to fetch crd-updater docker image from |
| `.crd.image.tag` | string | tag to use when fetching crd-updater |
| `.crd.image.pullPolicy` | string | `ImagePullPolicy` to use when creating the updater job |
| `.crd.clusterRoleRules` | list | additional cluster-wide permissions to grant to crd updater |
| `.crd.roleRules` | list | additional namespace-wide permissions to grant to crd updater |
| `.crd.pod.annotations`| map | additional annotations to use on the `Pod` provisioned by the `Job` that crd-updater creates |
| `.crd.hookDeletePolicy` | string | see [`hook-delete-policy` in Helm documentation](https://helm.sh/docs/topics/charts_hooks/) |
| `.crd.imagePullSecrets` | list | List of image pull secrets to be used by the `Pod` provisioned by the `Job` that crd-updater creates |

# Troubleshooting

In case a reconciliation error occurs, a helm timeout will happen (as the reconciliation Job will retry until Helm times out).

The easiest way to troubleshoot any issues is to make sure that the `.crd.hookDeletePolicy` is unset (that means the default of `before-hook-creation`), then take a look on the logs of the failing `Job`'s `Pods'` logs using the `kubectl logs` command.

# Subcharts

Please note that a template function is only able to access files in its own chart. It might be compelling to create a chart and add a subchart to it, then rely on `crd-updater` from the main chart to reconcile the subchart's CRDs. This will not work due to the previous limitation.

# Istio (or other Service Meshes)

When using `crd-updater` with Istio you will need to ensure that no sidecar gets injected, or that the sidecar stops as soon as the updater completes. In the case of Istio by specifying these values, the injection of `istio-proxy` can be disabled:

```yaml
crd:
  pod:
    annotations:
      sidecar.istio.io/inject: "false"
```

# Performance

Please note that using this library might increase the time it takes for Helm to upgrade/install the Helm chart as it creates objects one-by-one.
