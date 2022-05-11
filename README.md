[comment]: # " Copyright Contributors to the Open Cluster Management project "

# Governance Policy Addon Controller

Open Cluster Management - Governance Policy Addon Controller

[![Build](https://img.shields.io/badge/build-Prow-informational)](https://prow.ci.openshift.org/?repo=stolostron%2Fgovernance-policy-addon-controller)
[![KinD tests](https://github.com/stolostron/governance-policy-addon-controller/actions/workflows/kind.yml/badge.svg?branch=main&event=push)](https://github.com/stolostron/governance-policy-addon-controller/actions/workflows/kind.yml)
[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

## Description

The governance policy addon controller manages installations of policy addons on managed clusters by
using
[ManifestWorks](https://github.com/open-cluster-management-io/api/blob/main/docs/manifestwork.md).
The addons can be enabled, disabled, and configured via their `ManagedClusterAddon` resources. For
more information on the addon framework, see the
[addon-framework enhancement/design](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/8-addon-framework).
The addons managed by this controller are:

- The "config-policy-controller" consisting of the
  [Configuration Policy Controller](https://github.com/stolostron/config-policy-controller).
- The "cert-policy-controller" consisting of the
  [Certificate Policy Controller](https://github.com/stolostron/cert-policy-controller).
- The "iam-policy-controller" consisting of the
  [IAM Policy Controller](https://github.com/stolostron/iam-policy-controller).
- The "governance-policy-framework" consisting of the
  [Policy Spec Sync](https://github.com/stolostron/governance-policy-spec-sync), the
  [Policy Status Sync](https://github.com/stolostron/governance-policy-status-sync), and the
  [Policy Template Sync](https://github.com/stolostron/governance-policy-template-sync).

Go to the [Contributing guide](CONTRIBUTING.md) to learn how to get involved.

Check the [Security guide](SECURITY.md) if you need to report a security issue.

## Getting Started - Usage

### Prerequisites

These instructions assume:

- You have at least one running kubernetes cluster;
- You have already followed instructions from
  [registration-operator](https://github.com/open-cluster-management-io/registration-operator) and
  installed OCM successfully;
- At least one managed cluster has been imported and accepted.

### Deploying the controller

From the base of this repository, a default installation can be applied to the hub cluster with
`kubectl apply -k config/default`. You might want to customize the namespace the controller is
deployed to, or the specific image used by the controller. This can be done either by editing
[config/default/kustomization.yaml](./config/default/kustomization.yaml) directly, or by using
kustomize commands like `kustomize edit set namespace [mynamespace]` or
`kustomize edit set image policy-addon-image=[myimage]`.

### Deploying and Configuring an addon

This example CR would deploy the Configuration Policy Controller to a managed cluster called
`my-managed-cluster`:

```yaml
apiVersion: addon.open-cluster-management.io/v1alpha1
kind: ManagedClusterAddOn
metadata:
  name: config-policy-controller
  namespace: my-managed-cluster
spec:
  installNamespace: open-cluster-management-agent-addon
```

To modify the image used by the Configuration Policy Controller on this managed cluster, you can add
an annotation either by modifying and applying the YAML directly, or via a kubectl command like:

```shell
kubectl -n my-managed-cluster annotate managedclusteraddon config-policy-controller addon.open-cluster-management.io/values='{"global":{"imageOverrides":{"config_policy_controller":"quay.io/my-repo/my-configpolicy:imagetag"}}}'
```

Any values in the
[Helm chart's values.yaml](./pkg/addon/configpolicy/manifests/managedclusterchart/values.yaml) can
be modified with the `addon.open-cluster-management.io/values` annotation. However, the structure
of that annotation makes it difficult to apply mutliple changes - separate `kubectl annotate`
commands will override each other, as opposed to being merged.

To address this issue, there are some separate annotations that can be applied independently:
- `addon.open-cluster-management.io/on-multicluster-hub` - set to "true" on the
governance-policy-framework addon when deploying it on a self-managed hub. It has no effect on
other addons.
- `log-level` - set to an integer to adjust the logging levels on the addon. A higher number will
generate more logs. Note that logs from libraries used by the addon will be 2 levels below this
setting; to get a `v=5` log message from a library, annotate the addon with `log-level=7`.

## Getting Started - Development

To set up a local [KinD](https://kind.sigs.k8s.io/) cluster for development, you'll need to install
`kind`. Then you can use the `kind-deploy-controller` make target to set everything up, including
starting a kind cluster, installing the
[registration-operator](https://github.com/open-cluster-management-io/registration-operator), and
importing a cluster.

Alternatively, you can run `./build/manage-clusters.sh` to deploy a hub and a configurable number of
managed clusters (defaults to one) using Kind.

Before the addons can be successfully distributed to the managed cluster, the work-agent must be
started. This usually happens automatically within 5 minutes of importing the managed cluster, and
can be waited for programmatically with the `wait-for-work-agent` make target.

### Deploying changes

Two make targets are used to update the controller running in the kind clusters with any local
changes. The `kind-load-image` target will re-build the image, and load it into the kind cluster.
The `kind-regenerate-controller` target will update the deployment manifests with any local changes
(including RBAC changes), and restart the controller on the cluster to update it.

In general, the addon-controller will revert changes made to its managed ManifestWorks, to match 
what is rendered by the helm charts. To more quickly test changes to deployed resources without 
rebuilding the controller image, the `policy-addon-pause=true` annotation can be added to the 
ManagedClusterAddOn resource. This will enable changes to the ManifestWork on the hub cluster to
persist, but direct changes to resources on a managed cluster will still be reverted to match the
ManifestWork.

### Running Tests

The e2e tests are intended to be run against a `kind` cluster. After setting one up with the steps
above (and waiting for the work-agent), the tests can be run with the `e2e-test` make target.

<!---
Date: May/11/2022
-->
