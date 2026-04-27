# Phase 11 — Kubernetes 배포 (Helm)

## 목표

프로덕션·스테이징 환경에 K8s 배포. **Helm chart**로 패키징해 재사용·커스터마이즈 용이하게. OSS 사용자도 동일 chart로 자기 클러스터에 올릴 수 있어야 함.

## 진행 현황 (2026-04-27)

작업을 4개 슬라이스로 쪼개 진행. 슬라이스 1·2·3·4 모두 완료. **Phase 11 종료.**

- ✅ **슬라이스 1 — MVP chart** (2026-04-24): server / worker / web Deployment + Service, migrate Job(helm hook), ConfigMap + Secret (값 주입 → chart 가 k8s Secret 렌더), ServiceAccount, Postgres/Redis Bitnami subchart(`enabled=true` 기본, subchart off 시 외부 URL 주입), NOTES.txt, chart README, top-level `deploy/README.md`, `values.example.yaml`, gitignore 보강. `helm dependency update` + `helm lint` + `helm template` smoke OK (25 리소스, fail-fast 3종 동작 확인).
- ✅ **슬라이스 2 — prod-ready features** (2026-04-27): `templates/ingress.yaml` (web + `/api` 라우팅, TLS), `templates/hpa.yaml` (server / worker / web range loop, autoscaling/v2, CPU + 선택 memory metric), `templates/pdb.yaml` (3 컴포넌트 range, minAvailable XOR maxUnavailable 빈문자열 sentinel), `templates/servicemonitor.yaml` (Prometheus Operator 스캐폴드 — `/metrics` 미구현 상태에서 미리 와이어업), `templates/externalsecret.yaml` + Secret 템플릿 short-circuit (externalSecrets.enabled=true 시 inline Secret 비렌더). `values.schema.json` JSON Schema (필수 키·enum 검증). `environments/values.{dev,staging,prod}.yaml` 3종 overlay (dev=single-replica + 번들 subchart, staging=HPA+PDB+Ingress+ServiceMonitor, prod=managed DB + ExternalSecrets-only). Deployment 3종 모두 `spec.replicas` 가 autoscaling.enabled 시 생략 (HPA 가 필드 owner). `helm template` 풀세트(33 리소스) + 환경별(dev=25, staging=31, prod=17) 검증 OK + ExternalSecrets fail-fast 2종 + schema enum 거부 확인.
- ✅ **슬라이스 3 — 릴리스 파이프라인** (2026-04-27): `.github/workflows/helm.yml` chart CI (PR/push to `deploy/helm/**` → `helm dependency update` + `helm lint` + `helm template | kubeconform -strict`, dev/staging/prod 3 환경 + 풀-feature 5번째 패스. CRD schema 는 datreeio/CRDs-catalog 에서 fetch, `-ignore-missing-schemas` 로 미수록 CRD 는 skip). `.github/workflows/release.yml` 태그 push 트리거 (`v*`): 4-job DAG — `resolve` (semver 검증 + version 추출) → `build-backend` + `build-web` (`docker/setup-qemu-action` + `docker/setup-buildx-action` + `docker/build-push-action`, multi-arch `linux/amd64,linux/arm64`, `cache-from/to: type=gha,scope=...,mode=max`) → `package-helm` (Chart.yaml `version`/`appVersion` 을 태그로 sed-치환 후 `helm package` + `helm push oci://ghcr.io/<owner>/charts` + `softprops/action-gh-release` 로 .tgz 첨부). 권한: `contents:write` (Release) + `packages:write` (GHCR), `GITHUB_TOKEN` 만 사용 (PAT 불필요). `workflow_dispatch` 입력으로 기존 태그 재실행 가능. 로컬 검증: kubeconform 3환경 0 errors, `helm package` (Chart.yaml 임시 9.9.9 치환) tgz 생성 OK.
- ✅ **슬라이스 4 (선택)** (2026-04-27): `deploy/argocd/application.yaml` (단일 Application, git source 기본 + OCI source 주석 변형, `prune+selfHeal+CreateNamespace+ServerSideApply` syncOptions, `resources-finalizer.argocd.argoproj.io` 정리 보장), `deploy/argocd/applicationset.yaml` (list generator 로 dev/staging/prod 3 환경 fan-out), `deploy/argocd/README.md` (git vs OCI 패턴 비교, 시크릿 처리 가이드, sync wave 동작 메모). `deploy/scripts/kind-smoke.sh` — kind cluster 생성 → 백엔드 + web 이미지 로컬 빌드 → `kind load docker-image` 로 클러스터 주입 → `helm dependency update` + `helm upgrade --install` (dev overlay + image.tag=smoke) + `kubectl wait` → port-forward + `/healthz` curl. Make 타겟 `helm-deps` / `helm-lint` / `helm-template` / `kind-smoke` / `kind-smoke-down` 추가, `make helm-lint` 3 환경 통과 확인.

## 산출물 (DoD)

- [x] `deploy/helm/chain-sync-watch/` — Helm chart (사용자가 `helm install` 가능)  *[슬라이스 1]*
- [x] `deploy/helm/chain-sync-watch/values.yaml` — 기본값  *[슬라이스 1]*
- [x] `deploy/helm/chain-sync-watch/values.example.yaml` — 커스터마이즈 예시 (사내용, API 키·URL 실값 금지)  *[슬라이스 1]*
- [x] 템플릿: Deployment(server, worker, **web**), Service, ConfigMap, Secret, Job(migrate), ServiceAccount, NOTES.txt  *[슬라이스 1]*
- [x] 템플릿: Ingress, HPA, PDB, ServiceMonitor  *[슬라이스 2]*
- [x] `values.schema.json` 검증  *[슬라이스 2]*
- [x] ExternalSecrets Operator 지원 (`templates/externalsecret.yaml` + Secret 템플릿 short-circuit)  *[슬라이스 2]*
- [x] Postgres / Redis — **Bitnami subchart 기본 enabled** (단일 `helm install` 목표). `enabled=false` 로 끄면 `.secrets.DATABASE_URL` / `.secrets.REDIS_URL` 수동 주입.  *[슬라이스 1 — 원안의 "외부 전제" 에서 변경]*
- [x] CI 로 `helm lint` + `helm template` + `kubeconform` 검증  *[슬라이스 3]*
- [x] 이미지 빌드 파이프라인: Dockerfile (멀티스테이지) — 10b 에서 이미 존재  *[슬라이스 1 전제]*
- [x] `.github/workflows/release.yml` → GHCR push (backend + web 이미지 multi-arch + Helm chart OCI publish + GitHub Release)  *[슬라이스 3]*
- [x] 배포 문서 (`deploy/README.md`) — 로컬 `helm install` 부터 시크릿 주입 패턴까지  *[슬라이스 1]*
- [x] 환경별 values overlay (`environments/values.dev.yaml`, `values.staging.yaml`, `values.prod.yaml`)  *[슬라이스 2]*
- [x] 선택: ArgoCD `Application` + `ApplicationSet` 매니페스트 템플릿 (`deploy/argocd/`)  *[슬라이스 4]*
- [x] 선택: kind 로컬 스모크 스크립트 (`deploy/scripts/kind-smoke.sh` + `make kind-smoke`)  *[슬라이스 4]*

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
