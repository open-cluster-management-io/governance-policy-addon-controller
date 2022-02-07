{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "iamPolicyController.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Define the full name.
*/}}
{{- define "iamPolicyController.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "iamPolicyController.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "iamPolicyController.serviceAccount" -}}
  {{- template "iamPolicyController.fullname" . -}}-sa
{{- end -}}

{{/*
Create the name of the cluster Role to use
*/}}
{{- define "iamPolicyController.clusterRole" -}}
  {{- template "iamPolicyController.fullname" . -}}-role
{{- end -}}

{{/*
Create the name of the cluster Rolebinding to use
*/}}
{{- define "iamPolicyController.clusterRoleBinding" -}}
  {{- template "iamPolicyController.fullname" . -}}-rolebinding
{{- end -}}

{{/*
Create the name of the leader election role to use
*/}}
{{- define "iamPolicyController.leaderElectionRole" -}}
  {{- template "iamPolicyController.fullname" . -}}-leader-election-role
{{- end -}}

{{/*
Create the name of the leader election role binding to use
*/}}
{{- define "iamPolicyController.leaderElectionRoleBinding" -}}
  {{- template "iamPolicyController.fullname" . -}}-leader-election-rolebinding
{{- end -}}
