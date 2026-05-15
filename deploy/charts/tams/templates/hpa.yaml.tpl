{{- if and .Values.api.enabled .Values.api.autoscaling.enabled }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "tams.fullname" . }}-api
  labels:
    {{- include "tams.labels" . | nindent 4 }}
    app.kubernetes.io/component: api
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ include "tams.fullname" . }}
  minReplicas: {{ .Values.api.autoscaling.minReplicas }}
  maxReplicas: {{ .Values.api.autoscaling.maxReplicas }}
  metrics:
    {{- if .Values.api.autoscaling.targetCPUUtilizationPercentage }}
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .Values.api.autoscaling.targetCPUUtilizationPercentage }}
    {{- end }}
    {{- if .Values.api.autoscaling.targetMemoryUtilizationPercentage }}
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: {{ .Values.api.autoscaling.targetMemoryUtilizationPercentage }}
    {{- end }}
{{- end }}

---

{{- if and .Values.ui.enabled .Values.ui.autoscaling.enabled }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "tams.fullname" . }}-ui
  labels:
    {{- include "tams.labels" . | nindent 4 }}
    app.kubernetes.io/component: ui
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ include "tams.fullname" . }}-ui
  minReplicas: {{ .Values.ui.autoscaling.minReplicas }}
  maxReplicas: {{ .Values.ui.autoscaling.maxReplicas }}
  metrics:
    {{- if .Values.ui.autoscaling.targetCPUUtilizationPercentage }}
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .Values.ui.autoscaling.targetCPUUtilizationPercentage }}
    {{- end }}
    {{- if .Values.ui.autoscaling.targetMemoryUtilizationPercentage }}
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: {{ .Values.ui.autoscaling.targetMemoryUtilizationPercentage }}
    {{- end }}
{{- end }}
