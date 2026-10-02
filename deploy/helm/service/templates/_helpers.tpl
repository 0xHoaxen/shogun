{{- define "shogun.name" -}}
{{- required "values.name is required" .Values.name -}}
{{- end }}

{{- define "shogun.fullname" -}}
shogun-{{ include "shogun.name" . }}
{{- end }}

{{- define "shogun.image" -}}
{{- $repo := .Values.image.repository | default (printf "ghcr.io/0xhoaxen/shogun-%s" (include "shogun.name" .)) -}}
{{ $repo }}:{{ .Values.image.tag }}
{{- end }}

{{- define "shogun.secretName" -}}
{{ .Values.existingSecret | default (include "shogun.fullname" .) }}
{{- end }}

{{- define "shogun.labels" -}}
app.kubernetes.io/name: {{ include "shogun.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/part-of: shogun
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "shogun.selectorLabels" -}}
app.kubernetes.io/name: {{ include "shogun.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Environment shared by the pod and the migration job. */}}
{{- define "shogun.env" -}}
- name: GRPC_PORT
  value: {{ .Values.ports.grpc | quote }}
- name: HTTP_PORT
  value: {{ .Values.ports.http | quote }}
- name: MIGRATE_ON_START
  value: {{ .Values.migrateOnStart | quote }}
{{- range $key, $value := .Values.env }}
- name: {{ $key }}
  value: {{ $value | quote }}
{{- end }}
{{- range $peer := .Values.peers }}
- name: {{ upper $peer }}_ADDR
  value: {{ printf "shogun-%s:%v" $peer $.Values.ports.grpc | quote }}
{{- end }}
{{- end }}
