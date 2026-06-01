{{- define "tamoss-platform.tlsMode" -}}
{{- $tls := .Values.tls | default dict -}}
{{- default "existing" (get $tls "mode") -}}
{{- end -}}

{{- define "tamoss-platform.tlsIssuerName" -}}
{{- $tls := .Values.tls | default dict -}}
{{- default "tamoss-public" (get $tls "issuerName") -}}
{{- end -}}

{{/*
Return a base64-encoded Secret data value, preserving existing generated data.
*/}}
{{- define "tamoss-platform.stableSecretDataValue" -}}
{{- $value := get . "value" | default "" -}}
{{- $existingData := get . "existingData" | default dict -}}
{{- $existingKey := get . "existingKey" | default "" -}}
{{- $randomLength := get . "randomLength" | default 32 | int -}}
{{- if $value -}}
{{- $value | toString | b64enc -}}
{{- else if and $existingKey (hasKey $existingData $existingKey) -}}
{{- get $existingData $existingKey -}}
{{- else -}}
{{- randAlphaNum $randomLength | b64enc -}}
{{- end -}}
{{- end -}}
