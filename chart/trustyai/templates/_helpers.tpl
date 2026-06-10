{{- define "trustyai.name" -}}
trustyai-service-operator
{{- end -}}

{{- define "trustyai.labels" -}}
control-plane: trustyai-service-operator
app.kubernetes.io/name: {{ include "trustyai.name" . }}
app.kubernetes.io/part-of: trustyai-service-operator
app.kubernetes.io/managed-by: helm
{{- end -}}

{{- define "trustyai.selectorLabels" -}}
control-plane: trustyai-service-operator
{{- end -}}
