# Phase 8 — HTTP API (chi + huma)

## 목표

프론트가 소비할 REST API + **OpenAPI 3.1 스펙 자동 생성·서빙**. huma로 타입 안전 핸들러, chi로 라우팅·미들웨어.

## 산출물 (DoD)

- [ ] `internal/infrastructure/httpapi/server.go` — 서버 구성, chi + huma 와이어링
- [ ] `internal/infrastructure/httpapi/routes/` — 도메인별 라우트 핸들러
  - `runs.go` — `POST /runs`, `GET /runs`, `GET /runs/{id}`, `POST /runs/{id}/cancel`
  - `diffs.go` — `GET /diffs`, `GET /diffs/{id}`, `POST /diffs/{id}/replay`
  - `schedules.go` — `POST /schedules`, `GET /schedules`, `DELETE /schedules/{id}`
  - `sources.go` — `GET /sources` (사용 가능한 소스 + Capability 목록)
  - `health.go` — `GET /healthz`, `GET /readyz`
- [ ] `internal/infrastructure/httpapi/dto/` — HTTP 입출력 DTO (도메인과 분리)
- [ ] `internal/infrastructure/httpapi/middleware/` — request id, logging, recovery, CORS
- [ ] `internal/infrastructure/httpapi/errors.go` — 에러 → HTTP 매핑
- [ ] `cmd/csw-server/main.go` — 메인 엔트리 (use case DI 조립 + 라우트 등록)
- [ ] `/docs` 경로에 OpenAPI UI (stoplight elements 기본)
- [ ] `/openapi.json`, `/openapi.yaml` 자동 서빙
- [ ] 핸들러 단위 테스트 (httptest + fake use case)
- [ ] OpenAPI 스펙 덤프 테스트 (빌드 시 `openapi.json` 최신화 검증)

## 설계

### 디렉토리

```
internal/infrastructure/httpapi/
├── server.go
├── routes/
│   ├── runs.go
│   ├── diffs.go
│   ├── schedules.go
│   ├── sources.go
│   └── health.go
├── dto/
│   ├── run.go
│   ├── diff.go
│   └── schedule.go
├── middleware/
│   ├── requestid.go
│   ├── logging.go
│   ├── recovery.go
│   └── cors.go
└── errors.go
```

### 서버 구성

```go
// internal/infrastructure/httpapi/server.go
func NewServer(cfg Config, svcs Services) *http.Server {
    r := chi.NewRouter()

    // 미들웨어
    r.Use(middleware.RequestID)
    r.Use(middleware.Logger(cfg.Logger))
    r.Use(middleware.Recoverer(cfg.Logger))
    r.Use(cors.Handler(cors.Options{
        AllowedOrigins: cfg.CORS.AllowedOrigins,
        AllowedMethods: []string{"GET", "POST", "DELETE"},
    }))

    // huma API
    humaCfg := huma.DefaultConfig("chain-sync-watch", "v1")
    humaCfg.OpenAPI.Info.Description = "Chain indexer cross-source verification API"
    api := humachi.New(r, humaCfg)

    // 라우트 등록 — 각 route가 필요한 use case struct만 받음
    routes.RegisterRuns(api, svcs.Runs)       // RunDeps
    routes.RegisterDiffs(api, svcs.Diffs)     // DiffDeps
    routes.RegisterSchedules(api, svcs.Schedules)
    routes.RegisterSources(api, svcs.Sources)
    routes.RegisterHealth(api, svcs.Health)
    // svcs는 cmd/csw-server/main.go에서 use case 인스턴스 주입해 구성

    return &http.Server{
        Addr:              cfg.Addr,
        Handler:           r,
        ReadHeaderTimeout: 5 * time.Second,
    }
}
```

### 핸들러 예시 — `POST /runs`

Phase 5는 use case를 각자 struct로 분리 (`application.ScheduleRun`, `application.ExecuteRun`, `application.QueryRuns`, `application.QueryDiffs`, `application.ReplayDiff`). `routes.Register*`는 **필요한 use case struct만** 받음 (facade 없음 → DI 명시적).

```go
// routes/runs.go
type RunDeps struct {
    Schedule *application.ScheduleRun
    Query    *application.QueryRuns
    Cancel   *application.CancelRun
}

type CreateRunInput struct {
    Body struct {
        ChainID  uint64              `json:"chain_id" required:"true" doc:"Chain ID, e.g. 10 for Optimism"`
        Metrics  []string            `json:"metrics" required:"true" minItems:"1"`
        Sampling dto.SamplingInput   `json:"sampling" required:"true"`
        Trigger  dto.TriggerInput    `json:"trigger" required:"true"`
    }
}

type CreateRunOutput struct {
    Body struct {
        RunID string `json:"run_id"`
    }
    Status int
}

func RegisterRuns(api huma.API, d RunDeps) {
    huma.Register(api, huma.Operation{
        OperationID: "create-run",
        Method:      http.MethodPost,
        Path:        "/runs",
        Summary:     "Schedule a new verification run",
        Tags:        []string{"runs"},
    }, func(ctx context.Context, in *CreateRunInput) (*CreateRunOutput, error) {
        runID, err := d.Schedule.Handle(ctx, mapToScheduleInput(in))
        if err != nil {
            return nil, mapError(err)
        }
        return &CreateRunOutput{
            Status: http.StatusCreated,
            Body:   struct{ RunID string `json:"run_id"` }{RunID: string(runID)},
        }, nil
    })
}
```

### 에러 매핑

```go
// errors.go
func mapError(err error) error {
    switch {
    case errors.Is(err, application.ErrNotFound):
        return huma.Error404NotFound(err.Error())
    case errors.Is(err, application.ErrInvalidInput):
        return huma.Error400BadRequest(err.Error())
    case errors.Is(err, application.ErrConflict):
        return huma.Error409Conflict(err.Error())
    default:
        return huma.Error500InternalServerError("internal error", err)
    }
}
```

### DTO 분리

- HTTP DTO ↔ 도메인 모델 mapper 함수
- DTO에만 `json` 태그·`required` 스키마 힌트
- 도메인은 불변, DTO는 자유롭게 변형 가능

## 주요 엔드포인트

| 메서드 | 경로 | 설명 |
|---|---|---|
| `POST` | `/runs` | 새 검증 run 생성·스케줄 |
| `GET` | `/runs` | run 목록 (필터: chain, status, page) |
| `GET` | `/runs/{id}` | run 상세 |
| `POST` | `/runs/{id}/cancel` | run 취소 |
| `GET` | `/runs/{id}/diffs` | 해당 run의 diff 목록 |
| `GET` | `/diffs` | 전체 diff (필터: severity, resolved, metric, block range) |
| `GET` | `/diffs/{id}` | diff 상세 (values 원본 포함) |
| `POST` | `/diffs/{id}/replay` | 재검증 trigger |
| `POST` | `/schedules` | 반복 스케줄 등록 |
| `GET` | `/schedules` | 스케줄 목록 |
| `DELETE` | `/schedules/{id}` | 스케줄 해제 |
| `GET` | `/sources` | 사용 가능 소스 + Capability 매트릭스 |
| `GET` | `/healthz` | liveness |
| `GET` | `/readyz` | readiness (DB, Redis 확인) |

## 세부 단계

### 8.1 서버 골격 + 미들웨어
- [ ] 테스트: request id 전파, recovery가 500 반환, CORS preflight
- [ ] 구현

### 8.2 `/runs` 라우트
- [ ] CreateRun / Get / List / Cancel 각각 httptest 기반 테스트 (fake use case)
- [ ] 검증 실패 → 400, 없음 → 404, 충돌 → 409
- [ ] 구현

### 8.3 `/diffs` 라우트
- [ ] 동일한 패턴
- [ ] 구현

### 8.4 `/schedules` 라우트
- [ ] 동일
- [ ] 구현

### 8.5 `/sources` 라우트
- [ ] 현재 연결된 소스 목록 + Capability 노출
- [ ] 구현

### 8.6 `/healthz` / `/readyz`
- [ ] DB/Redis ping 체크
- [ ] 구현

### 8.7 OpenAPI 덤프
- [ ] 빌드 타임에 `openapi.json` 뽑는 기능 — `csw openapi-dump` 서브커맨드로 `cmd/csw/` CLI에 통합 (별도 바이너리 만들지 않음, Phase 0 네이밍 규약)
- [ ] `make openapi` → `go run ./cmd/csw openapi-dump > web/openapi.json` → 프론트가 git-committed spec에서 타입 생성
- [ ] 테스트: 스펙이 최신 코드와 일치하는지 (drift 감지)

### 8.8 Docs UI
- [ ] `/docs` — huma 기본 stoplight elements 확인
- [ ] 루트 `/` → `/docs`로 redirect (선택)

## 의존 Phase

- Phase 5 (application use case)
- Phase 6 (repository — 직접이 아니라 use case를 통해)

## 주의

- **huma 버전**: v2 사용 (제네릭 지원)
- **router 선택**: `humachi.New` (chi adapter)
- **큰 응답 페이징**: `limit`, `cursor`/`offset` 명확히. cursor 기반이 대규모에 더 맞지만 MVP는 offset/limit으로 시작
- **인증**: MVP는 생략 or 간단한 API 키 헤더만. 추후 JWT/OIDC 추가 가능하게 middleware 추상화
- **CORS**: Next.js dev server (localhost:3000)를 AllowedOrigins에 포함 (env로)
- **Graceful shutdown**: `http.Server.Shutdown(ctx)` + worker와 공통 signal handling

## 참고

- [huma v2 공식 문서](https://huma.rocks/)
- [chi router](https://github.com/go-chi/chi)
- [humachi adapter](https://pkg.go.dev/github.com/danielgtaylor/huma/v2/adapters/humachi)
- [OpenAPI 3.1 spec](https://spec.openapis.org/oas/latest.html)
