# Phase 7 — Queue / Scheduler (Redis + asynq)

## 목표

`JobDispatcher` 포트의 **asynq 기반 구현체**와 worker 프로세스. 스케줄링(cron), 재시도·backoff, rate limiter, 관측 대시보드(asynqmon).

## 산출물 (DoD)

- [ ] `internal/infrastructure/queue/dispatcher.go` — `JobDispatcher` 구현체
- [ ] `internal/infrastructure/queue/handlers.go` — asynq 태스크 핸들러 (ExecuteRun 호출)
- [ ] `internal/infrastructure/queue/scheduler.go` — cron 스케줄러 (asynq `PeriodicTaskManager`)
- [ ] `internal/infrastructure/queue/tasks.go` — 태스크 타입 상수, payload 정의
- [ ] `cmd/csw-worker/main.go` — worker 프로세스 (Go 관례: 디렉토리명 = 바이너리명 = `csw-worker`)
- [ ] **worker health endpoint** — 별도 경량 HTTP 서버로 `/healthz` (liveness, 프로세스 살아있음) / `/readyz` (asynq connection + Redis ping) 노출. Phase 11 K8s probe에서 요구됨.
- [ ] `docker-compose.override.yml`에 asynqmon 선택적으로 추가
- [ ] 통합 테스트 (miniredis 또는 testcontainers로 redis)
- [ ] 재시도·backoff·rate limit 정책 문서화

## 설계

### 태스크 타입

```go
// internal/infrastructure/queue/tasks.go
const (
    TaskTypeExecuteRun = "verification:execute_run"
    TaskTypeScheduledRun = "verification:scheduled_run"
    // 나중에 확장:
    // TaskTypeRealtimeBlock = "verification:realtime_block"
)

type ExecuteRunPayload struct {
    RunID string `json:"run_id"`
}

type ScheduledRunPayload struct {
    ChainID       uint64 `json:"chain_id"`
    MetricsJSON   string `json:"metrics"`
    StrategyKind  string `json:"strategy_kind"`
    StrategyJSON  string `json:"strategy"`
}
```

### Dispatcher

```go
// internal/infrastructure/queue/dispatcher.go
type Dispatcher struct {
    client *asynq.Client
    inspector *asynq.Inspector
}

func (d *Dispatcher) EnqueueRunExecution(ctx context.Context, runID verification.RunID) error {
    payload, _ := json.Marshal(ExecuteRunPayload{RunID: string(runID)})
    task := asynq.NewTask(TaskTypeExecuteRun, payload,
        asynq.MaxRetry(3),
        asynq.Timeout(30*time.Minute),
        asynq.Retention(7*24*time.Hour),
    )
    _, err := d.client.EnqueueContext(ctx, task)
    return err
}
```

### Worker (handler)

```go
// internal/infrastructure/queue/handlers.go
type Handlers struct {
    exec *application.ExecuteRun  // use case
    log  *slog.Logger
}

func (h *Handlers) HandleExecuteRun(ctx context.Context, t *asynq.Task) error {
    var p ExecuteRunPayload
    if err := json.Unmarshal(t.Payload(), &p); err != nil {
        return fmt.Errorf("unmarshal: %w", asynq.SkipRetry)
    }
    return h.exec.Execute(ctx, verification.RunID(p.RunID))
}
```

### Scheduler (cron)

```go
// internal/infrastructure/queue/scheduler.go
// asynq.PeriodicTaskManager로 cron 표현식 → 주기적 태스크 등록
```

### Rate Limiter

- asynq `Queue` 개념으로 소스별 큐 분리 고려:
  - 큐 이름: `etherscan`, `rpc`, `internal` 등
  - 소스별 동시성 제한을 `asynq.Queues` 가중치로
- 또는 source adapter 자체 rate limiter (Phase 3) 와 결합 — **adapter 측 rate limiter가 1차, asynq는 2차**

### worker 프로세스 구조

```go
// cmd/csw-worker/main.go
func main() {
    cfg := config.Load()
    log := observability.NewLogger()

    redisOpt, _ := asynq.ParseRedisURI(cfg.Redis.URL)  // REDIS_URL env 파싱
    srv := asynq.NewServer(
        redisOpt,
        asynq.Config{
            Concurrency: cfg.Worker.Concurrency,
            Queues: map[string]int{
                // 번들 어댑터별 큐 분리로 rate limit·isolation 확보
                "default":    5,
                "rpc":        5,
                "blockscout": 3,
                "etherscan":  3,
                // 사용자 정의 어댑터 큐는 config로 추가 가능
            },
            RetryDelayFunc: retryDelay,
        },
    )

    mux := asynq.NewServeMux()
    mux.HandleFunc(TaskTypeExecuteRun, handlers.HandleExecuteRun)

    // graceful shutdown + worker health server (별도 goroutine)
    go runHealthServer(cfg.Worker.HealthAddr, redisOpt)
    srv.Run(mux)
}
```

## 세부 단계 (TDD)

### 7.1 태스크 타입·payload 정의
- [ ] JSON round-trip 테스트
- [ ] 구현

### 7.2 Dispatcher
- [ ] miniredis 기반 테스트
- [ ] `EnqueueRunExecution`, `ScheduleRecurring`, `CancelScheduled`
- [ ] 구현

### 7.3 Handlers
- [ ] fake ExecuteRun use case로 테스트 (payload unmarshal 실패, 성공, use case 에러 전파)
- [ ] `asynq.SkipRetry` 적절한 경우에만 반환
- [ ] 구현

### 7.4 Scheduler (PeriodicTaskManager)
- [ ] cron 표현식 검증, 주기적 enqueue 동작
- [ ] 구현

### 7.5 worker 프로세스
- [ ] 그레이스풀 셧다운 (SIGTERM 대응 — asynq `server.Shutdown()` + health server 정리)
- [ ] 경량 health HTTP 서버 (별도 포트, 기본 `:8081`, config로 override 가능)
  - `/healthz` — 프로세스 liveness
  - `/readyz` — Redis ping + asynq client 상태
- [ ] 구현

### 7.6 관측
- [ ] asynqmon docker-compose 추가 (개발용, 기본 off)
- [ ] 핸들러 metrics (처리 시간, 성공/실패 카운트)

## 의존 Phase

- Phase 5 (ExecuteRun use case)

## 주의

- **재시도 정책**: asynq의 `MaxRetry` + application 레이어 에러 분류 조합.
  - Transient (rate limit, 네트워크) → 재시도
  - Permanent (도메인 검증 실패, 설정 오류) → `asynq.SkipRetry`
- **중복 enqueue 방지**: 같은 RunID를 중복 enqueue하면? → asynq `TaskID` 옵션으로 idempotency key 설정
- **Dead letter**: asynq는 MaxRetry 초과 시 `archived` 상태로 보관 → asynqmon에서 재실행 가능
- **타임존**: scheduled trigger의 cron 표현식 해석 타임존 명시 (`UTC` 기본)
- **Payload 크기**: asynq는 Redis 저장이라 페이로드 작게. 큰 데이터는 RunID만 넘기고 DB에서 로드.

## 참고

- [asynq 공식 문서](https://github.com/hibiken/asynq)
- [asynqmon 대시보드](https://github.com/hibiken/asynqmon)
- [asynq PeriodicTaskManager](https://github.com/hibiken/asynq/wiki/Periodic-Tasks)
- [miniredis (테스트용)](https://github.com/alicebob/miniredis)

---

## 2026-04-20 추가 — Tier-aware 스케줄링 + Rate-limit Budget Port

[docs/research/external-api-coverage.md](../research/external-api-coverage.md) "검증 Tier 분류" 및 "추가 합의 (2026-04-20)" 반영. Phase 7은 **Tier별 실행 정책**을 담당한다.

### 실행 정책

| Tier | 대상 Capability | 실행 모드 | 빈도 | 예산 제약 |
|---|---|---|---|---|
| A | RPC-canonical (block/tx/receipt/balance at block 등) | **전수** — finalized 블록 전부 | 매 finalized 배치 | 없음 (자체 노드) |
| B | Indexer-derived (holdings, stats 등) | **샘플링** — 4-stratum | 예산 허용만큼 | rate-limit budget 상한 |
| C | Mixed | 지표별 설정 (기본 Tier B처럼, 주기적 cross-check로 RPC 재구성) | 지표별 cron | 선택적 |

샘플링 4-stratum (verification use case가 address 세트를 구성):
- **known**: config에서 주입된 유명 주소 (bridge, DEX, treasury)
- **top-N**: balance·tx count 상위 (Tier B 자체 질의로 획득)
- **random**: 균등 샘플링 (verification seed로 재현 가능)
- **recently-active**: 최근 N 블록에서 tx 등장 주소 (**RPC 블록에서 추출** — indexer 결과 의존 금지, 편향 방지)

### Rate-limit Budget Port

Tier B 전용. asynq queue 분리로는 부족 (같은 소스에 대한 call 총량 제어 필요).

```go
// internal/application/ports/budget.go
type RateLimitBudget interface {
    // 소스별 남은 예산 질의 (calls/window)
    Remaining(ctx context.Context, source source.SourceID) (RemainingBudget, error)
    // 호출 의도 예약 (실제 호출 전에 차감). 부족하면 ErrBudgetExhausted.
    Reserve(ctx context.Context, source source.SourceID, n int) error
    // 실패 시 환불 (네트워크 에러 등 소스가 실제로 카운트 안 한 케이스)
    Refund(ctx context.Context, source source.SourceID, n int) error
}

type RemainingBudget struct {
    Source      source.SourceID
    Remaining   int
    WindowReset time.Time
    WindowLimit int
}

var ErrBudgetExhausted = errors.New("rate-limit budget exhausted")
```

**구현체**: `internal/infrastructure/queue/budget.go` — Redis INCR + TTL 기반 sliding / fixed window counter (기본 fixed window: Routescan 5 req/s & 100k/day = 2 window).

**정책**:
- Tier A 태스크는 budget 체크 스킵 (자체 노드)
- Tier B 태스크 핸들러 시작 시 `Reserve()` — 실패하면 `asynq.SkipRetry` 대신 backoff 재시도
- adapter 레벨 rate limiter(Phase 3 httpx)는 **단기 burst 방어**, budget port는 **장기 창 예산 추적** — 역할 분리

### Tier 기반 Queue 이름 재설계

```go
Queues: map[string]int{
    "default":      5,
    "tier-a-rpc":   10,   // 전수, 높은 처리량
    "tier-b-3rd":    3,   // 샘플링, 낮은 처리량 + budget 제어
    "tier-c-mixed": 2,
},
```

소스별 세분(`tier-b-blockscout`, `tier-b-routescan`)은 실제 부하 관찰 후 결정.

### 신규 DoD 항목

- [ ] `internal/application/ports/budget.go` — `RateLimitBudget` port
- [ ] `internal/infrastructure/queue/budget.go` — Redis 기반 구현 + miniredis 테스트
- [ ] Tier B 핸들러 경로에 budget reserve/refund 통합
- [ ] tier별 queue 가중치 config 노출

### 주의

- **샘플 세트 재현성**: random seed·timestamp·blockNumber 조합을 `Run`에 기록 → 사후 재현 가능. Phase 4/5 `verification.Run`에 필드 추가 필요.
- **예산 초과 시 동작**: fail fast가 아니라 "다음 window까지 대기 후 재시도" — 사용자가 설정 가능 (config `budget.exhausted_policy`: `skip` | `defer` | `fail`).
- **Tier A와 B 혼합 Run**: 한 Run이 양쪽 모두 포함 가능. 이 경우 A 부분은 전수, B 부분은 샘플링으로 분기 실행 + 결과 합류. use case 레벨 분기(Phase 5).
