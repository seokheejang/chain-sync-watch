# Phase 10 — Integration / Observability / Deploy

Phase 10은 두 단계로 쪼갭니다:

- **Phase 10a — Source Configuration Store** (이 문서 상단 섹션). DB-backed 어댑터 구성, 암호화된 시크릿, YAML 시드, `/sources` CRUD UI. ExecuteRun과 ReplayDiff가 실제 데이터를 쓰기 위한 필수 전제.
- **Phase 10b — Observability / Deploy** (이 문서 기존 섹션). 로깅·메트릭 통일, docker-compose 통합, E2E, 배포 가이드.

---

## Phase 10a — Source Configuration Store

### 결정 사항 (2026-04-23 컨설트)

| 항목 | 결정 |
|---|---|
| 아키텍처 | **하이브리드: YAML 시드 → DB 단일 진실** |
| YAML 역할 | `go:embed defaults.yaml`로 번들된 초기 시드. `csw migrate seed`가 DB 빈 경우에만 1회 삽입. 런타임 머징 없음 |
| 시크릿 저장 | **DB 암호화 컬럼**. AES-GCM, 마스터 키는 단일 env var (`CSW_SECRET_KEY`). 외부 KMS는 post-MVP |
| 인증 (Phase 10a) | **없음** — 로컬 MVP 가정. README에 "프로덕션은 reverse proxy 인증 앞단 필수" 경고 |
| 인증 (팀 공유 시) | **Option A**: Caddy/nginx basic auth 앞단 (docker-compose에 옵셔널 서비스). 앱 코드 불변 |
| 인증 (나중에) | Option C: oauth2-proxy or NextAuth OIDC. 이메일 allow-list env var → 향후 DB role 테이블로 승격 |
| UI 구조 | `/sources` 리소스 페이지에 **CRUD 통합**. 별도 `/admin` 라우트 분리 안 함. API는 role-aware 미들웨어 자리(slot)만 비워둠 |
| 배포 모델 | 시작은 개인 로컬 → 팀 공유 (single instance, multi-user). multi-tenant SaaS는 비 범위 |

### 산출물 (DoD)

- [ ] **migration 007** — `sources` 테이블 (id, type, chain_id, endpoint, secret_ciphertext, secret_nonce, enabled, created_at, updated_at)
- [ ] `internal/secrets/` — AES-GCM 래퍼 (`Encrypt([]byte, []byte) ([]byte, nonce, error)`, `Decrypt`). 마스터 키는 `CSW_SECRET_KEY` env에서 읽음 (32바이트 base64); 없으면 앱 부팅 실패
- [ ] `internal/infrastructure/persistence/source_repository.go` — gorm 기반 CRUD + 도메인 변환 (`SourceConfig` 값객체)
- [ ] `internal/infrastructure/gateway/` — DB-backed `SourceGateway` 구현체. `ForChain(id)` 호출 시 DB에서 enabled=true 행 로드 → 어댑터 타입별 팩토리 (`adapters/rpc/New`, `adapters/blockscout/New`, `adapters/routescan/New`) 호출
- [ ] `ChainHead` 실제 구현 — RPC 어댑터 재사용 (blockNumber + `finalized` tag). `stubs.NullChainHead`는 폴백으로만
- [ ] `csw migrate seed` 서브커맨드 — 임베디드 defaults.yaml을 읽어 sources 테이블 빈 경우에만 INSERT. api_key 필드가 env var 레퍼런스(`env:CSW_FOO_KEY` 형식)면 env 값을 읽어 암호화 후 삽입
- [ ] httpapi 라우트:
  - `POST /sources` — 새 소스 생성 (CreateSourceRequest: type, chain_id, endpoint, api_key optional)
  - `PUT /sources/{id}` — 업데이트
  - `DELETE /sources/{id}` — 소프트 삭제 (enabled=false)
  - `POST /sources/{id}/test` — 어댑터 연결 smoke-test (선택; post-10a OK)
- [ ] API 미들웨어 role-aware slot — `middleware/role.go`. Phase 10a에선 no-op (항상 통과). Phase 10b 팀 공유 시 enforcement 켬
- [ ] `web/app/sources/` — CRUD UI (create dialog, edit, delete 확인 다이얼로그). api_key 입력은 password field + 서버 응답엔 절대 포함 안 됨
- [ ] README.md — `CSW_SECRET_KEY` 생성 가이드 (`openssl rand -base64 32`) + 프로덕션 인증 경고
- [ ] `cmd/csw-server/main.go` / `cmd/csw-worker/main.go` 수정 — `stubs.NullGateway` / `stubs.NullChainHead` 제거, 실제 팩토리 주입

### 세부 단계 (구현 순서)

1. **migration 007** (sources 테이블 + 인덱스)
2. **`internal/secrets`** + 단위 테스트 (roundtrip, key size 검증, nonce 유일성)
3. **`internal/infrastructure/persistence/source_repository.go`** + 도메인 `SourceConfig` 타입 + round-trip 테스트
4. **어댑터 팩토리** — `internal/infrastructure/gateway/factory.go`. type string → adapter.Source 매핑. 단위 테스트 (unknown type → err, 각 타입 → New 호출 검증)
5. **`gateway.DBGateway`** — `SourceRepository` + factory 조합. `ForChain` / `Get` 구현. 캐싱 전략: per-request 캐시 (요청 초반 1회 DB read) 또는 TTL 캐시 (post-MVP)
6. **실제 `ChainHead`** — `rpc.Adapter`의 `FetchBlock(finalized)` 경로 재사용
7. **`csw migrate seed`** — 임베디드 YAML 파싱 → env 레퍼런스 해결 → `sources` INSERT
8. **API + DTO + 라우트** — `/sources` CRUD
9. **UI** — `/sources` 페이지 CRUD + `useCreateSource` / `useUpdateSource` / `useDeleteSource` 훅
10. **csw-server 와이어링 갱신** — stubs 제거
11. README + 배포 경고

### 보안 주의

- **API 응답에 절대 복호화된 api_key 포함 금지**. GET /sources 응답은 `has_api_key: bool`만. 편집 시에도 비워두면 유지, 새 값 입력 시 교체
- **로그에 api_key 평문 금지** — observability 레이어 스크러버 추가
- `CSW_SECRET_KEY` 로테이션: post-MVP. MVP는 교체 시 `csw rotate-secrets <old> <new>` 수동 커맨드 문서화
- DB 덤프 공유 시: 암호화된 상태라 안전하지만, 동시에 `CSW_SECRET_KEY`가 유출되면 같이 위험 → 둘을 물리적으로 분리 보관

### 의존

- Phase 6 (persistence) — ✅ 완료
- Phase 7I.2 (TokenPlans round-trip 패턴) — ✅ 완료 (migration + 암호화 없는 JSONB 칼럼 추가 패턴 참고)
- Phase 8 (HTTP API) — ✅ 완료

### Phase 10b 착수 전제

Phase 10a 완료 후 실제 어댑터로 discrepancy가 잡히는 end-to-end 흐름 확인 → Phase 10b (observability/deploy) 착수.

---

## Phase 10b — Observability / Deploy (이하 기존 내용)

## 목표

모든 구성요소를 **하나로 묶고**, 관측성을 확보하고, 실제 돌아가는 데모 환경 완성. MVP 론칭 준비.

## 산출물 (DoD)

- [ ] 통합 `docker-compose.yml` — server + worker + postgres + redis + (optional) asynqmon + web
- [ ] E2E 테스트 스위트 — 실 환경에서 run 생성 → 실행 → diff 확인 자동화
- [ ] 구조화 로깅 표준 (slog + JSON, 요청 ID 일관)
- [ ] Prometheus 메트릭 노출 (`/metrics`)
- [ ] 기본 Grafana 대시보드 JSON (선택)
- [ ] 배포 가이드 (README.md 갱신)
- [ ] 환경별 config 분리 (`.env.local`, `.env.staging`, `.env.production`)
- [ ] 백업·복구 가이드 (Postgres 스냅샷, Redis는 큐라 휘발 허용)
- [ ] 보안 체크리스트 (시크릿 관리, CORS, rate limit, SQL injection 점검)

## 관측성 설계

### 로깅 (slog)
- JSON 핸들러
- 필드 표준: `trace_id`, `run_id`, `source_id`, `metric`, `block`, `severity`
- 레벨: debug/info/warn/error
- Chi middleware에서 trace_id 자동 주입, context로 전파

### 메트릭 (Prometheus)
핵심 카운터·히스토그램:
- `csw_runs_total{chain, status}` — run 처리량
- `csw_run_duration_seconds` — run 실행 시간 histogram
- `csw_source_requests_total{source, metric, tier, outcome}` — 소스 호출 결과 (Tier 라벨 추가)
- `csw_source_request_duration_seconds{source, metric, tier}` — 소스 지연
- `csw_rate_limited_total{source}` — rate limit 히트
- `csw_discrepancies_total{metric, severity, tier}` — 탐지된 불일치 (Tier 분해)
- `csw_asynq_queue_size{queue}` — 큐 적체 (asynq 자체 메트릭과 병행)
- **Tier/Budget 관련 (2026-04-20 추가)**:
  - `csw_run_blocks_total{chain, tier, outcome}` — Tier별 처리 블록 수 (A=전수, B=샘플 갯수)
  - `csw_budget_reserved_total{source}` — `RateLimitBudget.Reserve()` 카운트
  - `csw_budget_refunded_total{source}` — 실패 시 환불 카운트
  - `csw_budget_remaining{source}` — 현재 window 남은 예산 (gauge)
  - `csw_budget_exhausted_total{source}` — budget 고갈 이벤트
  - `csw_anchor_window_discarded_total{metric}` — reflected_block이 window 밖이라 폐기된 샘플 수
  - `csw_sampling_coverage{strategy, stratum}` — 4-stratum별 샘플링 커버리지 (known/top-N/random/recently-active)

### Health checks
- `/healthz` — 프로세스 liveness만
- `/readyz` — DB·Redis 연결 확인

### (선택) Tracing
- OpenTelemetry OTLP 익스포터 연동은 post-MVP
- 인터페이스만 미리 뚫어둠 (`observability.Tracer`)

## 통합 docker-compose

```yaml
# docker-compose.yml (개요)
services:
  postgres:
    image: postgres:17
    environment:
      POSTGRES_USER: csw
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: csw
    volumes: [pgdata:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U csw"]

  redis:
    image: redis:7.4-alpine
    volumes: [redisdata:/data]

  migrate:
    build: .
    command: ["./bin/csw", "migrate", "up"]
    depends_on: { postgres: { condition: service_healthy } }
    environment:
      DATABASE_URL: postgres://csw:${POSTGRES_PASSWORD}@postgres:5432/csw?sslmode=disable

  server:
    build: .
    command: ["./bin/csw-server"]
    depends_on: { postgres: ..., redis: ..., migrate: { condition: service_completed_successfully } }
    ports: ["8080:8080"]
    environment: [...]

  worker:
    build: .
    command: ["./bin/csw-worker"]
    depends_on: { postgres: ..., redis: ... }

  asynqmon:
    image: hibiken/asynqmon
    profiles: [tools]   # 기본 off
    ports: ["8081:8080"]

  web:
    build: ./web
    depends_on: [server]
    ports: ["3000:3000"]
    environment:
      NEXT_PUBLIC_API_BASE_URL: http://localhost:8080

volumes:
  pgdata:
  redisdata:
```

## E2E 시나리오

`e2e/` 디렉토리에 `-tags=e2e` 태그로:

### Scenario 1 — Happy path (3-way 일치)
1. docker-compose up
2. `POST /runs` (manual trigger, FixedList [블록 1개])
3. poll `GET /runs/{id}` until status=completed
4. `GET /runs/{id}/diffs` → 0개 확인 (단, real 네트워크 의존이라 flaky 가능)

### Scenario 2 — 인위적 불일치
1. fake source adapter를 주입할 수 있는 **테스트 전용 빌드** (build tag `e2etest`)
2. 한 소스가 잘못된 값 반환하도록 설정
3. run 실행 → diff 1개 생성 확인

### Scenario 3 — 스케줄 / 취소
1. scheduled trigger 등록 (1분 간격)
2. 1분 대기 후 run이 자동 생성되는지 확인
3. cancel / schedule 삭제

## 배포 고려

### 로컬 개발
- docker-compose 통합 스택 (본 Phase에서 담당)

### 프로덕션
- **K8s 배포는 [Phase 11](./phase-11-kubernetes-deploy.md)에서 별도 관리**.
- Phase 10은 로컬 개발/스모크 테스트 환경 완성이 목표.

## 보안 체크리스트

- [ ] 모든 시크릿 `.env` only, git 금지
- [ ] `.env.example` 최신 상태 유지
- [ ] CORS 화이트리스트 env로
- [ ] HTTP server에 `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` 설정
- [ ] gorm 쿼리 전체 파라미터 바인딩 사용 (raw SQL 지양)
- [ ] 의존성 업데이트 (`dependabot` 또는 `renovate`)
- [ ] `gosec` 린터 통과

## 세부 단계

### 10.1 로깅·메트릭 통일
- [ ] `internal/observability/` 완성
- [ ] 모든 레이어에 request id·run id·source id 로그 필드 전파

### 10.2 Prometheus 메트릭
- [ ] `/metrics` 핸들러
- [ ] 주요 카운터·히스토그램 계측 추가

### 10.3 docker-compose 통합
- [ ] 단일 명령으로 full-stack 기동
- [ ] `make up` / `make down` / `make logs`

### 10.4 E2E 테스트
- [ ] `e2etest` 빌드 태그로 fake source 주입 가능하게
- [ ] 3개 시나리오 자동화

### 10.5 배포 가이드 문서
- [ ] `README.md` Quick Start 완성
- [ ] `docs/runbook.md` (장애 대응 가이드)

### 10.6 보안 점검
- [ ] 체크리스트 항목 전부 확인

## 의존 Phase

- Phase 3 (어댑터)
- Phase 6 (persistence)
- Phase 7 (queue)
- Phase 8 (HTTP API)
- Phase 9 (frontend)

## 완료 기준 (MVP 출시)

- [ ] 로컬 `make up` 한 번으로 전체 스택 기동
- [ ] Optimism 메인넷 실 블록 대상 3-way 비교 성공 (적어도 1 run)
- [ ] 프론트에서 run 생성·diff 확인 UX 완성
- [ ] OpenAPI 스펙·프론트 타입이 자동 동기화
- [ ] CI 녹색, 커버리지 (domain+application) 80%+

## post-MVP 확장 (별도 Phase 또는 후속 릴리스)

- 실시간 streaming 검증 (block 수신 → 즉시 trigger)
- 멀티체인 (Ethereum, Base, Arbitrum)
- 인증·멀티 사용자 / RBAC
- OpenTelemetry tracing
- Slack/Discord/PagerDuty 알림 통합

## 참고

- [slog structured logging](https://pkg.go.dev/log/slog)
- [Prometheus Go client](https://github.com/prometheus/client_golang)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
- [asynqmon](https://github.com/hibiken/asynqmon)
