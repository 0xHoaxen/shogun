{{- define "shogun.service" -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "shogun.fullname" . }}
  labels:
    {{- include "shogun.labels" . | nindent 4 }}
spec:
  selector:
    {{- include "shogun.selectorLabels" . | nindent 4 }}
  ports:
    - name: grpc
      port: {{ .Values.ports.grpc }}
      targetPort: grpc
    - name: http
      port: {{ .Values.ports.http }}
      targetPort: http
    {{- if .Values.publicPort.enabled }}
    - name: public
      port: {{ .Values.publicPort.port }}
      targetPort: public
    {{- end }}
{{- end }}
