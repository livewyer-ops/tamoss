{{- if .Values.networkPolicy.enabled -}}
{{- $components := list
  (dict "name" "api" "enabled" .Values.api.enabled "config" .Values.networkPolicy.api)
  (dict "name" "ui" "enabled" .Values.ui.enabled "config" .Values.networkPolicy.ui)
  (dict "name" "worker" "enabled" .Values.worker.enabled "config" .Values.networkPolicy.worker)
-}}
{{- $first := true -}}
{{- range $comp := $components }}
{{- if $comp.enabled }}
{{- if and (ne $comp.name "worker") (empty $comp.config.ingress) }}
{{- fail (printf "networkPolicy.%s.ingress must be supplied when networkPolicy.enabled=true and %s.enabled=true" $comp.name $comp.name) }}
{{- end }}
{{- if not $first }}
---
{{- end }}
{{- $first = false }}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "tams.fullname" $ }}-{{ $comp.name }}
  labels:
    {{- include "tams.labels" $ | nindent 4 }}
    app.kubernetes.io/component: {{ $comp.name }}
spec:
  podSelector:
    matchLabels:
      {{- include "tams.selectorLabels" $ | nindent 6 }}
      app.kubernetes.io/component: {{ $comp.name }}
  policyTypes:
    - Ingress
    - Egress
  ingress:
    {{- toYaml (default (list) $comp.config.ingress) | nindent 4 }}
  egress:
    {{- toYaml (default (list) $comp.config.egress) | nindent 4 }}
{{- end }}
{{- end }}
{{- end }}
