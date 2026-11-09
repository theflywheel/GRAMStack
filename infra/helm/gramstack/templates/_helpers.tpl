{{- define "gramstack.name" -}}
{{- if gt (len .root.Release.Name) 40 -}}
{{- fail "use a Helm release name of at most 40 characters" -}}
{{- end -}}
{{- printf "%s-%s" .root.Release.Name .name -}}
{{- end -}}

{{- define "gramstack.selector" -}}
app.kubernetes.io/name: gramstack
app.kubernetes.io/instance: {{ .root.Release.Name | quote }}
app.kubernetes.io/component: {{ .name | quote }}
{{- end -}}

{{- define "gramstack.pod" -}}
automountServiceAccountToken: false
securityContext:
  runAsNonRoot: true
  runAsUser: 10001
  runAsGroup: 10001
  seccompProfile:
    type: RuntimeDefault
  {{- if .settings.fileSecret }}
  fsGroup: 10001
  {{- end }}
{{- with .root.Values.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .settings.fileSecret }}
volumes:
  - name: runtime-files
    secret:
      secretName: {{ . | quote }}
      defaultMode: 288 # 0440
{{- end }}
{{- end -}}

{{- define "gramstack.container" -}}
image: {{ required (printf "%s image is required" .name) .settings.image | quote }}
imagePullPolicy: {{ .root.Values.imagePullPolicy }}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
{{- with .settings.resources }}
resources:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .secret }}
envFrom:
  - secretRef:
      name: {{ . | quote }}
{{- end }}
{{- if .settings.fileSecret }}
volumeMounts:
  - name: runtime-files
    mountPath: /etc/gramstack
    readOnly: true
{{- end }}
{{- end -}}

{{- define "gramstack.probe" -}}
{{- if .http -}}
httpGet:
  path: {{ .path }}
  port: http
{{- else -}}
tcpSocket:
  port: private
{{- end -}}
{{- end -}}
