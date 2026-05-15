{{- if .Values.api.enabled }}
apiVersion: v1
kind: Pod
metadata:
  name: "{{ include "tams.fullname" . }}-test-api"
  labels:
    {{- include "tams.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": test
spec:
  containers:
    - name: wget
      image: busybox:1.37.0
      command: ['wget']
      args: ['http://{{ include "tams.fullname" . }}-api:{{ (index .Values.service.api.ports 0).port }}/healthz']
  restartPolicy: Never
{{- end }}

---

{{- if .Values.ui.enabled }}
apiVersion: v1
kind: Pod
metadata:
  name: "{{ include "tams.fullname" . }}-test-ui"
  labels:
    {{- include "tams.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": test
spec:
  containers:
    - name: wget
      image: busybox:1.37.0
      command: ['wget']
      args: ['{{ include "tams.fullname" . }}-ui:{{ (index .Values.service.ui.ports 0).port }}']
  restartPolicy: Never
{{- end }}
