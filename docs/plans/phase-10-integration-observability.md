# Phase 10 — Integration / Observability / Deploy

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
- `csw_source_requests_total{source, metric, outcome}` — 소스 호출 결과
- `csw_source_request_duration_seconds{source, metric}` — 소스 지연
- `csw_rate_limited_total{source}` — rate limit 히트
- `csw_discrepancies_total{metric, severity}` — 탐지된 불일치
- `csw_asynq_queue_size{queue}` — 큐 적체 (asynq 자체 메트릭과 병행)

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
