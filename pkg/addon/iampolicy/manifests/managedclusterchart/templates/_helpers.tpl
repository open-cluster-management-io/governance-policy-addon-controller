{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "controller.name" -}}
    {{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "controller.fullname" -}}
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
{{- define "controller.chart" -}}
    {{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create role name used in cluster role and binding
*/}}
{{- define "controller.rolename" -}}
    {{- .Values.org }}:{{ template "controller.fullname" . -}}
{{- end -}}

{{/*
Create role name used in role and binding for leader election
*/}}
{{- define "controller.leaderrolename" -}}
    {{ template "controller.fullname" . }}-leader
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "controller.serviceAccountName" -}}
    {{- template "controller.fullname" . -}}-sa
{{- end -}}
