{{- if .Values.api.enabled }}

apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "tams.fullname" . }}
  labels:
    {{- include "tams.labels" . | nindent 4 }}
spec:
  {{- if not .Values.api.autoscaling.enabled }}
  replicas: {{ .Values.api.replicaCount }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "tams.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: api
  template:
    metadata:
      annotations:
        checksum/backends-secret: {{ include (print $.Template.BasePath "/backend-secret.yaml.tpl") . | sha256sum }}
        checksum/backend-db-secret-ref: {{ toYaml .Values.backends.db.auth | sha256sum }}
        checksum/backend-s3-secret-ref: {{ toYaml .Values.backends.s3.auth | sha256sum }}
      {{- with .Values.api.podAnnotations }}
        {{- toYaml . | nindent 8 }}
      {{- end }}
        {{- if .Values.secrets.apiToken.generate }}
        checksum/api-token-secret: {{ include (print $.Template.BasePath "/api-secret.yaml.tpl") . | sha256sum }}
        {{- end }}
      labels:
        {{- include "tams.labels" . | nindent 8 }}
        app.kubernetes.io/component: api
        {{- with .Values.api.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      {{- with .Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      serviceAccountName: {{ include "tams.serviceAccountName" . }}
      terminationGracePeriodSeconds: {{ .Values.api.terminationGracePeriodSeconds | default 30 }}
      {{- with .Values.api.podSecurityContext }}
      securityContext:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: {{ .Chart.Name }}
          {{- with .Values.api.securityContext }}
          securityContext:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          image: "{{ .Values.api.image.repository }}:{{ .Values.api.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.api.image.pullPolicy | default "IfNotPresent" }}
          command:
            - /bin/uv
            - run
            - uvicorn
            - "tamoss.app:app"
            - --host
            - "0.0.0.0"
            - --port
            - "{{ (index .Values.service.api.ports 0).port }}"
          {{- if gt (int (.Values.api.preStopSleepSeconds | default 0)) 0 }}
          lifecycle:
            preStop:
              exec:
                command: ["/bin/sh", "-c", "sleep {{ .Values.api.preStopSleepSeconds }}"]
          {{- end }}
          env:
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.backends.db.auth.existingSecret | quote }}
                  key: {{ .Values.backends.db.auth.secretKeys.username | quote }}
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.backends.db.auth.existingSecret | quote }}
                  key: {{ .Values.backends.db.auth.secretKeys.password | quote }}
            - name: TAMOSS_S3_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.backends.s3.auth.existingSecret | quote }}
                  key: {{ .Values.backends.s3.auth.secretKeys.accessKey | quote }}
            - name: TAMOSS_S3_SECRET_KEY
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.backends.s3.auth.existingSecret | quote }}
                  key: {{ .Values.backends.s3.auth.secretKeys.secretKey | quote }}
            - name: TAMOSS_AUTH_REQUIRED
              value: {{ (.Values.auth.required | default false) | quote }}
            - name: TAMOSS_TRUST_FORWARD_AUTH_HEADERS
              value: {{ (.Values.auth.trustForwardAuthHeaders | default false) | quote }}
            - name: TAMOSS_OAUTH2_ENABLED
              value: {{ (.Values.auth.oauth2.enabled | default false) | quote }}
            - name: TAMOSS_OAUTH2_ISSUER
              value: {{ .Values.auth.oauth2.issuer | default "" | quote }}
            - name: TAMOSS_OAUTH2_JWKS_URI
              value: {{ .Values.auth.oauth2.jwksUri | default "" | quote }}
            - name: TAMOSS_OAUTH2_AUDIENCE
              value: {{ .Values.auth.oauth2.audience | default "" | quote }}
            - name: TAMOSS_OAUTH2_REQUIRED_SCOPES
              value: {{ (default (list) .Values.auth.oauth2.requiredScopes) | join "," | quote }}
            - name: TAMOSS_OAUTH2_ALGORITHMS
              value: {{ (default (list "RS256") .Values.auth.oauth2.algorithms) | join "," | quote }}
          {{- if not .Values.secrets.apiToken.generate }}
            - name: TAMOSS_API_TOKEN
              value: {{ .Values.secrets.apiToken.token | quote }}
          {{- end }}
          {{- with .Values.api.env }}
            {{- range $key, $value := . }}
            - name: {{ $key }}
              value: {{ $value | quote }}
            {{- end }}
          {{- end }}
          envFrom:
            - secretRef:
                name: {{ include "tams.fullname" . }}-backends
          {{- if .Values.secrets.apiToken.generate }}
            - secretRef:
                name: {{ include "tams.fullname" . }}-api-token
          {{- end }}
          {{- with .Values.api.envFrom }}
            {{- toYaml . | nindent 12 }}
          {{- end }}
          ports:
            - name: http
              containerPort: {{ (index .Values.service.api.ports 0).port }}
              protocol: TCP
          {{- with .Values.api.livenessProbe }}
          livenessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.api.readinessProbe }}
          readinessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.api.startupProbe }}
          startupProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.api.resources }}
          resources:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.api.volumeMounts }}
          volumeMounts:
            {{- toYaml . | nindent 12 }}
          {{- end }}
      {{- with .Values.api.volumes }}
      volumes:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.api.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.api.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.api.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}

{{- end }}
---
{{- if .Values.worker.enabled }}

apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "tams.fullname" . }}-worker
  labels:
    {{- include "tams.labels" . | nindent 4 }}
    app.kubernetes.io/component: worker
spec:
  replicas: {{ .Values.worker.replicaCount }}
  selector:
    matchLabels:
      {{- include "tams.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: worker
  template:
    metadata:
      annotations:
        checksum/backends-secret: {{ include (print $.Template.BasePath "/backend-secret.yaml.tpl") . | sha256sum }}
        checksum/backend-db-secret-ref: {{ toYaml .Values.backends.db.auth | sha256sum }}
        checksum/backend-s3-secret-ref: {{ toYaml .Values.backends.s3.auth | sha256sum }}
      {{- with .Values.worker.podAnnotations }}
        {{- toYaml . | nindent 8 }}
      {{- end }}
      labels:
        {{- include "tams.labels" . | nindent 8 }}
        app.kubernetes.io/component: worker
        {{- with .Values.worker.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      {{- with .Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      serviceAccountName: {{ include "tams.serviceAccountName" . }}
      terminationGracePeriodSeconds: {{ .Values.worker.terminationGracePeriodSeconds | default 60 }}
      {{- with .Values.worker.podSecurityContext }}
      securityContext:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: {{ .Chart.Name }}-worker
          {{- with .Values.worker.securityContext }}
          securityContext:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          image: "{{ .Values.api.image.repository }}:{{ .Values.api.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.api.image.pullPolicy | default "IfNotPresent" }}
          command:
            - /bin/uv
            - run
            - python
            - -m
            - tamoss.worker
          env:
          {{- if not (hasKey .Values.worker.env "TAMOSS_WORKER_ID") }}
            - name: TAMOSS_WORKER_ID
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
          {{- end }}
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.backends.db.auth.existingSecret | quote }}
                  key: {{ .Values.backends.db.auth.secretKeys.username | quote }}
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.backends.db.auth.existingSecret | quote }}
                  key: {{ .Values.backends.db.auth.secretKeys.password | quote }}
            - name: TAMOSS_S3_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.backends.s3.auth.existingSecret | quote }}
                  key: {{ .Values.backends.s3.auth.secretKeys.accessKey | quote }}
            - name: TAMOSS_S3_SECRET_KEY
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.backends.s3.auth.existingSecret | quote }}
                  key: {{ .Values.backends.s3.auth.secretKeys.secretKey | quote }}
          {{- with .Values.worker.env }}
            {{- range $key, $value := . }}
            - name: {{ $key }}
              value: {{ $value | quote }}
            {{- end }}
          {{- end }}
          envFrom:
            - secretRef:
                name: {{ include "tams.fullname" . }}-backends
          {{- with .Values.worker.envFrom }}
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.worker.resources }}
          resources:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.worker.volumeMounts }}
          volumeMounts:
            {{- toYaml . | nindent 12 }}
          {{- end }}
      {{- with .Values.worker.volumes }}
      volumes:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.worker.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.worker.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.worker.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}

{{- end }}
---
{{- if .Values.ui.enabled }}

apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "tams.fullname" . }}-ui
  labels:
    {{- include "tams.labels" . | nindent 4 }}
spec:
  {{- if not .Values.ui.autoscaling.enabled }}
  replicas: {{ .Values.ui.replicaCount }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "tams.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: ui
  template:
    metadata:
      {{- if or .Values.secrets.apiToken.generate .Values.ui.podAnnotations }}
      annotations:
        {{- if .Values.secrets.apiToken.generate }}
        checksum/api-token-secret: {{ include (print $.Template.BasePath "/api-secret.yaml.tpl") . | sha256sum }}
        {{- end }}
      {{- with .Values.ui.podAnnotations }}
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- end }}
      labels:
        {{- include "tams.labels" . | nindent 8 }}
        app.kubernetes.io/component: ui
        {{- with .Values.ui.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      {{- with .Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      serviceAccountName: {{ include "tams.serviceAccountName" . }}
      terminationGracePeriodSeconds: {{ .Values.ui.terminationGracePeriodSeconds | default 30 }}
      {{- with .Values.ui.podSecurityContext }}
      securityContext:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: {{ .Chart.Name }}-ui
          {{- with .Values.ui.securityContext }}
          securityContext:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          image: "{{ .Values.ui.image.repository }}:{{ .Values.ui.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.ui.image.pullPolicy | default "IfNotPresent" }}
          {{- if gt (int (.Values.ui.preStopSleepSeconds | default 0)) 0 }}
          lifecycle:
            preStop:
              exec:
                command: ["/bin/sh", "-c", "sleep {{ .Values.ui.preStopSleepSeconds }}"]
          {{- end }}
          {{- if or .Values.api.enabled (not .Values.secrets.apiToken.generate) .Values.ui.env }}
          env:
          {{- if .Values.api.enabled }}
            - name: TAMOSS_API_UPSTREAM
              value: {{ printf "http://%s-api:%v" (include "tams.fullname" .) ((index .Values.service.api.ports 0).port) | quote }}
          {{- end }}
          {{- if not .Values.secrets.apiToken.generate }}
            - name: TAMOSS_API_TOKEN
              value: {{ .Values.secrets.apiToken.token | quote }}
          {{- end }}
          {{- with .Values.ui.env }}
            {{- range $key, $value := . }}
            - name: {{ $key }}
              value: {{ $value | quote }}
            {{- end }}
          {{- end }}
          {{- end }}
          {{- if or .Values.secrets.apiToken.generate .Values.ui.envFrom }}
          envFrom:
            {{- if .Values.secrets.apiToken.generate }}
            - secretRef:
                name: {{ include "tams.fullname" . }}-api-token
            {{- end }}
          {{- with .Values.ui.envFrom }}
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- end }}
          {{- with .Values.ui.ports }}
          ports:
            {{- range . }}
            - name: {{ .name }}
              containerPort: {{ .containerPort }}
              protocol: {{ .protocol }}
            {{- end }}
          {{- end }}
          {{- with .Values.ui.livenessProbe }}
          livenessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.ui.readinessProbe }}
          readinessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.ui.startupProbe }}
          startupProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.ui.resources }}
          resources:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.ui.volumeMounts }}
          volumeMounts:
            {{- toYaml . | nindent 12 }}
          {{- end }}
      {{- with .Values.ui.volumes }}
      volumes:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.ui.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.ui.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.ui.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}

{{- end }}
