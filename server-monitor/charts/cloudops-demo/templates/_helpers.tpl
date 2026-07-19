{{/* Common labels for the GitOps-owned demo resources. */}}
{{- define "cloudops-demo.labels" -}}
app.kubernetes.io/name: cloudops-demo-workload
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end }}

{{- define "cloudops-demo.selectorLabels" -}}
app.kubernetes.io/name: cloudops-demo-workload
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "cloudops-demo.image" -}}
{{- if .Values.image.digest -}}
{{ printf "%s@%s" .Values.image.repository .Values.image.digest }}
{{- else -}}
{{ printf "%s:%s" .Values.image.repository .Values.image.tag }}
{{- end -}}
{{- end }}
