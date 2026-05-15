{{- if .Values.ingress.enabled -}}
{{- $fullName := include "tams.fullname" . -}}
{{- $components := list
  (dict "name" "api" "resourceSuffix" "api" "serviceSuffix" "api" "componentLabel" "api" "enabled" .Values.api.enabled "config" .Values.ingress.api "servicePortsKey" "api" "servicePortName" "http")
  (dict "name" "web" "resourceSuffix" "web" "serviceSuffix" "ui" "componentLabel" "ui" "enabled" .Values.ui.enabled "config" .Values.ingress.ui.web "servicePortsKey" "ui" "servicePortName" "http")
-}}
{{- $first := true -}}
{{- range $comp := $components }}
{{- if $comp.enabled }}
{{- if not $first }}
---
{{- end }}
{{- $first = false }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ $fullName }}-{{ $comp.resourceSuffix }}
  labels:
    {{- include "tams.labels" $ | nindent 4 }}
    app.kubernetes.io/component: {{ $comp.componentLabel }}
  {{- with $.Values.ingress.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  {{- with $.Values.ingress.className }}
  ingressClassName: {{ . }}
  {{- end }}
  {{- if $.Values.ingress.tls }}
  tls:
    {{- range $.Values.ingress.tls }}
    - hosts:
        {{- range .hosts }}
        - {{ . | quote }}
        {{- end }}
      secretName: {{ .secretName }}
    {{- end }}
  {{- end }}
  rules:
    - host: {{ $comp.config.host | quote }}
      http:
        paths:
          {{- range $comp.config.paths }}
          {{- if or .portName .portNumber .port }}
          - path: {{ .path }}
            {{- with .pathType }}
            pathType: {{ . }}
            {{- end }}
            backend:
              service:
                name: {{ $fullName }}-{{ $comp.serviceSuffix }}
                port:
                  {{- if .portName }}
                  name: {{ .portName }}
                  {{- else if .portNumber }}
                  number: {{ .portNumber }}
                  {{- else if .port.name }}
                  name: {{ .port.name }}
                  {{- else }}
                  number: {{ .port.number }}
                  {{- end }}
          {{- else }}
          {{- /* Auto-expose matching service port */}}
          {{- $pathConfig := . }}
          {{- range (index $.Values.service $comp.servicePortsKey).ports }}
          {{- if eq .name $comp.servicePortName }}
          - path: {{ $pathConfig.path }}
            {{- with $pathConfig.pathType }}
            pathType: {{ . }}
            {{- end }}
            backend:
              service:
                name: {{ $fullName }}-{{ $comp.serviceSuffix }}
                port:
                  name: {{ .name }}
          {{- end }}
          {{- end }}
          {{- end }}
          {{- end }}
{{- end }}
{{- end }}
{{- end }}
