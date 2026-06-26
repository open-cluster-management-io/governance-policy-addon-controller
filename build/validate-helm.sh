#! /bin/bash

set -euo pipefail

BASE_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../" >/dev/null 2>&1 && pwd)
CHART_DIR="${BASE_DIR}/config/chart"
KUSTOMIZE="${BASE_DIR}/bin/kustomize"
RELEASE_NAMESPACE="open-cluster-management"

if ! command -v "${KUSTOMIZE}" &> /dev/null; then
  echo "Missing binary at path './bin/kustomize'. Run 'make kustomize' to install it."
  exit 1
fi

echo "Validating Helm chart contents..."

function report_result() {
  local description=$1
  local exit_status=$2
  local output=$3

  if [ "${exit_status}" -eq 0 ]; then
    if [ -n "${output}" ]; then
      echo "${output}"
    fi
    echo "✅ ${description} PASSED"
    return 0
  fi

  echo "❌ ${description} FAILED"
  if [ -n "${output}" ]; then
    echo "${output}"
  fi
  echo "FAILED! Failure encountered in ${description}. Missing YAML content can be resolved by regenerating the manifests:"
  echo "  make manifests"
  exit 1
}

function validate_cmd() {
  local description=$1
  shift
  local output
  local status=0
  output=$("$@") || status=$?
  report_result "${description}" "${status}" "${output}"
}

function validate_silent() {
  local description=$1
  shift
  local status=0
  "$@" >/dev/null || status=$?
  report_result "${description}" "${status}" ""
}

function validate_diff() {
  local description=${1}
  local content1=${2//\"/}
  local content2=${3//\"/}
  local unified_context=${4:-3}
  local output
  local status=0
  output=$(diff -u"${unified_context}" -L "Helm chart" -L "Kustomize" \
    <(printf '%s\n' "${content1}") \
    <(printf '%s\n' "${content2}")) || status=$?
  report_result "${description}" "${status}" "${output}"
}

echo "=== Lint Helm chart"

validate_cmd "Helm chart linting" helm lint "${CHART_DIR}"
validate_silent "Helm template validation" helm template test "${CHART_DIR}" -n "${RELEASE_NAMESPACE}"

rendered_chart=$(helm template test "${CHART_DIR}" -n "${RELEASE_NAMESPACE}")
kustomize_output=$("${KUSTOMIZE}" build "${BASE_DIR}/config/default" | yq 'select(.kind != "Namespace")')

query='[.apiVersion + "/" + .kind + "/" + .metadata.name].[]'
chart_resources=$(echo "${rendered_chart}" | yq ea "${query}" | sort)
kustomize_resources=$(echo "${kustomize_output}" | yq ea "${query}" | sort)

echo "=== Compare Helm and Kustomize output"
validate_diff "Diff chart and kustomize kinds" "${chart_resources}" "${kustomize_resources}" 1

for resource in $(echo "${rendered_chart}" | yq ea '[.kind].[]' | sort -u); do
  query="select(.kind == \"${resource}\") | del(.metadata.namespace)"
  chart_resource=$(echo "${rendered_chart}" | yq "${query}" | grep -v '^# Source:' | grep -v '^---$')
  kustomize_resource=$(echo "${kustomize_output}" | yq "${query}" | grep -v '^---$')
  validate_diff "Diff ${resource} YAML" "${chart_resource}" "${kustomize_resource}"
done
