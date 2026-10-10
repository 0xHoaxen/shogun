{{- define "shogunPostgres.fullname" -}}
shogun-postgres
{{- end }}

{{- define "shogunPostgres.labels" -}}
app.kubernetes.io/name: {{ include "shogunPostgres.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/part-of: shogun
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/* Name only: a StatefulSet selector cannot change after creation. */}}
{{- define "shogunPostgres.selectorLabels" -}}
app.kubernetes.io/name: {{ include "shogunPostgres.fullname" . }}
{{- end }}
