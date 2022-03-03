#!/usr/bin/env bash

set -euxo pipefail  # exit on errors and unset vars, and stop on the first error in a "pipeline"

BRANCH="${BRANCH:-main}"

mkdir -p .go

# Clone repositories containing the CRD definitions
for REPO in cert-policy-controller config-policy-controller iam-policy-controller governance-policy-propagator
do
    git clone -b "${BRANCH}" --depth 1 https://github.com/stolostron/${REPO}.git .go/${REPO}
done

(
    cd .go/cert-policy-controller
    cp deploy/crds/policy.open-cluster-management.io_certificatepolicies.yaml ../cert-policy-crd-v1.yaml
    CRD_OPTIONS="crd:trivialVersions=true,crdVersions=v1beta1" make manifests
    cp deploy/crds/policy.open-cluster-management.io_certificatepolicies.yaml ../cert-policy-crd-v1beta1.yaml
)

(
    cd .go/config-policy-controller
    cp deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml ../config-policy-crd-v1.yaml
    CRD_OPTIONS="crd:trivialVersions=true,crdVersions=v1beta1" make manifests
    cp deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml ../config-policy-crd-v1beta1.yaml
)

(
    cd .go/iam-policy-controller
    cp deploy/crds/policy.open-cluster-management.io_iampolicies.yaml ../iam-policy-crd-v1.yaml
    CRD_OPTIONS="crd:trivialVersions=true,crdVersions=v1beta1" make manifests
    cp deploy/crds/policy.open-cluster-management.io_iampolicies.yaml ../iam-policy-crd-v1beta1.yaml
)

(
    cd .go/governance-policy-propagator
    cp deploy/crds/policy.open-cluster-management.io_policies.yaml ../policy-crd-v1.yaml
    CRD_OPTIONS="crd:trivialVersions=true,crdVersions=v1beta1" make manifests
    cp deploy/crds/policy.open-cluster-management.io_policies.yaml ../policy-crd-v1beta1.yaml
)

cat > pkg/addon/certpolicy/manifests/managedclusterchart/templates/policy.open-cluster-management.io_certificatepolicy_crd.yaml << EOF
# Copyright Contributors to the Open Cluster Management project

{{- if semverCompare "< 1.16.0" .Capabilities.KubeVersion.Version }}
$(cat .go/cert-policy-crd-v1beta1.yaml)
{{ else }}
$(cat .go/cert-policy-crd-v1.yaml)
{{- end }}
EOF

cat > pkg/addon/configpolicy/manifests/managedclusterchart/templates/policy.open-cluster-management.io_configurationpolicies_crd.yaml << EOF
# Copyright Contributors to the Open Cluster Management project

{{- if semverCompare "< 1.16.0" .Capabilities.KubeVersion.Version }}
$(cat .go/config-policy-crd-v1beta1.yaml)
{{ else }}
$(cat .go/config-policy-crd-v1.yaml)
{{- end }}
EOF

cat > pkg/addon/iampolicy/manifests/managedclusterchart/templates/policy.open-cluster-management.io_iampolicy_crd.yaml << EOF
# Copyright Contributors to the Open Cluster Management project

{{- if semverCompare "< 1.16.0" .Capabilities.KubeVersion.Version }}
$(cat .go/iam-policy-crd-v1beta1.yaml)
{{ else }}
$(cat .go/iam-policy-crd-v1.yaml)
{{- end }}
EOF

cat > pkg/addon/policyframework/manifests/managedclusterchart/templates/policy.open-cluster-management.io_policies_crd.yaml << EOF
# Copyright Contributors to the Open Cluster Management project

{{- if semverCompare "< 1.16.0" .Capabilities.KubeVersion.Version }}
$(cat .go/policy-crd-v1beta1.yaml)
{{ else }}
$(cat .go/policy-crd-v1.yaml)
{{- end }}
EOF

# Clean up the repositories - the chmod is necessary because Go makes some read-only things.
for REPO in cert-policy-controller config-policy-controller iam-policy-controller governance-policy-propagator
do
    chmod -R +rw .go/${REPO}
    rm -rf .go/${REPO}
done
