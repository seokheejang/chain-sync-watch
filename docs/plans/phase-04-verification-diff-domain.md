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

| 카테고리 | 기본 Tolerance | 판정 정책 |
|---|---|---|
| `BlockImmutable` | `ExactMatch` (strict) | 1개라도 다르면 Critical, RPC를 trusted로 |
| `AddressLatest` | `ExactMatch` | 다르면 Warning. "latest" 정의 차이(block height 차이) 가능성 메모 |
| `AddressAtBlock` | `ExactMatch` | RPC 기반이라 Critical, RPC가 trusted |
| `Snapshot` | `Observational` (판정 없음) | 기록만 — 소스별 "의미 정의" 달라 자동 판정 위험 |

**이유**: 현장 관찰(내부 indexer vs Blockscout)에서 `totalAddressCount`가 21% 격차. 의미 정의 차이로 자동 Error 판정은 노이즈. Snapshot은 "관찰" 레이어로 대시보드에만 노출, diff로 저장은 하되 Judgement는 발급 안 함 (또는 Info만).

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
    Raw        string  // 문자열로 정규화해 보관
    Typed      any     // 원래 타입 (uint64, *big.Int 등)
    FetchedAt  time.Time
}
```

### `Tolerance`
```go
type Tolerance interface {
    Allows(a, b ValueSnapshot, metric verification.Metric) bool
}

// 완전 일치 요구
type ExactMatch struct{}

// 수치 허용오차 (balance 등 대단위 수치에 유용 — 보통은 ExactMatch지만 유연성 확보)
type NumericTolerance struct {
    AbsoluteMax *big.Int  // 절대 오차 상한
    RelativePPM uint      // 부 단위 상대 오차 (parts per million)
}
```
MVP에선 대부분 `ExactMatch`이지만, 구조만 먼저 확보.

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
