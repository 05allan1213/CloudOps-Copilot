{{/* Common labels for all resources owned by the canonical CloudOps chart. */}}
{{- define "cloudops.labels" -}}
app.kubernetes.io/part-of: cloudops
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}

{{/* Prefer immutable image digests when supplied; retain tags for local builds. */}}
{{- define "cloudops.image" -}}
{{- if .digest -}}
{{ printf "%s@%s" .repository .digest }}
{{- else -}}
{{ printf "%s:%s" .repository .tag }}
{{- end -}}
{{- end }}

{{/* Fixed resource names are intentional deployment and render contracts. */}}
{{- define "cloudops.configMapName" -}}cloudops-config{{- end }}
{{- define "cloudops.apiName" -}}cloudops-api{{- end }}
{{- define "cloudops.apiServiceName" -}}cloudops-api{{- end }}
{{- define "cloudops.apiManagementServiceName" -}}cloudops-api-management{{- end }}
{{- define "cloudops.workerName" -}}cloudops-worker{{- end }}
