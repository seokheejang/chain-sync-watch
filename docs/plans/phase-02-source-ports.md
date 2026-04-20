# Phase 2 — `source/` 포트 + 공통 모델 (코어 추상)

## 목표

외부 데이터 소스의 **순수 추상 레이어**를 `internal/source/`에 구현. **어떤 구체 어댑터 코드도 이 패키지에 없음** (`database/sql`이 `mysql` 드라이버를 모르는 것과 동일).

이 Phase의 산출물은:
- Phase 3의 번들 어댑터(`adapters/rpc`, `adapters/blockscout`, `adapters/etherscan`)가 구현할 계약
- Phase 4~5 도메인/애플리케이션 테스트에서 **fake 구현**으로 대체되어 사용

## 산출물 (DoD)

- [ ] `internal/source/source.go` — `Source` 인터페이스
- [ ] `internal/source/capability.go` — 필드 단위 `Capability` enum
- [ ] `internal/source/query.go` — Query 타입들 (조회 요청 입력)
- [ ] `internal/source/result.go` — Result 타입들 (조회 응답)
- [ ] `internal/source/id.go` — `SourceID` (소스 식별자)
- [ ] `internal/source/errors.go` — 공통 에러
- [ ] `internal/source/fake/` — 테스트용 inmem fake 구현
- [ ] 블랙박스 테스트
- [ ] 이 패키지의 외부 import는 `internal/chain`과 표준 라이브러리만 (린트 룰로 강제)

## 핵심 원칙

- **`internal/source/`에 구체 HTTP 클라이언트/gorm/ethclient 등의 import 금지**
- 모든 어댑터는 `adapters/<name>/`에 **독립 패키지**로 존재하며 `internal/source/`를 import
- 사용자는 원하는 어댑터만 선택적으로 import (미사용 어댑터 의존성은 바이너리에 포함되지 않음)

## 설계

### `SourceID`

```go
type SourceID string

// NOTE: 코어는 특정 소스 이름을 enum 상수로 못박지 않는다.
// 어댑터 자체가 자기 ID를 선언. 예시:
//   adapters/rpc:        source.SourceID("rpc")
//   adapters/blockscout: source.SourceID("blockscout")
//   adapters/etherscan:  source.SourceID("etherscan")
//   examples/custom-*:   사용자 정의
```

왜 enum 안 쓰나: 코어가 특정 어댑터를 알면 의존 방향이 역전됨. 어댑터 자신이 ID를 선언하고 코어는 그냥 `string`으로 취급.

### `Capability` — **필드 단위** (변경)

```go
type Capability string

const (
    // Block immutable fields (블록 번호로 앵커, 온체인 불변)
    CapBlockHash            Capability = "block.hash"
    CapBlockParentHash      Capability = "block.parent_hash"
    CapBlockTimestamp       Capability = "block.timestamp"
    CapBlockTxCount         Capability = "block.tx_count"
    CapBlockGasUsed         Capability = "block.gas_used"
    CapBlockStateRoot       Capability = "block.state_root"
    CapBlockReceiptsRoot    Capability = "block.receipts_root"
    CapBlockTransactionsRoot Capability = "block.transactions_root"
    CapBlockMiner           Capability = "block.miner"

    // Address latest (현 시점 address 스냅샷)
    CapBalanceAtLatest      Capability = "address.balance_at_latest"
    CapNonceAtLatest        Capability = "address.nonce_at_latest"
    CapTxCountAtLatest      Capability = "address.tx_count_at_latest"

    // Address at specific block (RPC 원천 지원, indexer는 대개 미지원)
    CapBalanceAtBlock       Capability = "address.balance_at_block"
    CapNonceAtBlock         Capability = "address.nonce_at_block"

    // Snapshot / cumulative (시점 무의존)
    CapTotalAddressCount    Capability = "snapshot.total_addresses"
    CapTotalTxCount         Capability = "snapshot.total_txs"
    CapERC20TokenCount      Capability = "snapshot.erc20_token_count"
    CapTotalContractCount   Capability = "snapshot.total_contracts"
)
```

**왜 필드 단위**: 현장 조사 결과 소스마다 노출하는 필드가 크게 다름 (예: Blockscout v2 REST는 state_root 미노출, RPC는 제공). Capability를 "블록 조회"로 뭉치면 계약 세분도가 떨어지고, Judgement 단계에서 "이 필드는 어느 소스가 진실인가" 판단을 못 함.

### `Source` 인터페이스

```go
type Source interface {
    ID() SourceID
    ChainID() chain.ChainID
    Supports(Capability) bool

    // 메서드는 "조회 단위" 기준으로 묶되, 어떤 필드가 채워지는지는 Capability로 확인
    FetchBlock(ctx context.Context, q BlockQuery) (BlockResult, error)
    FetchAddressLatest(ctx context.Context, q AddressQuery) (AddressLatestResult, error)
    FetchAddressAtBlock(ctx context.Context, q AddressAtBlockQuery) (AddressAtBlockResult, error)
    FetchSnapshot(ctx context.Context, q SnapshotQuery) (SnapshotResult, error)
}
```

**설계 포인트**:
- 메서드는 "조회 단위"로 묶어 RPC·REST 호출 횟수 최소화
- Result의 각 필드는 `*T`로 optional — 어댑터가 미지원이면 nil 반환
- 호출자는 `Supports(Cap...)` 먼저 확인하거나, Result에서 nil 필드 skip
- 인터페이스가 커지면 Phase 3 끝난 시점에 `BlockReader`, `AddressReader` 등으로 분리 고려 (ISP)

### Query / Result

```go
type BlockQuery struct {
    Number chain.BlockNumber
}

type BlockResult struct {
    Number            chain.BlockNumber
    Hash              *chain.BlockHash
    ParentHash        *chain.BlockHash
    Timestamp         *time.Time
    TxCount           *uint64
    GasUsed           *uint64
    StateRoot         *chain.Hash32
    ReceiptsRoot      *chain.Hash32
    TransactionsRoot  *chain.Hash32
    Miner             *chain.Address

    SourceID    SourceID
    FetchedAt   time.Time
    RawResponse []byte  // 디버깅·감사용 (config로 on/off)
}

type AddressQuery struct {
    Address chain.Address
}

type AddressLatestResult struct {
    Balance  *big.Int
    Nonce    *uint64
    TxCount  *uint64
    SourceID SourceID
    // ...
}

type AddressAtBlockQuery struct {
    Address chain.Address
    Block   chain.BlockNumber
}

type AddressAtBlockResult struct {
    Balance  *big.Int
    Nonce    *uint64
    Block    chain.BlockNumber
    SourceID SourceID
}

type SnapshotQuery struct {
    // 시점 무의존 (소스가 판단)
}

type SnapshotResult struct {
    TotalAddressCount  *uint64
    TotalTxCount       *uint64
    TotalContractCount *uint64
    ERC20TokenCount    *uint64
    SnapshotAt         time.Time  // 소스가 알려주는 시점 (없으면 FetchedAt)
    SourceID           SourceID
}
```

### 에러

```go
var (
    ErrUnsupported       = errors.New("source: unsupported capability")
    ErrRateLimited       = errors.New("source: rate limited")
    ErrSourceUnavailable = errors.New("source: unavailable")
    ErrNotFound          = errors.New("source: not found")
    ErrInvalidResponse   = errors.New("source: invalid response")
)
```

## 세부 단계 (TDD)

### 2.1 `SourceID` / `Capability`
- [ ] 테스트: 상수 유효성
- [ ] 구현

### 2.2 Query / Result 타입
- [ ] 순수 데이터 구조 (테스트 최소)
- [ ] 구현

### 2.3 `Source` 인터페이스
- [ ] 선언

### 2.4 Fake 구현 (`source/fake`)
- [ ] `FakeSource` struct — 사전 설정한 응답 반환
- [ ] 호출 기록 (method/args)
- [ ] 에러 주입 (rate limit 시뮬레이션 등)
- [ ] Capability 목록을 생성자에서 선언 가능
- [ ] 테스트: `Source` 계약 만족 + 호출 기록 정확성

### 2.5 import 제약 린트 룰
- [ ] `.golangci.yml` 또는 custom linter: `internal/source/` 패키지가 `internal/chain` 외의 내부 패키지를 import 못 하게
- [ ] 어댑터 외부 라이브러리(gorm, ethclient 등)를 `internal/source/`에서 import 시 CI fail

## 의존 Phase

- Phase 1 (`chain/` 값객체)

## 주의 / 트레이드오프

- **단일 vs. 분할 인터페이스**: 현재 4 메서드 단일 interface. capability가 늘면 `BlockReader`, `AddressReader`, `SnapshotReader` 등으로 분해. Phase 3 구현 후 판단.
- **`*big.Int` 노출**: balance 표현용. 표준 라이브러리라 허용. 의존성 부담 없음.
- **Optional 필드 대량화 vs. 메서드 분할**: 현 설계는 "한 번의 호출로 여러 필드 획득" 선호. 미지원 필드는 nil.
- **`RawResponse` 보관**: diff 재현·감사 목적으로 유용. 저장 공간·민감정보 고려 → 기본 off, config flag로 on.
- **신뢰도(가중치)**: 여기선 표현 X → Phase 4 `diff/`의 Judgement 단계에서 결정.

## 참고

- [Hexagonal ports & adapters](https://alistair.cockburn.us/hexagonal-architecture/)
- [Go `database/sql` driver pattern](https://pkg.go.dev/database/sql/driver)
- [Interface Segregation Principle](https://en.wikipedia.org/wiki/Interface_segregation_principle)

---

# Phase 2C — Capability 확장 + Tier 체계 + Anchor BlockTag (2026-04-20 추가)

Phase 3 어댑터 구현 **전** 진행. research doc 추가 합의사항 반영.

## 배경

[docs/research/external-api-coverage.md](../research/external-api-coverage.md) "검증 Tier 분류" 및 "추가 합의 (2026-04-20)" 섹션 참조. 요약:

- **3-tier 모델**: A(RPC-canonical 전수) / B(Indexer-derived 샘플링) / C(Mixed).
- **anchor 전략**: finalized 고정 + 응답 메타 기반 사후 대조.
- **지표 우선순위 원칙**: `at block N` 또는 `startblock/endblock` 같이 기준 명확한 쿼리 우선. latest-only는 후순위.

이를 반영해 `source/` 포트를 확장한다.

## 산출물 (DoD)

- [ ] `internal/source/capability.go` — Tier B 신규 Capability 4개 + 각 Capability에 `Tier()` 메타 부여
- [ ] `internal/source/tier.go` — `Tier` enum (A/B/C)
- [ ] `internal/source/blocktag.go` — `BlockTag` 공통 값객체
- [ ] 기존 Query 타입들에 `Anchor BlockTag` 필드 추가 (2.x 호환 위해 default = `BlockTagLatest`)
- [ ] `internal/source/result.go` — Result에 `ReflectedBlock *chain.BlockNumber` 메타 필드 추가 (anchor 사후 대조용)
- [ ] Tier B 대응 Query/Result 4종 추가 (ERC-20 balance/holdings, internal tx by block/tx)
- [ ] `internal/source/fake/` — 신규 Capability + BlockTag 처리 확장
- [ ] 블랙박스 테스트 갱신

## 설계

### `Tier` 분류

```go
// internal/source/tier.go
type Tier uint8

const (
    TierA Tier = iota + 1  // RPC-canonical (전수 가능, RPC가 정답)
    TierB                  // Indexer-derived (cross-indexer 샘플링만)
    TierC                  // Mixed (RPC/3rd-party 양쪽, 지표별 결정)
)

func (c Capability) Tier() Tier { /* 표에서 조회 */ }
```

**Capability → Tier 매핑 (초안)**:

| Capability | Tier |
|---|---|
| `block.*` (hash, parent, timestamp, roots, miner, gas, tx_count) | A |
| `address.balance_at_block`, `address.nonce_at_block` | A |
| `address.balance_at_latest`, `address.nonce_at_latest`, `address.tx_count_at_latest` | A |
| `snapshot.total_addresses`, `snapshot.total_txs`, `snapshot.total_contracts`, `snapshot.erc20_token_count` | B |
| `address.erc20_balance_at_latest` (특정 토큰) | C |
| `address.erc20_holdings_at_latest` (보유 전체 목록) | B |
| `trace.internal_tx_by_tx`, `trace.internal_tx_by_block` | C |

### `BlockTag` — 공통 anchor

```go
// internal/source/blocktag.go
type BlockTag struct {
    kind BlockTagKind
    num  chain.BlockNumber // kind == Numeric 때만 유효
}

type BlockTagKind uint8

const (
    BlockTagLatest BlockTagKind = iota
    BlockTagSafe
    BlockTagFinalized
    BlockTagNumeric
)

func BlockTagAt(n chain.BlockNumber) BlockTag { /* ... */ }
func (b BlockTag) String() string             { /* "latest"|"safe"|"finalized"|"0x..." */ }
```

**원칙**:
- 모든 Query에 `Anchor BlockTag` 필드. 기본값 `BlockTagLatest`와 의미 같지만 명시적으로 선언.
- Tier A 검증은 보통 `BlockTagNumeric` 또는 `BlockTagFinalized` 사용.
- Tier B는 어댑터가 `at block` 지원 안 하면 **`ErrUnsupportedAtBlock` 반환** → caller(verification use case)가 reflected-block 메타로 사후 대조 분기.

### Result 메타 확장

```go
// 모든 Result 공통 embed 구조
type ResultMeta struct {
    SourceID       SourceID
    FetchedAt      time.Time
    Anchor         BlockTag            // 요청 시 anchor
    ReflectedBlock *chain.BlockNumber  // 응답이 실제 반영하는 블록 (메타 없으면 nil)
    RawResponse    []byte              // config on/off
}
```

`ReflectedBlock`이 nil인 Tier B 응답 = "관찰 전용" 판정 (자동 diff 대상 제외).

### 신규 Capability (2C 추가분)

```go
// Per-address ERC-20
CapERC20BalanceAtLatest  Capability = "address.erc20_balance_at_latest"   // Tier C
CapERC20HoldingsAtLatest Capability = "address.erc20_holdings_at_latest"  // Tier B

// Internal transactions (debug_trace 대체)
CapInternalTxByBlock Capability = "trace.internal_tx_by_block"            // Tier C
CapInternalTxByTx    Capability = "trace.internal_tx_by_tx"               // Tier C
```

### 신규 Query / Result

```go
type ERC20BalanceQuery struct {
    Address      chain.Address
    TokenAddress chain.Address
    Anchor       BlockTag
}
type ERC20BalanceResult struct {
    Balance  *big.Int
    Decimals uint8
    Meta     ResultMeta
}

type ERC20HoldingsQuery struct {
    Address chain.Address
    Anchor  BlockTag
}
type ERC20HoldingsResult struct {
    Tokens []TokenHolding
    Meta   ResultMeta
}
type TokenHolding struct {
    Contract chain.Address
    Name     string
    Symbol   string
    Decimals uint8
    Balance  *big.Int
}

// internal tx: "범위/식별자 기준" — at-block anchor 필요 없음 (startblock/endblock 또는 txhash로 고정)
type InternalTxByBlockQuery struct {
    Block chain.BlockNumber
}
type InternalTxByTxQuery struct {
    TxHash chain.Hash32
}
type InternalTxResult struct {
    Traces []InternalTx
    Meta   ResultMeta
}
type InternalTx struct {
    From, To chain.Address
    Value    *big.Int
    GasUsed  uint64
    CallType string  // "call", "delegatecall", "create", ...
    Error    string
}
```

### `Source` 인터페이스 확장

```go
type Source interface {
    // 기존
    ID() SourceID
    ChainID() chain.ChainID
    Supports(Capability) bool
    FetchBlock(context.Context, BlockQuery) (BlockResult, error)
    FetchAddressLatest(context.Context, AddressQuery) (AddressLatestResult, error)
    FetchAddressAtBlock(context.Context, AddressAtBlockQuery) (AddressAtBlockResult, error)
    FetchSnapshot(context.Context, SnapshotQuery) (SnapshotResult, error)

    // 2C 신규
    FetchERC20Balance(context.Context, ERC20BalanceQuery) (ERC20BalanceResult, error)
    FetchERC20Holdings(context.Context, ERC20HoldingsQuery) (ERC20HoldingsResult, error)
    FetchInternalTxByBlock(context.Context, InternalTxByBlockQuery) (InternalTxResult, error)
    FetchInternalTxByTx(context.Context, InternalTxByTxQuery) (InternalTxResult, error)
}
```

**메서드 수가 늘어나면** Phase 3 이후에 `ERC20Reader`, `TraceReader` 등으로 분할(ISP) 검토.

### 에러 확장

```go
var (
    ErrUnsupportedAtBlock = errors.New("source: anchor at block not supported (latest only)")
)
```

## 세부 단계 (TDD)

### 2C.1 `Tier` + `Capability.Tier()`
- [ ] 테스트: 각 Capability가 정의된 Tier 반환
- [ ] 구현

### 2C.2 `BlockTag`
- [ ] 테스트: round-trip (`String` → parse), latest/safe/finalized/numeric 각 케이스
- [ ] 구현

### 2C.3 기존 Query 타입에 `Anchor` 필드 추가
- [ ] zero value = `BlockTagLatest` 보장 (기존 테스트 불변)
- [ ] 테스트 갱신

### 2C.4 `ResultMeta` embed
- [ ] 기존 Result들 meta 흡수
- [ ] `ReflectedBlock` nil 처리 블랙박스 테스트

### 2C.5 신규 Capability + Query/Result (4종)
- [ ] 타입 선언 + 테스트
- [ ] `Source` 인터페이스 확장

### 2C.6 Fake 확장
- [ ] 신규 메서드 4개 지원
- [ ] `ErrUnsupportedAtBlock` 주입 가능
- [ ] `ReflectedBlock` 주입 가능

## 의존 Phase

- Phase 2 완료 (기 구현된 포트 위에 확장)

## 주의

- **후방 호환성**: 기존 Query 타입에 `Anchor` 추가는 zero value 안전 (default latest). 기존 테스트·fake 사용처 영향 없음 원칙.
- **`ResultMeta` 리팩터**: 모든 Result가 embed 구조로 변경됨 → 블랙박스 테스트 경로 명시적 갱신 필요.
- **Tier C Capability의 실제 주체**: `CapERC20BalanceAtLatest`는 RPC eth_call도 가능, Blockscout REST도 가능. 어느 쪽을 primary로 할지는 어댑터 레벨 정책.
- **L2 특이 필드는 포함 안 함** (백로그).
- **indexer 측 Capability 선언**: Phase 3/4 이후 필요 시점에 도입. 2C 범위 아님.
