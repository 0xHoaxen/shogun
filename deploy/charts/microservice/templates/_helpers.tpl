{{- define "microservice.name" -}}
{{- required "values.name is required" .Values.name -}}
{{- end }}

{{- define "microservice.image" -}}
{{- $repo := .Values.image.repository | default (printf "ghcr.io/0xhoaxen/%s" (include "microservice.name" .)) -}}
{{ $repo }}:{{ required "values.image.tag is required" .Values.image.tag }}
{{- end }}

{{- define "microservice.secretName" -}}
{{ .Values.secret.name | default (include "microservice.name" .) }}
{{- end }}

{{- define "microservice.labels" -}}
app.kubernetes.io/name: {{ include "microservice.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/part-of: shogun
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "microservice.selectorLabels" -}}
app.kubernetes.io/name: {{ include "microservice.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Environment shared by the pod and the migration job. May be empty. */}}
{{- define "microservice.env" -}}
{{- if .Values.serviceEnv }}
- name: GRPC_PORT
  value: {{ .Values.ports.grpc | quote }}
- name: HTTP_PORT
  value: {{ .Values.ports.http | quote }}
- name: MIGRATE_ON_START
  value: {{ .Values.migrateOnStart | quote }}
{{- end }}
{{- range $key, $value := .Values.env }}
- name: {{ $key }}
  value: {{ $value | quote }}
{{- end }}
{{- if .Values.serviceEnv }}
{{- range $peer := .Values.peers }}
- name: {{ upper $peer }}_ADDR
  value: {{ printf "%s:%v" $peer $.Values.ports.grpc | quote }}
{{- end }}
{{- end }}
{{- end }}

{{/* Container security settings shared by the pod and the migration job. */}}
{{- define "microservice.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
{{- end }}

{{/* env and envFrom, shared by the pod and the migration job. */}}
{{- define "microservice.envBlock" -}}
{{- with (include "microservice.env" . | trim) }}
env:
  {{- . | nindent 2 }}
{{- end }}
{{- if .Values.secret.enabled }}
envFrom:
  - secretRef:
      name: {{ include "microservice.secretName" . }}
{{- end }}
{{- end }}

{{/* imagePullSecrets and the pod security context, shared by the pod and the migration job. */}}
{{- define "microservice.podSpecCommon" -}}
{{- with .Values.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
securityContext:
  {{- toYaml .Values.podSecurityContext | nindent 2 }}
{{- end }}
