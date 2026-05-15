{{- if .Values.secrets.apiToken.generate }}
{{- $existing := lookup "v1" "Secret" .Release.Namespace (printf "%s-api-token" (include "tams.fullname" .)) }}
{{- $token := "" }}
{{- if .Values.secrets.apiToken.token }}
  {{- $token = .Values.secrets.apiToken.token }}
{{- else if and $existing $existing.data (index $existing.data "TAMOSS_API_TOKEN") }}
  {{- $token = (index $existing.data "TAMOSS_API_TOKEN" | b64dec) }}
{{- else }}
  {{- $token = randAlphaNum 32 }}
{{- end }}
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "tams.fullname" . }}-api-token
  labels:
    {{- include "tams.labels" . | nindent 4 }}
type: Opaque
data:
  TAMOSS_API_TOKEN: {{ $token | b64enc }}
{{- end }}
