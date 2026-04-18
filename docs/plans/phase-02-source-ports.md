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
