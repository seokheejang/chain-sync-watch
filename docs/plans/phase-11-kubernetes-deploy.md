# Phase 11 — Kubernetes 배포 (Helm)

## 목표

프로덕션·스테이징 환경에 K8s 배포. **Helm chart**로 패키징해 재사용·커스터마이즈 용이하게. OSS 사용자도 동일 chart로 자기 클러스터에 올릴 수 있어야 함.

## 산출물 (DoD)

- [ ] `deploy/helm/chain-sync-watch/` — Helm chart (사용자가 `helm install` 가능)
- [ ] `deploy/helm/chain-sync-watch/values.yaml` — 기본값
- [ ] `deploy/helm/chain-sync-watch/values.example.yaml` — 커스터마이즈 예시 (사내용, API 키·URL 실값 금지)
- [ ] 템플릿: Deployment(server, worker), Service, Ingress, ConfigMap, Secret, Job(migrate), ServiceMonitor(선택), HPA(선택)
- [ ] Postgres / Redis는 **외부 매니지드 서비스** 가정 (chart에 포함하지 않음, URL만 주입). 선택적으로 Bitnami subchart 도입 가능하게 values 플래그.
- [ ] CI로 `helm lint` + `helm template` + `kubeval`/`kubeconform` 검증
- [ ] 이미지 빌드 파이프라인: Dockerfile (멀티스테이지) + `.github/workflows/release.yml` → GHCR push
- [ ] 배포 문서 (`deploy/README.md`) — 로컬 `helm install`부터 프로덕션 시크릿 주입까지
- [ ] 환경별 values overlay (`values.dev.yaml`, `values.staging.yaml`, `values.prod.yaml`)
- [ ] 선택: ArgoCD `Application` 매니페스트 템플릿 (GitOps 배포 원할 시)

## 설계

### Chart 구조

```
deploy/helm/chain-sync-watch/
├── Chart.yaml                 (name, version, appVersion, maintainers)
├── values.yaml                기본값 (커밋)
├── values.example.yaml        커스터마이즈 템플릿 (커밋)
├── values.schema.json         (선택) values 스키마 검증
├── templates/
│   ├── _helpers.tpl           naming, labels
│   ├── configmap.yaml         configs/config.default.yaml을 ConfigMap으로
│   ├── secret.yaml            (외부 시크릿 권장, 이건 dev용)
│   ├── deployment-server.yaml HTTP server
│   ├── deployment-worker.yaml asynq worker
│   ├── service.yaml           server용
│   ├── ingress.yaml           (선택, values로 on/off)
│   ├── hpa.yaml               (선택)
│   ├── job-migrate.yaml       helm hook: pre-install/pre-upgrade
│   ├── servicemonitor.yaml    (선택, Prometheus Operator용)
│   └── NOTES.txt              설치 후 가이드
└── README.md                  chart 사용법
```

### `values.yaml` 뼈대

```yaml
image:
  repository: ghcr.io/seokheejang/chain-sync-watch
  tag: ""                      # Chart.appVersion 사용 (빈 값이면 차트 appVersion)
  pullPolicy: IfNotPresent
  pullSecrets: []

imageFrontend:                 # 프론트 별도 이미지 (선택 — 프론트가 BE와 분리 배포일 때)
  repository: ghcr.io/seokheejang/chain-sync-watch-web
  tag: ""
  pullPolicy: IfNotPresent

server:
  replicaCount: 2
  resources:
    requests: { cpu: 100m, memory: 128Mi }
    limits:   { cpu: 500m, memory: 512Mi }
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 5
    targetCPUUtilizationPercentage: 70
  service:
    type: ClusterIP
    port: 8080

worker:
  replicaCount: 1
  resources:
    requests: { cpu: 100m, memory: 128Mi }
    limits:   { cpu: 1, memory: 1Gi }
  queues:
    # 2026-04-20: Tier 기반 큐 재설계 (phase-07-queue-scheduler.md)
    default:      5
    tier-a-rpc:   10   # Tier A 전수 (자체 RPC, 높은 처리량)
    tier-b-3rd:    3   # Tier B 샘플링 (3rd-party + budget)
    tier-c-mixed:  2   # Tier C (지표별 A/B 혼합)
  # Tier B budget 정책 (RateLimitBudget port, Phase 7)
  budget:
    exhaustedPolicy: skip   # skip | defer | fail

ingress:
  enabled: false
  className: ""
  host: ""
  tls: []
  annotations: {}

migrate:
  enabled: true                # Helm hook: pre-install / pre-upgrade Job

config:
  # configs/config.default.yaml 내용을 override. 여기 값이 ConfigMap으로 감싸짐.
  server:
    addr: ":8080"
  adapters:
    rpc:
      endpoints:
        10: "https://optimism-rpc.publicnode.com"

externalSecrets:
  # 권장: ExternalSecrets Operator 또는 Secret-CSI. dev 편의상 Secret 직접 생성도 지원.
  enabled: false
  secretStoreRef: { kind: ClusterSecretStore, name: aws-secretsmanager }

secrets:
  # externalSecrets.enabled=false일 때만 사용. 값이 있으면 Secret 리소스 생성.
  databaseUrl: ""              # postgres://...
  redisUrl: ""                 # redis://...
  etherscanApiKey: ""

postgresql:
  enabled: false               # true면 Bitnami postgres subchart (dev용)
  # bitnami/postgresql 값들...

redis:
  enabled: false
  # bitnami/redis 값들...

serviceAccount:
  create: true
  name: ""
  annotations: {}              # IRSA(AWS), Workload Identity(GCP) 등

podAnnotations: {}
podSecurityContext: {}
securityContext:
  runAsNonRoot: true
  readOnlyRootFilesystem: true

prometheus:
  serviceMonitor:
    enabled: false             # Prometheus Operator 있을 때
    interval: 30s

affinity: {}
tolerations: []
nodeSelector: {}
```

### 주요 템플릿 설계 포인트

**Deployment (server)**:
- `envFrom`으로 ConfigMap + Secret 투입
- `readinessProbe`: `/readyz` (200 OK 기다림)
- `livenessProbe`: `/healthz`
- `SIGTERM` → graceful shutdown + `preStop` hook에서 sleep(5s) (LB 대기)
- `resources.requests/limits` 명시 (필수)

**Deployment (worker)**:
- probe 동일 구조지만 별도 port로 healthz 노출 필요 (worker 바이너리에 추가)
- graceful shutdown: asynq server.Stop()

**Job (migrate) with Helm hook**:
```yaml
annotations:
  "helm.sh/hook": pre-install,pre-upgrade
  "helm.sh/hook-weight": "-5"
  "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
```

**ConfigMap vs Secret 분리**:
- 비밀 아닌 설정 (서버 addr, chains 목록, rate limit) → ConfigMap
- 비밀 (DATABASE_URL, API 키) → Secret (또는 ExternalSecrets)

**ServiceMonitor (Prometheus Operator 있을 시)**:
- `/metrics` endpoint scraping 자동 설정
- values.prometheus.serviceMonitor.enabled

### 이미지 빌드 (Dockerfile)

```dockerfile
# Dockerfile (backend — 멀티스테이지)
FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGET=server  # server | worker | cli
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/app ./cmd/${TARGET}

FROM gcr.io/distroless/static:nonroot
USER nonroot:nonroot
COPY --from=builder /out/app /app
ENTRYPOINT ["/app"]
```

- distroless + nonroot: 이미지 크기 작고 공격면 최소
- 단일 Dockerfile, `--build-arg TARGET`으로 바이너리 선택
- 별도 `web/Dockerfile`: Next.js production build (standalone output)

### 릴리스 워크플로우

```
.github/workflows/release.yml:
  on:
    push: { tags: ['v*'] }
  steps:
    - checkout
    - docker buildx (linux/amd64,arm64)
    - push ghcr.io/seokheejang/chain-sync-watch:$TAG
    - push ghcr.io/seokheejang/chain-sync-watch-web:$TAG
    - helm package deploy/helm/chain-sync-watch
    - gh release upload .tgz
```

## 환경별 values 오버레이

```
deploy/helm/chain-sync-watch/
├── values.yaml              공통 기본값
└── environments/
    ├── values.dev.yaml      dev 클러스터
    ├── values.staging.yaml
    └── values.prod.yaml     prod 전용 override
```

설치:
```bash
helm install csw deploy/helm/chain-sync-watch \
  -f deploy/helm/chain-sync-watch/environments/values.prod.yaml \
  --namespace chain-sync-watch --create-namespace
```

## 세부 단계

### 11.1 Dockerfile 작성
- [ ] backend Dockerfile (멀티스테이지, distroless)
- [ ] `web/Dockerfile` (Next.js standalone output)
- [ ] `.dockerignore` 각각

### 11.2 Helm chart 스켈레톤
- [ ] `helm create` 후 불필요 파일 정리
- [ ] Chart.yaml, values.yaml 기본
- [ ] `_helpers.tpl` (이름·라벨 표준)

### 11.3 템플릿 구현
- [ ] ConfigMap (configs/config.default.yaml 주입)
- [ ] Secret (dev only; prod는 ExternalSecrets 권장)
- [ ] Deployment server + Service
- [ ] Deployment worker
- [ ] Job migrate (hook)
- [ ] ServiceMonitor (선택)
- [ ] Ingress (선택)
- [ ] HPA (선택)

### 11.4 검증
- [ ] `helm lint`
- [ ] `helm template ... | kubeconform` CI job
- [ ] kind/minikube로 로컬 설치 smoke test

### 11.5 릴리스
- [ ] `.github/workflows/release.yml` 작성
- [ ] GHCR 이미지 push 권한 확인
- [ ] 첫 릴리스 태그 (v0.1.0-rc.1 등)

### 11.6 (선택) GitOps
- [ ] ArgoCD Application YAML 템플릿 (사용자가 자기 ArgoCD에 붙일 수 있게)
- [ ] `deploy/argocd/` 디렉토리

### 11.7 배포 가이드 문서
- [ ] `deploy/README.md`:
  - Quick Start (kind + helm install)
  - 시크릿 주입 방식 선택 (inline / ExternalSecrets / CSI)
  - Postgres·Redis 매니지드 vs in-cluster 선택
  - 운영 팁 (로그·메트릭·스케일링)
- [ ] K8s 버전 호환 표 (최소 1.28 권장)

## 의존 Phase

- Phase 10 (로컬 docker-compose 통합 — 이미지·관측성 검증 후에 K8s)
- Phase 8 (HTTP API + health/metrics)
- Phase 6 (마이그레이션)
- Phase 7 (worker 바이너리)

## 주의 / 보안

- **이미지 non-root**: `runAsNonRoot: true`, `readOnlyRootFilesystem: true`
- **Secret 관리**: prod에선 K8s Secret 직접 사용 지양. ExternalSecrets Operator 또는 Secret-CSI + 클라우드 시크릿 매니저.
- **NetworkPolicy**: server ↔ db/redis 외 차단 (선택이지만 프로덕션엔 권장)
- **ServiceAccount**: 각 Deployment 전용 SA, 최소 RBAC
- **리소스 요청/제한**: 반드시 명시 (K8s scheduler, node pressure 대응)
- **PodDisruptionBudget**: replica 2 이상에 `minAvailable: 1` 권장

## 트레이드오프

- **Helm vs Kustomize**: Helm은 패키지화·redistribution 쉬움, Kustomize는 override가 명시적. OSS 배포 관점에선 **Helm 추천** (사용자가 `helm install` 한 번).
- **Postgres를 chart에 포함할지**: 기본은 **외부 매니지드** 권장 (RDS, Cloud SQL, Crunchy). dev 편의용 Bitnami subchart를 `postgresql.enabled=true`로 토글만 가능하게.
- **ArgoCD 의존**: OSS 사용자 환경이 다양하므로 **선택적 제공**. 기본은 `helm install`.

## post-MVP 확장

- Kustomize 별도 layer 제공 (Helm 외 대안)
- Argo Rollouts / Flagger (progressive delivery)
- OPA Gatekeeper 정책 예시
- Service mesh (Istio/Linkerd) 통합 가이드

## 참고

- [Helm Best Practices](https://helm.sh/docs/chart_best_practices/)
- [kubeconform](https://github.com/yannh/kubeconform)
- [ExternalSecrets Operator](https://external-secrets.io/)
- [Distroless images](https://github.com/GoogleContainerTools/distroless)
