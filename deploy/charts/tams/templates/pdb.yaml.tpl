{{- if and .Values.api.enabled .Values.api.pdb.enabled }}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "tams.fullname" . }}-api
  labels:
    {{- include "tams.labels" . | nindent 4 }}
    app.kubernetes.io/component: api
spec:
  {{- if .Values.api.pdb.minAvailable }}
  minAvailable: {{ .Values.api.pdb.minAvailable }}
  {{- end }}
  {{- if and .Values.api.pdb.maxUnavailable (not .Values.api.pdb.minAvailable) }}
  maxUnavailable: {{ .Values.api.pdb.maxUnavailable }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "tams.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: api
{{- end }}

---

{{- if and .Values.ui.enabled .Values.ui.pdb.enabled }}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "tams.fullname" . }}-ui
  labels:
    {{- include "tams.labels" . | nindent 4 }}
    app.kubernetes.io/component: ui
spec:
  {{- if .Values.ui.pdb.minAvailable }}
  minAvailable: {{ .Values.ui.pdb.minAvailable }}
  {{- end }}
  {{- if and .Values.ui.pdb.maxUnavailable (not .Values.ui.pdb.minAvailable) }}
  maxUnavailable: {{ .Values.ui.pdb.maxUnavailable }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "tams.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: ui
{{- end }}

---

{{- if and .Values.worker.enabled .Values.worker.pdb.enabled }}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "tams.fullname" . }}-worker
  labels:
    {{- include "tams.labels" . | nindent 4 }}
    app.kubernetes.io/component: worker
spec:
  {{- if .Values.worker.pdb.minAvailable }}
  minAvailable: {{ .Values.worker.pdb.minAvailable }}
  {{- end }}
  {{- if and .Values.worker.pdb.maxUnavailable (not .Values.worker.pdb.minAvailable) }}
  maxUnavailable: {{ .Values.worker.pdb.maxUnavailable }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "tams.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: worker
{{- end }}

---
