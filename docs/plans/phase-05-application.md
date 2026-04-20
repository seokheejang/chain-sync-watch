# Phase 5 — Application (Use Case)

## 목표

도메인 + 포트만 사용해 **실제 비즈니스 흐름(use case)** 구현. 이 Phase의 코드는 infra를 모르고, 모든 외부 의존은 **포트 인터페이스**로 추상화됨.

TDD 관점에서 **가장 많은 시나리오 테스트가 살게 되는 레이어**.

## 산출물 (DoD)

- [ ] `internal/application/ports.go` — 모든 포트 인터페이스 모음 (application이 의존하는 외부 경계)
- [ ] `internal/application/schedule_run.go` — 스케줄링 use case
- [ ] `internal/application/execute_run.go` — 실행 use case (핵심)
- [ ] `internal/application/query_runs.go` — 조회 use case
- [ ] `internal/application/query_diffs.go` — diff 조회 use case
- [ ] `internal/application/replay_diff.go` — 특정 diff 재검증 use case
- [ ] `internal/application/errors.go` — 애플리케이션 레이어 에러
- [ ] 블랙박스 테스트 — fake 포트로 시나리오 검증

## 포트 정의

```go
// internal/application/ports.go

// --- Filter / ID / DTO 타입 (application 레이어 전용) ---

type RunFilter struct {
    ChainID    *chain.ChainID
    Status     *verification.Status
    CreatedAt  *TimeRange
    Limit      int
    Offset     int
}

type TimeRange struct {
    From time.Time
    To   time.Time
}

type DiffFilter struct {
    RunID      *verification.RunID
    MetricKey  *string
    Severity   *diff.Severity
    Resolved   *bool
    BlockRange *chain.BlockRange
    Limit      int
    Offset     int
}

type DiffID string

// Application 레이어가 persistence 에 노출하는 읽기 모델.
// Discrepancy + Judgement + metadata 합성.
type DiffRecord struct {
    ID             DiffID
    Discrepancy    diff.Discrepancy
    Judgement      diff.Judgement
    Resolved       bool
    ResolvedAt     *time.Time

    // 2026-04-20 확장 — Tier/Anchor 메타 (Phase 2C + Phase 4 Tolerance 대응)
    Tier           source.Tier              // 비교한 Capability의 Tier (A/B/C)
    AnchorBlock    chain.BlockNumber        // Run이 고정한 anchor block
    SamplingSeed   *int64                   // Tier B 샘플링 run 재현용 (Random strategy 등)
}

type JobID string

// 스케줄링 payload — 실행 시 사용할 Run 설정 스냅샷.
type SchedulePayload struct {
    ChainID  chain.ChainID
    Metrics  []verification.Metric
    Strategy verification.SamplingStrategy
    // trigger는 스케줄러가 ScheduledTrigger로 래핑
}

// --- 포트 ---

type RunRepository interface {
    Save(ctx context.Context, r *verification.Run) error
    FindByID(ctx context.Context, id verification.RunID) (*verification.Run, error)
    List(ctx context.Context, f RunFilter) ([]*verification.Run, int /* total */, error)
}

type DiffRepository interface {
    Save(ctx context.Context, d *diff.Discrepancy, j diff.Judgement) (DiffID, error)
    FindByRun(ctx context.Context, runID verification.RunID) ([]DiffRecord, error)
    FindByID(ctx context.Context, id DiffID) (*DiffRecord, error)
    List(ctx context.Context, f DiffFilter) ([]DiffRecord, int /* total */, error)
    MarkResolved(ctx context.Context, id DiffID, at time.Time) error
}

type SourceGateway interface {
    ForChain(chainID chain.ChainID) ([]source.Source, error)  // 해당 체인에 연결된 모든 소스
    Get(sourceID source.SourceID) (source.Source, error)
}

type JobDispatcher interface {
    EnqueueRunExecution(ctx context.Context, runID verification.RunID) error
    ScheduleRecurring(ctx context.Context, schedule verification.Schedule, payload SchedulePayload) (JobID, error)
    CancelScheduled(ctx context.Context, id JobID) error
}

type Clock interface {
    Now() time.Time
}

// tip block 조회용 (샘플링에 필요)
type ChainHead interface {
    Tip(ctx context.Context, chainID chain.ChainID) (chain.BlockNumber, error)
    // 2026-04-20: anchor 고정용. Optimism는 finalized tag 직접 지원.
    Finalized(ctx context.Context, chainID chain.ChainID) (chain.BlockNumber, error)
}

// Tier B rate-limit budget (상세 정의는 Phase 7)
type RateLimitBudget interface {
    Reserve(ctx context.Context, source source.SourceID, n int) error
    Refund(ctx context.Context, source source.SourceID, n int) error
}
```

**DTO vs 도메인 분리 의도**: `RunFilter`, `DiffFilter`, `DiffRecord`, `DiffID`, `JobID`, `SchedulePayload`는 **application 레이어 전용 DTO**. 도메인 패키지 (`verification`, `diff`)에 넣지 않는 이유는 이들이 "use case 경계에서의 입출력 형태"라서 도메인 모델이 아님. persistence (Phase 6) / queue (Phase 7) / httpapi (Phase 8)가 이 타입을 공유.

## Use Case 설계

### `ScheduleRun`
**입력**: `ChainID`, `SamplingStrategy`, `Metrics`, `Trigger`
**동작**:
1. `Run` 값 생성 (검증 포함)
2. `RunRepository.Save` (status=pending)
3. `Trigger` 분기:
   - Manual → 즉시 `JobDispatcher.EnqueueRunExecution`
   - Scheduled → `JobDispatcher.ScheduleRecurring`
   - Realtime → (post-MVP) 구독 설정

### `ExecuteRun` (핵심)
**입력**: `RunID`
**동작**:
1. `RunRepository.FindByID`
2. `Run.Start()` → `RunRepository.Save` (status=running)
3. **Anchor 고정**: `ChainHead.Finalized()` 호출로 finalized block 획득 → Run의 모든 조회가 이 anchor 기준 ([phase-04](./phase-04-verification-diff-domain.md) `CompareContext.AnchorBlock`)
4. **Tier별 분기** (2026-04-20 확장):
   - Tier A Capability → **전수 모드**: `SamplingStrategy.Blocks()` 그대로. RPC 중심 조회, budget 체크 스킵
   - Tier B Capability → **샘플링 모드**: 4-stratum (known / top-N / random / recently-active) 주소 세트 + anchor 시점 latest 호출. `RateLimitBudget.Reserve()` 필수, `ErrBudgetExhausted` 시 config policy(`skip`/`defer`/`fail`)에 따라 분기
   - Tier C Capability → Metric의 policy 설정 따라 A 또는 B처럼 동작
5. 각 (블록|주소) × Metric × Source 병렬 조회
   - `Source.Supports(cap)=false` → skip
   - `ErrUnsupportedAtBlock` → 해당 (source, metric) 조합 skip + 로그
   - rate limit / unavailable → backoff 재시도, 한도 초과 시 skip + `RateLimitBudget.Refund()`
6. 응답 수집 후 `ValueSnapshot`으로 정규화 (ReflectedBlock 메타 포함)
7. 비교: Metric의 `Tolerance.Judge(a, b, metric, ctx)` 호출
   - `needDiscard=true` → 샘플 폐기, diff 저장 안 함
   - `ok=false` → `Discrepancy` 생성 → `diff.JudgementPolicy.Judge` → `DiffRepository.Save` (Tier/AnchorBlock/SamplingSeed 포함)
8. `Run.Complete()` 또는 `Run.Fail(err)` → `RunRepository.Save`

**병렬성**: per-block × per-metric × per-source → `errgroup` 기반, 동시성 상한 (rate limit과 별개)

**샘플링 4-stratum 세부** (Tier B 전용):
- `known`: config 주입 (bridge, DEX, treasury 주소)
- `top-N`: Tier B 자체 질의로 획득 (예: Blockscout `/addresses?sort=balance`)
- `random`: seed 고정 — `Random{Seed: X}`와 동일 방식, 재현성
- `recently-active`: **RPC**의 `eth_getBlockByNumber` + tx `from`/`to` 추출 (indexer 결과 의존 금지 — 편향 방지)
- 수집한 union set에 대해 샘플링 실행. seed는 `DiffRecord.SamplingSeed`에 기록.

### `QueryRuns` / `QueryDiffs` / `ReplayDiff`
- 조회는 단순 위임
- `ReplayDiff`: 특정 `DiffID` 읽어서 그 블록·metric에 대해 다시 `ExecuteRun`과 같은 비교 1건 수행 → 결과가 여전히 불일치면 새 Discrepancy 저장, 일치하면 "resolved"로 기존 diff 업데이트

## 테스트 전략 (TDD)

### 5.1 Fake 포트 준비
- [ ] `application/internal/testsupport/fake_run_repo.go` (inmem)
- [ ] `application/internal/testsupport/fake_diff_repo.go`
- [ ] `application/internal/testsupport/fake_source_gateway.go` (Phase 2 `source/fake` 활용)
- [ ] `application/internal/testsupport/fake_dispatcher.go`
- [ ] `application/internal/testsupport/fake_chain_head.go`
- [ ] `application/internal/testsupport/fake_clock.go`

### 5.2 `ScheduleRun` 테스트
- [ ] Manual trigger → 즉시 enqueue 호출됨
- [ ] Scheduled trigger → ScheduleRecurring 호출됨
- [ ] 검증 실패 (metrics 비어있음 등) → 저장/enqueue 호출 안 됨
- [ ] 중복 RunID → 에러

### 5.3 `ExecuteRun` 테스트 (가장 많은 시나리오)
- [ ] 3소스 모두 일치 → diff 0건, run=completed
- [ ] 1개 소스 불일치 → diff 1건 생성, judgement 정확
- [ ] 소스가 Capability 미지원 → 해당 조합 skip, run 여전히 완료
- [ ] 소스 rate limit 에러 → 재시도 후 성공
- [ ] 소스 영구 실패 → 해당 조합 skip + 경고 로그, run=completed (일부 실패)
- [ ] 모든 소스 실패 → run=failed
- [ ] Cancelled 상태로 변경되면 중단
- [ ] 샘플링 Blocks가 빈 리스트 → 즉시 completed, diff 0건
- [ ] **Tier 분기**: Run에 Tier A Capability만 있으면 budget 체크 없음, Tier B 있으면 `Reserve` 호출
- [ ] **Anchor window discard**: Tier B 샘플 중 `ReflectedBlock`이 window 밖 → discrepancy 저장 안 됨
- [ ] **`ErrUnsupportedAtBlock`**: anchor numeric인데 소스 미지원 → 해당 (source, metric) skip
- [ ] **`ErrBudgetExhausted`**: config `budget.exhausted_policy=skip` 시 남은 metric skip, `=fail` 시 run 실패
- [ ] **SamplingSeed 재현성**: 같은 seed로 ReplayDiff → 같은 샘플 세트 생성 확인

### 5.4 `QueryRuns` / `QueryDiffs` / `ReplayDiff` 테스트
- [ ] 필터링, 페이징
- [ ] ReplayDiff: 기존 diff 상태 resolved로 바뀌는 시나리오

## 의존 Phase

- Phase 1 (chain)
- Phase 2 (source 포트)
- Phase 4 (verification + diff 도메인)

## 주의

- **트랜잭션 경계**: Application은 트랜잭션을 **경계만 표시** (UoW 패턴 간소화 버전). 실제 트랜잭션은 infra 레이어에서.
  - 초기엔 생략하고, Phase 6에서 repository가 알아서 단일 트랜잭션으로 처리
- **동시성 한도**: config로 주입 (`max_concurrent_requests_per_source`). 기본값 5.
- **에러 분류**: transient(재시도) vs permanent(skip) vs fatal(run 전체 실패) — `errors.Is/As` 패턴으로 구분
- **재시도 vs source adapter의 재시도**: 중복 방지. **재시도는 source adapter에만** 두고, application은 "한 번 호출하고 결과 수용" 하자.

## 참고

- [Clean Architecture use cases](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)
- [errgroup](https://pkg.go.dev/golang.org/x/sync/errgroup)
