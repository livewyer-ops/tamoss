apiVersion: v1
kind: Secret
metadata:
  name: {{ include "tams.fullname" . }}-backends
  labels:
    {{- include "tams.labels" . | nindent 4 }}
type: Opaque
stringData:
  {{- with .Values.backends.db }}
  POSTGRES_HOST: {{ .host | quote }}
  POSTGRES_PORT: {{ .port | quote }}
  POSTGRES_DB: {{ .database | quote }}
  {{- end }}
  {{- with .Values.backends.s3 }}
  TAMOSS_S3_ENDPOINT: {{ .endpoint.default.url | quote }}
  {{- if .endpoint.public.url }}
  TAMOSS_S3_PUBLIC_ENDPOINT: {{ .endpoint.public.url | quote }}
  {{- end }}
  TAMOSS_S3_BUCKET: {{ .bucket | quote }}
  TAMOSS_S3_REGION: {{ .region | quote }}
  {{- end }}
