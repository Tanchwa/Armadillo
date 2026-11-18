{{- define "armadillo.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "armadillo.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "armadillo.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "armadillo.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/* Usage: include "armadillo.selectorLabels" (list $ "loader") */}}
{{- define "armadillo.selectorLabels" -}}
{{- $root := index . 0 -}}
app.kubernetes.io/name: {{ include "armadillo.name" $root }}
app.kubernetes.io/instance: {{ $root.Release.Name }}
app.kubernetes.io/component: {{ index . 1 }}
{{- end }}

{{/* Usage: include "armadillo.componentLabels" (list $ "loader") */}}
{{- define "armadillo.componentLabels" -}}
{{ include "armadillo.labels" (index . 0) }}
app.kubernetes.io/component: {{ index . 1 }}
{{- end }}

{{/* Usage: include "armadillo.image" (list $ .Values.loader.image) */}}
{{- define "armadillo.image" -}}
{{- $root := index . 0 -}}
{{- $image := index . 1 -}}
{{- printf "%s:%s" $image.repository (default $root.Chart.AppVersion $image.tag) }}
{{- end }}

{{- define "armadillo.managerServiceAccount" -}}
{{ include "armadillo.fullname" . }}-manager
{{- end }}

{{- define "armadillo.webhookServiceAccount" -}}
{{ include "armadillo.fullname" . }}-webhook
{{- end }}

{{- define "armadillo.loaderServiceAccount" -}}
{{ include "armadillo.fullname" . }}-loader
{{- end }}

{{- define "armadillo.webhookService" -}}
{{ include "armadillo.fullname" . }}-webhook
{{- end }}

{{- define "armadillo.webhookCertSecret" -}}
{{ include "armadillo.fullname" . }}-webhook-cert
{{- end }}
