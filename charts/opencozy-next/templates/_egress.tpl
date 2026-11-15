{{/*
Slice 0c. The private allowlists, from one value each: rendered as the app's env vars (which
internal/egress enforces per request) and as NetworkPolicy peers (networkpolicy-egress.yaml).
*/}}
{{- define "app.egressEnv" -}}
{{- with .Values.egress.homeAssistant.cidrs }}
- name: HOME_ASSISTANT_ALLOWED_PRIVATE_CIDRS
  value: {{ join "," . | quote }}
{{- end }}
{{- with .Values.egress.oidc.cidrs }}
- name: OIDC_ALLOWED_PRIVATE_CIDRS
  value: {{ join "," . | quote }}
{{- end }}
{{- end }}
