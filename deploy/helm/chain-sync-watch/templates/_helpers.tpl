{{/*
Standard naming + label helpers. Long names get truncated to 63 chars
(the k8s DNS-1123 ceiling) and have trailing "-" stripped so that
release names ending in a digit don't corrode into e.g. "csw-myapp-".
*/}}

{{- define "chain-sync-watch.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "chain-sync-watch.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "chain-sync-watch.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels — applied to every object the chart emits.
*/}}
{{- define "chain-sync-watch.labels" -}}
helm.sh/chart: {{ include "chain-sync-watch.chart" . }}
{{ include "chain-sync-watch.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "chain-sync-watch.selectorLabels" -}}
app.kubernetes.io/name: {{ include "chain-sync-watch.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Per-component selector labels — used to distinguish server / worker /
web / migrate pods under a single release.
*/}}
{{- define "chain-sync-watch.componentSelectorLabels" -}}
{{ include "chain-sync-watch.selectorLabels" . }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "chain-sync-watch.componentLabels" -}}
{{ include "chain-sync-watch.labels" . }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/*
Image reference — honors .tag falling back to Chart.appVersion.
*/}}
{{- define "chain-sync-watch.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.image.repository $tag -}}
{{- end -}}

{{- define "chain-sync-watch.imageWeb" -}}
{{- $tag := default .Chart.AppVersion .Values.imageWeb.tag -}}
{{- printf "%s/%s:%s" .Values.imageWeb.registry .Values.imageWeb.repository $tag -}}
{{- end -}}

{{- define "chain-sync-watch.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "chain-sync-watch.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Names of the child ConfigMap / Secret that server, worker, migrate all
consume via envFrom. Kept as helpers so template files don't duplicate
the naming convention.
*/}}
{{- define "chain-sync-watch.configMapName" -}}
{{- printf "%s-config" (include "chain-sync-watch.fullname" .) -}}
{{- end -}}

{{- define "chain-sync-watch.secretName" -}}
{{- printf "%s-secrets" (include "chain-sync-watch.fullname" .) -}}
{{- end -}}

{{/*
Resolve DATABASE_URL: prefer the user-supplied secret, otherwise
derive from the Bitnami postgresql subchart service when enabled.
The subchart names its primary service `<release>-postgresql`.
*/}}
{{- define "chain-sync-watch.databaseURL" -}}
{{- if .Values.secrets.DATABASE_URL -}}
{{- .Values.secrets.DATABASE_URL -}}
{{- else if .Values.postgresql.enabled -}}
{{- printf "postgres://%s:%s@%s-postgresql:5432/%s?sslmode=disable" .Values.postgresql.auth.username .Values.postgresql.auth.password .Release.Name .Values.postgresql.auth.database -}}
{{- end -}}
{{- end -}}

{{/*
Resolve REDIS_URL: prefer user-supplied, else derive from Bitnami
redis subchart. Standalone mode binds a single `<release>-redis-master`
service. auth.enabled=false means no password in the URL.
*/}}
{{- define "chain-sync-watch.redisURL" -}}
{{- if .Values.secrets.REDIS_URL -}}
{{- .Values.secrets.REDIS_URL -}}
{{- else if .Values.redis.enabled -}}
{{- printf "redis://%s-redis-master:6379/0" .Release.Name -}}
{{- end -}}
{{- end -}}
