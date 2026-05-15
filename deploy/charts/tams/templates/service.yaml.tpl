{{- if .Values.service.enabled }}
{{- if .Values.api.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "tams.fullname" . }}-api
  labels:
    {{- include "tams.labels" . | nindent 4 }}
    app.kubernetes.io/component: api
spec:
  type: {{ .Values.service.type }}
  {{- with .Values.service.api.ports }}
  ports:
    {{- range . }}
    - port: {{ .port }}
      targetPort: {{ .targetPort }}
      protocol: {{ .protocol }}
      name: {{ .name }}
    {{- end }}
  {{- end }}
  selector:
    {{- include "tams.selectorLabels" . | nindent 4 }}
    app.kubernetes.io/component: api
{{- end }}

---

{{- if .Values.ui.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "tams.fullname" . }}-ui
  labels:
    {{- include "tams.labels" . | nindent 4 }}
    app.kubernetes.io/component: ui
spec:
  type: {{ .Values.service.type }}
  {{- with .Values.service.ui.ports }}
  ports:
    {{- range . }}
    - port: {{ .port }}
      targetPort: {{ .targetPort }}
      protocol: {{ .protocol }}
      name: {{ .name }}
    {{- end }}
  {{- end }}
  selector:
    {{- include "tams.selectorLabels" . | nindent 4 }}
    app.kubernetes.io/component: ui
{{- end }}

{{- end }}
