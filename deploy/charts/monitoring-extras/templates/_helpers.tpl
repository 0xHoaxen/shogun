{{- define "shogunMonitoring.labels" -}}
app.kubernetes.io/name: monitoring
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: shogun
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
