#!/usr/bin/env bash

set -euxo pipefail  # exit on errors and unset vars, and stop on the first error in a "pipeline"

BRANCH="${BRANCH:-main}"

mkdir -p .go

# Clone repositories containing the CRD definitions
for REPO in config-policy-controller governance-policy-propagator
do
    git clone -b "${BRANCH}" --depth 1 https://github.com/open-cluster-management-io/${REPO}.git .go/${REPO}
done

(
    cd .go/config-policy-controller
    cp deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml ../config-policy-crd-v1.yaml
    CRD_OPTIONS="crd:trivialVersions=true,crdVersions=v1beta1" make manifests
    cp deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml ../config-policy-crd-v1beta1.yaml
)

(
    cd .go/governance-policy-propagator
    cp deploy/crds/policy.open-cluster-management.io_policies.yaml ../policy-crd-v1.yaml
    CRD_OPTIONS="crd:trivialVersions=true,crdVersions=v1beta1" make manifests
    cp deploy/crds/policy.open-cluster-management.io_policies.yaml ../policy-crd-v1beta1.yaml
)

addLabelsExpression='.metadata.labels += {"addon.open-cluster-management.io/hosted-manifest-location": "hosting"}'

cat > pkg/addon/configpolicy/manifests/managedclusterchart/templates/policy.open-cluster-management.io_configurationpolicies_crd.yaml << EOF
# Copyright Contributors to the Open Cluster Management project

{{- if semverCompare "< 1.16.0" .Capabilities.KubeVersion.Version }}
$(yq e "$addLabelsExpression" .go/config-policy-crd-v1beta1.yaml)
{{ else }}
$(yq e "$addLabelsExpression" .go/config-policy-crd-v1.yaml)
{{- end }}
EOF

cat > pkg/addon/policyframework/manifests/managedclusterchart/templates/policy.open-cluster-management.io_policies_crd.yaml << EOF
# Copyright Contributors to the Open Cluster Management project

{{- if semverCompare "< 1.16.0" .Capabilities.KubeVersion.Version }}
$(yq e "$addLabelsExpression" .go/policy-crd-v1beta1.yaml)
{{ else }}
$(yq e "$addLabelsExpression" .go/policy-crd-v1.yaml)
{{- end }}
EOF

# Clean up the repositories - the chmod is necessary because Go makes some read-only things.
for REPO in config-policy-controller governance-policy-propagator
do
    chmod -R +rw .go/${REPO}
    rm -rf .go/${REPO}
done
