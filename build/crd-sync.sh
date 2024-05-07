#!/usr/bin/env bash

set -euxo pipefail  # exit on errors and unset vars, and stop on the first error in a "pipeline"

ORG=${ORG:-"open-cluster-management-io"}
BRANCH=${BRANCH:-"main"}

mkdir -p .go

# Clone repositories containing the CRD definitions
for REPO in config-policy-controller governance-policy-propagator
do
    # Try a given ORG/BRANCH, but fall back to the open-cluster-management-io org on the main branch if it fails
    git clone -b "${BRANCH}" --depth 1 https://github.com/${ORG}/${REPO}.git .go/${REPO} \
    || git clone -b main --depth 1 https://github.com/open-cluster-management-io/${REPO}.git .go/${REPO}
done

generate_v1beta1() {
    CRD_PATH=${1}
    yq '.apiVersion += "beta1"' -i ${CRD_PATH}
    yq '.spec.version = "v1"' -i ${CRD_PATH}
    yq '.spec.additionalPrinterColumns = .spec.versions[].additionalPrinterColumns' -i ${CRD_PATH}
    yq '.spec.additionalPrinterColumns[] |= .JSONPath = .jsonPath' -i ${CRD_PATH}
    yq 'del(.spec.additionalPrinterColumns[].jsonPath)' -i ${CRD_PATH}
    yq '.spec.validation = .spec.versions[].schema' -i ${CRD_PATH}
    yq '.spec.subresources.status = {}' -i ${CRD_PATH}
    yq '.spec.versions = [{"name": "v1", "served": true, "storage": true}]' -i ${CRD_PATH}
    yq 'del(.. | select(has("default")).default)' -i ${CRD_PATH}
    yq 'del(.. | select(has("oneOf")).oneOf)' -i ${CRD_PATH}
    yq 'sort_keys(..)' -i ${CRD_PATH}
}

(
    cd .go/config-policy-controller
    # ConfigurationPolicy CRD
    cp deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml ../config-policy-crd-v1.yaml
    cp deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml ../config-policy-crd-v1beta1.yaml
    generate_v1beta1 ../config-policy-crd-v1beta1.yaml
    # OperatorPolicy CRD (v1beta1 not required since it's not supported on earlier K8s)
    cp deploy/crds/policy.open-cluster-management.io_operatorpolicies.yaml ../operator-policy-crd-v1.yaml
)

(
    cd .go/governance-policy-propagator
    # Policy CRD
    cp deploy/crds/policy.open-cluster-management.io_policies.yaml ../policy-crd-v1.yaml
    cp deploy/crds/policy.open-cluster-management.io_policies.yaml ../policy-crd-v1beta1.yaml
    generate_v1beta1 ../policy-crd-v1beta1.yaml
)

crdPrefix='# Copyright Contributors to the Open Cluster Management project

{{- if semverCompare "< 1.16.0" (.Values.hostingClusterCapabilities.KubeVersion.Version | default .Capabilities.KubeVersion.Version) }}'

addLocationLabel='.metadata.labels += {"addon.open-cluster-management.io/hosted-manifest-location": "hosting"}'
addTemplateLabel='.metadata.labels += {"policy.open-cluster-management.io/policy-type": "template"}'

# This annotation must *only* be added on the hub cluster. On others, we want the CRD removed. 
# This kind of condition is not valid YAML on its own, so it has to be hacked in.
addTempAnnotation='.metadata.annotations += {"SEDTARGET": "SEDTARGET"}'
replaceAnnotation='s/SEDTARGET: SEDTARGET/{{ if .Values.onMulticlusterHub }}"addon.open-cluster-management.io\/deletion-orphan": ""{{ end }}/g'

cat > pkg/addon/configpolicy/manifests/managedclusterchart/templates/policy.open-cluster-management.io_configurationpolicies_crd.yaml << EOF
${crdPrefix}
$(yq e "$addLocationLabel | $addTemplateLabel" .go/config-policy-crd-v1beta1.yaml)
{{ else }}
$(yq e "$addLocationLabel" .go/config-policy-crd-v1.yaml)
{{- end }}
EOF

cat > pkg/addon/configpolicy/manifests/managedclusterchart/templates/policy.open-cluster-management.io_operatorpolicies_crd.yaml << EOF
$(echo "${crdPrefix}" | sed 's/</>/')
$(yq e "$addLocationLabel" .go/operator-policy-crd-v1.yaml)
{{- end }}
EOF

cat > pkg/addon/policyframework/manifests/managedclusterchart/templates/policy.open-cluster-management.io_policies_crd.yaml << EOF
${crdPrefix}
$(yq e "$addTempAnnotation | $addLocationLabel" .go/policy-crd-v1beta1.yaml | sed -E "$replaceAnnotation")
{{ else }}
$(yq e "$addTempAnnotation | $addLocationLabel" .go/policy-crd-v1.yaml | sed -E "$replaceAnnotation")
{{- end }}
EOF

# Clean up the repositories - the chmod is necessary because Go makes some read-only things.
for REPO in config-policy-controller governance-policy-propagator
do
    chmod -R +rw .go/${REPO}
    rm -rf .go/${REPO}
done
