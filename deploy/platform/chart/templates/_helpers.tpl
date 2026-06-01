{{- define "tamoss-platform.tlsMode" -}}
{{- $tls := .Values.tls | default dict -}}
{{- default "existing" (get $tls "mode") -}}
{{- end -}}

{{- define "tamoss-platform.tlsIssuerName" -}}
{{- $tls := .Values.tls | default dict -}}
{{- default "tamoss-public" (get $tls "issuerName") -}}
{{- end -}}
