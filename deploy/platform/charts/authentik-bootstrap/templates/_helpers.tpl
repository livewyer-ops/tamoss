{{/*
Return a base64-encoded Secret data value, preserving existing generated data.
*/}}
{{- define "tamoss-authentik-bootstrap.stableSecretDataValue" -}}
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
