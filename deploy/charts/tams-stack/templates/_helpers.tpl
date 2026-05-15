{{- define "tams-stack.postgresqlSecretName" -}}
{{- $creds := .Values.credentials.postgresql -}}
{{- default "tams-postgresql-auth" (default $creds.secretName $creds.existingSecret) -}}
{{- end -}}

{{- define "tams-stack.rustfsSecretName" -}}
{{- $creds := .Values.credentials.rustfs -}}
{{- default "tams-rustfs-auth" (default $creds.secretName $creds.existingSecret) -}}
{{- end -}}

{{- define "tams-stack.rustfsFullname" -}}
{{- if .Values.rustfs.fullnameOverride -}}
{{- .Values.rustfs.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default "rustfs" .Values.rustfs.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}
