{{- if .Values.httpRoute.enabled -}}
{{- $fullName := include "tams.fullname" . -}}
{{- $components := list
  (dict "name" "api"    "resourceSuffix" "api"    "componentLabel" "api" "enabled" .Values.api.enabled "config" .Values.httpRoute.api)
  (dict "name" "web"    "resourceSuffix" "web"    "componentLabel" "ui"  "enabled" .Values.ui.enabled  "config" .Values.httpRoute.ui)
-}}
{{- $first := true -}}
{{- range $comp := $components }}
{{- if $comp.enabled }}
{{- if not $first }}
---
{{- end }}
{{- $first = false }}
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: {{ $fullName }}-{{ $comp.resourceSuffix }}
  labels:
    {{- include "tams.labels" $ | nindent 4 }}
    app.kubernetes.io/component: {{ $comp.componentLabel }}
  {{- with $.Values.httpRoute.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  parentRefs:
    {{- toYaml $.Values.httpRoute.parentRefs | nindent 4 }}
  {{- with $comp.config.hostnames }}
  hostnames:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  rules:
    {{- $compFilters := concat (default (list) $comp.config.defaultFilters) (default (list) $comp.config.filters) }}
    {{- range $comp.config.rules }}
    {{- $baseFilters := $compFilters }}
    {{- if (get . "skipDefaultFilters") }}
    {{- $baseFilters = list }}
    {{- end }}
    {{- $ruleFilters := concat $baseFilters (default (list) .filters) }}
    - matches:
        {{- toYaml .matches | nindent 8 }}
      {{- if $ruleFilters }}
      filters:
        {{- toYaml $ruleFilters | nindent 8 }}
      {{- end }}
      backendRefs:
        {{- toYaml .backendRefs | nindent 8 }}
    {{- end }}
{{- end }}
{{- end }}
{{- end }}
