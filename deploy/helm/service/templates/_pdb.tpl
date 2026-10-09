{{- define "shogun.pdb" -}}
{{- if .Values.podDisruptionBudget.enabled }}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "shogun.fullname" . }}
  labels:
    {{- include "shogun.labels" . | nindent 4 }}
spec:
  minAvailable: {{ .Values.podDisruptionBudget.minAvailable }}
  selector:
    matchLabels:
      {{- include "shogun.selectorLabels" . | nindent 6 }}
{{- end }}
{{- end }}
