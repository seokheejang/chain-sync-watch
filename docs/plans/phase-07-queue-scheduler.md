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
