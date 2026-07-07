{{/*
Expand the name of the chart.
*/}}
{{- define "aether.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "aether.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aether.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "aether.labels" -}}
helm.sh/chart: {{ include "aether.chart" . }}
{{ include "aether.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "aether.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aether.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "aether.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "aether.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
API Server labels
*/}}
{{- define "aether.apiServer.labels" -}}
{{ include "aether.labels" . }}
app.kubernetes.io/component: api-server
{{- end }}

{{/*
API Server selector labels
*/}}
{{- define "aether.apiServer.selectorLabels" -}}
{{ include "aether.selectorLabels" . }}
app.kubernetes.io/component: api-server
{{- end }}

{{/*
Return the proper image name
*/}}
{{- define "aether.image" -}}
{{- $registryName := .imageRoot.registry -}}
{{- $repositoryName := .imageRoot.repository -}}
{{- $tag := .imageRoot.tag | toString -}}
{{- if .global }}
    {{- if .global.imageRegistry }}
     {{- $registryName = .global.imageRegistry -}}
    {{- end -}}
{{- end -}}
{{- if $registryName }}
{{- printf "%s/%s:%s" $registryName $repositoryName $tag -}}
{{- else }}
{{- printf "%s:%s" $repositoryName $tag -}}
{{- end }}
{{- end }}

{{/*
Return the proper Docker Image Registry Secret Names
*/}}
{{- define "aether.imagePullSecrets" -}}
{{- if .Values.global }}
  {{- if .Values.global.imagePullSecrets }}
imagePullSecrets:
    {{- range .Values.global.imagePullSecrets }}
  - name: {{ . }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end }}

{{/*
PostgreSQL host
*/}}
{{- define "aether.postgresql.host" -}}
{{- if .Values.postgresql.enabled }}
{{- printf "%s-postgresql" (include "aether.fullname" .) }}
{{- else }}
{{- .Values.postgresql.external.host }}
{{- end }}
{{- end }}

{{/*
PostgreSQL port
*/}}
{{- define "aether.postgresql.port" -}}
{{- if .Values.postgresql.enabled }}
{{- print "5432" }}
{{- else }}
{{- .Values.postgresql.external.port }}
{{- end }}
{{- end }}

{{/*
PostgreSQL database
*/}}
{{- define "aether.postgresql.database" -}}
{{- if .Values.postgresql.enabled }}
{{- .Values.postgresql.auth.database }}
{{- else }}
{{- .Values.postgresql.external.database }}
{{- end }}
{{- end }}

{{/*
PostgreSQL secret name
*/}}
{{- define "aether.postgresql.secretName" -}}
{{- if .Values.postgresql.enabled }}
{{- if .Values.postgresql.auth.existingSecret }}
{{- .Values.postgresql.auth.existingSecret }}
{{- else }}
{{- printf "%s-postgresql" (include "aether.fullname" .) }}
{{- end }}
{{- else }}
{{- if .Values.postgresql.external.existingSecret }}
{{- .Values.postgresql.external.existingSecret }}
{{- else }}
{{- printf "%s-postgresql-external" (include "aether.fullname" .) }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Redis host
*/}}
{{- define "aether.redis.host" -}}
{{- if .Values.redis.enabled }}
{{- printf "%s-redis-master" (include "aether.fullname" .) }}
{{- else }}
{{- .Values.redis.external.host }}
{{- end }}
{{- end }}

{{/*
Redis port
*/}}
{{- define "aether.redis.port" -}}
{{- if .Values.redis.enabled }}
{{- print "6379" }}
{{- else }}
{{- .Values.redis.external.port }}
{{- end }}
{{- end }}

{{/*
Redis secret name
*/}}
{{- define "aether.redis.secretName" -}}
{{- if .Values.redis.enabled }}
{{- if .Values.redis.auth.existingSecret }}
{{- .Values.redis.auth.existingSecret }}
{{- else }}
{{- printf "%s-redis" (include "aether.fullname" .) }}
{{- end }}
{{- else }}
{{- if .Values.redis.external.existingSecret }}
{{- .Values.redis.external.existingSecret }}
{{- else }}
{{- printf "%s-redis-external" (include "aether.fullname" .) }}
{{- end }}
{{- end }}
{{- end }}
