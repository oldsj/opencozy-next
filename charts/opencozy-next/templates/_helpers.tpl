{{- define "app.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 50 | trimSuffix "-" -}}
{{- end -}}

{{- define "app.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
{{- end -}}

{{- define "app.labels" -}}
{{ include "app.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | quote }}
{{- end -}}

{{- define "app.dbCluster" -}}
{{- printf "%s-db" (include "app.fullname" .) -}}
{{- end -}}

{{- define "app.dbSecret" -}}
{{- if .Values.database.cnpg.enabled -}}
{{- printf "%s-app" (include "app.dbCluster" .) -}}
{{- else -}}
{{- required "database.existingSecret is required when database.cnpg.enabled is false" .Values.database.existingSecret -}}
{{- end -}}
{{- end -}}

{{- define "app.dbCASecret" -}}
{{- if .Values.database.cnpg.enabled -}}
{{- printf "%s-ca" (include "app.dbCluster" .) -}}
{{- else -}}
{{- .Values.database.caSecret -}}
{{- end -}}
{{- end -}}

{{- define "app.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else if .Values.onepassword.itemPath -}}
{{- printf "%s-secrets" (include "app.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Database env for a container. The owner URL (database.urlKey) migrates. With
database.runtime.role, serving uses the runtime role's URL and migrations grant it row access.
Args: root, ca (the CA Secret name or ""), serve (true for the app container).
*/}}
{{- define "app.dbEnv" -}}
{{- $v := .root.Values.database -}}
{{- if $v.runtime.role }}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ required "database.runtime.existingSecret is required with database.runtime.role" $v.runtime.existingSecret | quote }}
      key: {{ $v.runtime.urlKey | quote }}
- name: DATABASE_RUNTIME_ROLE
  value: {{ $v.runtime.role | quote }}
{{- if not .serve }}
- name: MIGRATION_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "app.dbSecret" .root | quote }}
      key: {{ $v.urlKey | quote }}
{{- end }}
{{- else }}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "app.dbSecret" .root | quote }}
      key: {{ $v.urlKey | quote }}
{{- end }}
{{- if .ca }}
# pgx reads these libpq variables: TLS, with the server's certificate and name checked.
- name: PGSSLMODE
  value: verify-full
- name: PGSSLROOTCERT
  value: /etc/db-ca/ca.crt
{{- end }}
{{- end -}}
