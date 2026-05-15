{{- define "tams-platform.values" -}}
{{- if hasKey .Values "platform" -}}
{{- toYaml .Values.platform -}}
{{- else -}}
{{- toYaml .Values -}}
{{- end -}}
{{- end -}}
