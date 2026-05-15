{{- range .Values.extraObjects }}
---
{{ include "tams.render" (dict "value" . "context" $) }}
{{- end }}
