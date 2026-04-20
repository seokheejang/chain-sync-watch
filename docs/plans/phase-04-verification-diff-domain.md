# Phase 4 — `verification/` + `diff/` 도메인

## 목표

**순수 도메인 레이어** 구현. 외부 의존 0. 샘플링 알고리즘과 diff 판정 로직을 테스트로 단단히 고정.

## 산출물 (DoD)

### `verification/`
- [ ] `run.go` — `Run` 엔티티, 상태 전이
- [ ] `metric.go` — `Metric` (비교할 지표 단위)
- [ ] `sampling.go` — `SamplingStrategy` 인터페이스 + 4 구현체
- [ ] `schedule.go` — `Schedule` (cron or interval)
- [ ] `trigger.go` — `Trigger` (manual / scheduled / realtime) — sealed type 패턴
- [ ] 블랙박스 테스트

### `diff/`
- [ ] `discrepancy.go` — `Discrepancy` (어느 소스와 어느 소스 간 불일치)
- [ ] `tolerance.go` — `Tolerance` 정책 (지표별)
- [ ] `judgement.go` — `Judgement` (심각도 + 신뢰도 평가)
- [ ] `severity.go` — `Severity` enum (Info / Warning / Error / Critical)
- [ ] 블랙박스 테스트

## `verification/` 설계

### `Run`
```go
type RunID string

type Status string
const (
    StatusPending   Status = "pending"
    StatusRunning   Status = "running"
    StatusCompleted Status = "completed"
    StatusFailed    Status = "failed"
    StatusCancelled Status = "cancelled"
)

type Run struct {
    id         RunID
    chainID    chain.ChainID
    strategy   SamplingStrategy
    metrics    []Metric
    trigger    Trigger
    status     Status
    createdAt  time.Time
    startedAt  *time.Time
    finishedAt *time.Time
    errorMsg   string
}

// 생성자
func NewRun(cid chain.ChainID, s SamplingStrategy, m []Metric, t Trigger) (*Run, error)
// 상태 전이 (불법 전이는 에러)
func (r *Run) Start() error
func (r *Run) Complete() error
func (r *Run) Fail(err error)
func (r *Run) Cancel() error
```

### `Metric` — 카테고리 기반 재설계

현장 조사 결과 소스별로 "어느 시점에 앵커되는가"가 상이해 Metric을 **카테고리**로 분류. 카테고리는 비교 전략·신뢰도·판정 기준에 영향.

```go
type MetricCategory string

const (
    // 블록 번호로 앵커, 온체인 불변 (가장 신뢰도 높은 비교)
    CatBlockImmutable MetricCategory = "block_immutable"

    // 현재 시점 address 상태 ("latest" 기준, 세 소스가 같은 "latest"를 본다는 전제)
    CatAddressLatest  MetricCategory = "address_latest"

    // 특정 과거 블록 시점 address 상태 (RPC archive 기반, indexer 대개 미지원)
    CatAddressAtBlock MetricCategory = "address_at_block"

    // 체인 총량/누적값 (시점 무의존 — 소스별 의미가 다를 수 있음, 비교는 참고용)
    CatSnapshot       MetricCategory = "snapshot"
)

type Metric struct {
    Key        string  // 안정 식별자
    Category   MetricCategory
    Capability source.Capability  // Phase 2 필드 단위 Capability 참조
}

// 기본 Metric 카탈로그 (확장 가능)
var (
    // BlockImmutable
    MetricBlockHash            = Metric{Key: "block.hash",             Category: CatBlockImmutable, Capability: source.CapBlockHash}
    MetricBlockParentHash      = Metric{Key: "block.parent_hash",      Category: CatBlockImmutable, Capability: source.CapBlockParentHash}
    MetricBlockTimestamp       = Metric{Key: "block.timestamp",        Category: CatBlockImmutable, Capability: source.CapBlockTimestamp}
    MetricBlockTxCount         = Metric{Key: "block.tx_count",         Category: CatBlockImmutable, Capability: source.CapBlockTxCount}
    MetricBlockGasUsed         = Metric{Key: "block.gas_used",         Category: CatBlockImmutable, Capability: source.CapBlockGasUsed}
    MetricBlockStateRoot       = Metric{Key: "block.state_root",       Category: CatBlockImmutable, Capability: source.CapBlockStateRoot}
    MetricBlockReceiptsRoot    = Metric{Key: "block.receipts_root",    Category: CatBlockImmutable, Capability: source.CapBlockReceiptsRoot}
    MetricBlockTransactionsRoot= Metric{Key: "block.transactions_root",Category: CatBlockImmutable, Capability: source.CapBlockTransactionsRoot}

    // AddressLatest
    MetricBalanceLatest = Metric{Key: "address.balance_latest", Category: CatAddressLatest, Capability: source.CapBalanceAtLatest}
    MetricNonceLatest   = Metric{Key: "address.nonce_latest",   Category: CatAddressLatest, Capability: source.CapNonceAtLatest}

    // AddressAtBlock
    MetricBalanceAtBlock = Metric{Key: "address.balance_at_block", Category: CatAddressAtBlock, Capability: source.CapBalanceAtBlock}
    MetricNonceAtBlock   = Metric{Key: "address.nonce_at_block",   Category: CatAddressAtBlock, Capability: source.CapNonceAtBlock}

    // Snapshot
    MetricTotalAddressCount = Metric{Key: "snapshot.total_addresses",      Category: CatSnapshot, Capability: source.CapTotalAddressCount}
    MetricTotalTxCount      = Metric{Key: "snapshot.total_txs",            Category: CatSnapshot, Capability: source.CapTotalTxCount}
    MetricERC20TokenCount   = Metric{Key: "snapshot.erc20_token_count",    Category: CatSnapshot, Capability: source.CapERC20TokenCount}
    MetricTotalContractCount= Metric{Key: "snapshot.total_contracts",      Category: CatSnapshot, Capability: source.CapTotalContractCount}
)
```

### 카테고리별 비교 정책

| 카테고리 | 주요 Tier | 기본 Tolerance | 판정 정책 |
|---|---|---|---|
| `BlockImmutable` | A | `ExactMatch` (strict) | 1개라도 다르면 Critical, RPC를 trusted로 |
| `AddressLatest` | A (+ C 확장) | `AnchorWindowed(ExactMatch, tol_fwd=64)` | 다르면 Warning. anchor window 밖이면 샘플 discard |
| `AddressAtBlock` | A | `ExactMatch` | RPC 기반이라 Critical, RPC가 trusted |
| `Snapshot` | B (일부 C) | `Observational` or `AnchorWindowed` | reflected-block 메타 있으면 cross-check, 없으면 관찰만 |

**이유**: 현장 관찰(내부 indexer vs Blockscout)에서 `totalAddressCount`가 21% 격차. 의미 정의 차이로 자동 Error 판정은 노이즈. Snapshot 중 reflected-block 메타가 있는 지표(Blockscout address coin_balance 등)는 cross-check 가능하지만, 없는 지표(stats.total_*)는 대시보드 관찰만.

### MetricCategory ↔ Tier 매핑 (2C 연계)

[docs/research/external-api-coverage.md](../research/external-api-coverage.md) "검증 Tier 분류" + [phase-02-source-ports.md](./phase-02-source-ports.md) Phase 2C 참조.

| MetricCategory | 대응 Tier | 전수/샘플링 | budget 필요 |
|---|---|---|---|
| `CatBlockImmutable` | A | 전수 (모든 finalized 블록) | ❌ 자체 RPC |
| `CatAddressAtBlock` | A | 전수 또는 샘플링 (addr set × blocks) | ❌ 자체 RPC |
| `CatAddressLatest` | A (기본) · C (ERC-20 holdings 등) | 샘플링 | Tier C는 3rd-party 경로 시 ✅ |
| `CatSnapshot` | B (대부분) · C (일부) | 샘플링 | ✅ |

Category는 "**무엇을 비교하나**", Tier는 "**어떻게 조달하나**". 둘은 orthogonal. `Metric`에 Tier 필드를 두지 않고 `source.Capability.Tier()`로 조회 (2C에서 정의됨) — Metric이 하나의 Capability를 참조하기 때문에 Tier도 거기서 파생.

### `SamplingStrategy`
```go
type SamplingStrategy interface {
    Kind() string
    Blocks(ctx Context) []chain.BlockNumber  // Context: tip block 등 조회 컨텍스트
}

type Context struct {
    TipBlock chain.BlockNumber
    Now      time.Time
}

// 구현체 4종
type FixedList    struct{ Numbers []chain.BlockNumber }
type LatestN      struct{ N uint }
type Random       struct{ Range chain.BlockRange; Count uint; Seed int64 }
type SparseSteps  struct{ Range chain.BlockRange; Step uint64 }
```

**설계 포인트**:
- 샘플링 결과는 **결정론적**이어야 테스트 가능 → `Random`은 seed 지정 필수
- 샘플링 자체에 외부 I/O 없음 (tip block은 호출자가 주입)

### `Trigger` (sealed-type 패턴)
```go
type Trigger interface{ isTrigger() }

type ManualTrigger    struct{ User string }
type ScheduledTrigger struct{ CronExpr string }
type RealtimeTrigger  struct{ BlockNumber chain.BlockNumber }  // post-MVP이지만 미리 정의

func (ManualTrigger) isTrigger()    {}
func (ScheduledTrigger) isTrigger() {}
func (RealtimeTrigger) isTrigger()  {}
```
- Go엔 진짜 sum type이 없어 이 패턴(unexported method로 외부 구현 차단)을 씀
- 실시간 streaming 확장점 확보

### `Schedule` (간단한 값객체)
```go
type Schedule struct {
    CronExpr string  // "0 */6 * * *" 등
    Timezone string  // "UTC" 기본
}
```

## `diff/` 설계

### `Discrepancy`
```go
type Discrepancy struct {
    RunID       verification.RunID
    Metric      verification.Metric
    Block       chain.BlockNumber
    Subject     Subject  // 비교 대상 (address 등)
    Values      map[source.SourceID]ValueSnapshot
    DetectedAt  time.Time
}

type Subject struct {
    Type    SubjectType  // "block" | "address" | "contract"
    Address *chain.Address
}

type ValueSnapshot struct {
    Raw            string                // 문자열로 정규화해 보관
    Typed          any                   // 원래 타입 (uint64, *big.Int 등)
    FetchedAt      time.Time
    ReflectedBlock *chain.BlockNumber    // 응답이 반영하는 블록 (Tier B anchor window용, nil=미노출)
}
```

### `Tolerance`

2026-04-20 확장: Tier B의 latest-only 응답을 anchor block과 정합성 대조하기 위해 `ReflectedBlock` 메타를 판정에 포함.

```go
// Tolerance 판정에 필요한 비교 컨텍스트
type CompareContext struct {
    Anchor         source.BlockTag              // Run이 고정한 anchor (보통 finalized block)
    AnchorBlock    chain.BlockNumber            // Anchor 해석 결과 (numeric)
    ReflectedA     *chain.BlockNumber           // a의 응답이 실제 반영하는 블록 (nil = 미노출)
    ReflectedB     *chain.BlockNumber
}

type Tolerance interface {
    // ok: 동등 판정, needDiscard: 샘플 자체를 버려야 함 (anchor window 밖)
    Judge(a, b ValueSnapshot, metric verification.Metric, ctx CompareContext) (ok bool, needDiscard bool)
}

// 완전 일치 요구
type ExactMatch struct{}

// 수치 허용오차 (balance 등 대단위 수치)
type NumericTolerance struct {
    AbsoluteMax *big.Int
    RelativePPM uint
}

// Tier B 응답 대응: ReflectedBlock이 anchor window 안에 들어와야만 비교 유효
type AnchorWindowed struct {
    Inner  Tolerance               // ExactMatch / NumericTolerance / Observational
    TolBack  uint64                // anchor - tol_back 이전 응답은 discard
    TolFwd   uint64                // anchor + tol_fwd 이후 응답도 discard
}
// 기본값: tol_back=0, tol_fwd=64 (≈Optimism 2분)

// 관찰 전용: 항상 ok=true, needDiscard=false. 자동 판정 안 함.
type Observational struct{}
```

**판정 흐름**:
1. `AnchorWindowed`: 양측의 `ReflectedBlock`이 모두 `[anchor-tol_back, anchor+tol_fwd]`에 들어오는지 먼저 검사. 하나라도 밖이면 `needDiscard=true` → `Judgement` 발급 안 함 (discrepancy도 저장 X).
2. 한쪽만 `ReflectedBlock=nil`이면 (소스가 메타 미노출) `Inner`에 위임하되, 기본 Policy가 trusted sources 결정 시 해당 소스 신뢰도 감점.
3. 모두 통과 → `Inner.Judge()`로 실제 값 비교.

MVP에선 대부분 `ExactMatch` 또는 `AnchorWindowed{Inner: ExactMatch}` 사용.

### `Judgement`
```go
type Severity string
const (
    SevInfo     Severity = "info"
    SevWarning  Severity = "warning"
    SevError    Severity = "error"
    SevCritical Severity = "critical"
)

type Judgement struct {
    Severity       Severity
    TrustedSources []source.SourceID  // 어느 값이 "진실"에 가까운가
    Reasoning      string              // 판정 근거 (디버그용)
}

// 기본 판정 규칙
type JudgementPolicy interface {
    Judge(d Discrepancy) Judgement
}

// MVP 기본 정책:
// - Metric.Category 기반 1차 분기
// - BlockImmutable / AddressAtBlock: RPC가 있으면 trusted, 다른 소스 mismatch → Critical
// - AddressLatest: RPC가 있으면 trusted, mismatch → Warning (latest race 가능성)
// - Snapshot: 판정 없음 (관찰만). 선택적으로 Info.
// - 소스 신뢰도 서열: RPC > external explorer (blockscout/etherscan) > custom indexer
type DefaultPolicy struct{}
```

## 세부 단계 (TDD)

### verification/

#### 4.1 `Metric` + `MetricCategory`
- [ ] 테스트: 기본 카탈로그의 각 Metric이 올바른 카테고리·Capability 매핑
- [ ] 테스트: 사용자 정의 Metric 등록 API (확장성)
- [ ] 구현

#### 4.2 `SamplingStrategy`
- [ ] 테스트: 각 4종 구현체
  - `FixedList`: 주어진 리스트 그대로
  - `LatestN`: tip-N+1 ~ tip
  - `Random`: seed 같으면 같은 결과, Count만큼
  - `SparseSteps`: start~end를 step 간격으로
- [ ] 엣지 케이스: 빈 range, N이 tip보다 큼, Step이 range보다 큼
- [ ] 구현

#### 4.3 `Trigger` / `Schedule`
- [ ] 테스트: cron 표현식 검증
- [ ] 구현

#### 4.4 `Run`
- [ ] 테스트: 생성자 validation (metrics 비어있으면 실패 등), 상태 전이 (pending→running→completed 정상, completed→running 금지)
- [ ] 구현

### diff/

#### 4.5 `Discrepancy` / `ValueSnapshot`
- [ ] 테스트: 구성, 직렬화
- [ ] 구현

#### 4.6 `Tolerance`
- [ ] 테스트: ExactMatch (일치/불일치), NumericTolerance (절대·상대 오차 경계값)
- [ ] 테스트: `AnchorWindowed` — 양측 ReflectedBlock이 window 안/밖 조합 (4케이스), nil 처리
- [ ] 테스트: `Observational` — 항상 ok=true, needDiscard=false
- [ ] 구현

#### 4.7 `Judgement` + `DefaultPolicy`
- [ ] 테스트 시나리오:
  - 3소스 모두 일치 → Judgement 없음 (Discrepancy 자체가 없음)
  - RPC=A, Etherscan=A, Internal=B → Warning, trusted=[RPC, Etherscan]
  - RPC=A, Etherscan=B, Internal=C → Critical, trusted=[RPC]
  - RPC 없음, Etherscan=A, Internal=B → Warning
- [ ] 구현

## 의존 Phase

- Phase 1 (chain 값객체)
- Phase 2 (source 포트 — Capability 참조)

## 주의

- **도메인 순수성**: 이 Phase의 모든 코드는 `net/http`, `database/sql`, 라이브러리 import **0** (표준 라이브러리 + `internal/chain`, `internal/source` 포트만)
- **`big.Int` 사용**: 표준 라이브러리라 허용. gorm/asynq는 금지.
- **diff 판정의 정책성**: `DefaultPolicy` 외에도 향후 사용자가 **정책 커스터마이즈**하고 싶어할 수 있음 → 인터페이스로 분리해둠

## 참고

- [Go에서 sum type 흉내내기](https://eli.thegreenplace.net/2022/summary-of-go-1-in-a-nutshell/)
- [DDD: Value Objects vs Entities](https://martinfowler.com/bliki/ValueObject.html)
