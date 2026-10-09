{{- define "shogunWeb.fullname" -}}
shogun-web
{{- end }}

{{- define "shogunWeb.image" -}}
{{- $repo := .Values.image.repository | default "ghcr.io/0xhoaxen/shogun-web" -}}
{{ $repo }}:{{ required "values.image.tag is required" .Values.image.tag }}
{{- end }}

{{- define "shogunWeb.labels" -}}
app.kubernetes.io/name: {{ include "shogunWeb.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/part-of: shogun
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "shogunWeb.selectorLabels" -}}
app.kubernetes.io/name: {{ include "shogunWeb.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
