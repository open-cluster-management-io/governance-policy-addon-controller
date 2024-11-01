#!/usr/bin/env bash

set -euxo pipefail # exit on errors and unset vars, and stop on the first error in a "pipeline"

ORG=${ORG:-"open-cluster-management-io"}
BRANCH=${BRANCH:-"main"}

# Fix sed issues on mac by using GSED
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
SED="sed"
if [ "${OS}" == "darwin" ]; then
  SED="gsed"
  if [ ! -x "$(command -v ${SED})" ]; then
    echo "ERROR: ${SED} required, but not found."
    echo 'Perform "brew install gnu-sed" and try again.'
    exit 1
  fi
fi

mkdir -p .go

# Clone repositories containing the CRD definitions
for REPO in config-policy-controller governance-policy-propagator; do
    # Try a given ORG/BRANCH, but fall back to the open-cluster-management-io org on the main branch if it fails
    git clone -b "${BRANCH}" --depth 1 https://github.com/${ORG}/${REPO}.git .go/${REPO} ||
        git clone -b main --depth 1 https://github.com/open-cluster-management-io/${REPO}.git .go/${REPO}
done

format_descriptions() {
    crd_path=${1}

    ${SED} -i 's/ description: |-/ description: >-/g' "${crd_path}"
}

(
    cd .go/config-policy-controller
    # ConfigurationPolicy CRD
    format_descriptions deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml
    cp deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml ../config-policy-crd-v1.yaml
    # OperatorPolicy CRD
    format_descriptions deploy/crds/policy.open-cluster-management.io_operatorpolicies.yaml
    cp deploy/crds/policy.open-cluster-management.io_operatorpolicies.yaml ../operator-policy-crd-v1.yaml
)

(
    cd .go/governance-policy-propagator
    # Policy CRD
    format_descriptions deploy/crds/policy.open-cluster-management.io_policies.yaml
    cp deploy/crds/policy.open-cluster-management.io_policies.yaml ../policy-crd-v1.yaml
)

crdPrefix='# Copyright Contributors to the Open Cluster Management project
'

addLocationLabel='.metadata.labels += {"addon.open-cluster-management.io/hosted-manifest-location": "hosting"}'

# This annotation must *only* be added on the hub cluster. On others, we want the CRD removed.
# This kind of condition is not valid YAML on its own, so it has to be hacked in.
addTempAnnotation='.metadata.annotations += {"SEDTARGET": "SEDTARGET"}'
replaceAnnotation='s/SEDTARGET: SEDTARGET/{{ if .Values.onMulticlusterHub }}"addon.open-cluster-management.io\/deletion-orphan": ""{{ end }}/g'

cat >pkg/addon/configpolicy/manifests/managedclusterchart/templates/policy.open-cluster-management.io_configurationpolicies_crd.yaml <<EOF
${crdPrefix}
$(yq e "$addLocationLabel" .go/config-policy-crd-v1.yaml)
EOF

cat >pkg/addon/configpolicy/manifests/managedclusterchart/templates/policy.open-cluster-management.io_operatorpolicies_crd.yaml <<EOF
$(yq e "$addLocationLabel" .go/operator-policy-crd-v1.yaml)
EOF

cat >pkg/addon/policyframework/manifests/managedclusterchart/templates/policy.open-cluster-management.io_policies_crd.yaml <<EOF
${crdPrefix}
$(yq e "$addTempAnnotation | $addLocationLabel" .go/policy-crd-v1.yaml | sed -E "$replaceAnnotation")
EOF

# Clean up the repositories - the chmod is necessary because Go makes some read-only things.
for REPO in config-policy-controller governance-policy-propagator; do
    chmod -R +rw .go/${REPO}
    rm -rf .go/${REPO}
done
