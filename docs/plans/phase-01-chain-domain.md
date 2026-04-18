# Phase 1 — `chain/` 도메인

## 목표

체인 세계의 **원시 값객체(Value Object)** 만 정의. 다른 context들이 공통으로 참조할 불변 개념들의 **유비쿼터스 언어(ubiquitous language)** 를 확정.

## 산출물 (DoD)

- [ ] `internal/chain/chain.go` — `ChainID` + 체인 메타 (Optimism 상수)
- [ ] `internal/chain/block.go` — `BlockNumber`, `BlockHash`
- [ ] `internal/chain/address.go` — `Address` (EIP-55 체크섬 포함)
- [ ] `internal/chain/hash.go` — `Hash32`, `TxHash` (TxHash = Hash32 alias)
- [ ] `internal/chain/range.go` — `BlockRange` (start, end, 정합성 검증)
- [ ] 전 패키지 블랙박스 테스트 (`package chain_test`)
- [ ] 커버리지 90%+

## 왜 값객체로?

- **불변**: 생성 시점에 검증, 이후 수정 불가
- **자기 검증**: `NewAddress("0x...")`가 형식 체크·길이 체크·체크섬 검증까지
- **동등성**: value-by-value 비교 (포인터 비교 X)
- **다른 context가 이 개념을 가져다 써도 의미가 왜곡되지 않음**

## 설계

### `ChainID`
```go
type ChainID uint64

const (
    EthereumMainnet ChainID = 1
    OptimismMainnet ChainID = 10
    // 확장 포인트: Base(8453), Arbitrum(42161), ...
)

// 기본 속성
func (c ChainID) Uint64() uint64
func (c ChainID) String() string
func (c ChainID) IsKnown() bool

// 어댑터용 표준 키 (일관성을 위해 chain 패키지가 단독으로 보유)
// 어댑터는 이 키로 자신의 endpoint / param 값을 결정
func (c ChainID) Slug() string   // "optimism", "ethereum", "base" - 소문자 슬러그
func (c ChainID) DisplayName() string  // "Optimism", "Ethereum", "Base"
```

**설계 의도**: Etherscan V2 multichain은 숫자 `chainid=10`을 쓰고, Blockscout은 배포 URL(`optimism.blockscout.com`)로 구분하며, GraphQL 류 indexer는 `chainName: "optimism"`을 쓰기도 한다. **이 매핑 책임을 한 곳에 모아** 어댑터가 `chain.OptimismMainnet.Slug()`만 호출하면 되도록 함.

어댑터별 endpoint 매핑 테이블은 어댑터 자신의 패키지에서 관리:
```go
// adapters/blockscout/chain_map.go
var defaultBaseURL = map[chain.ChainID]string{
    chain.OptimismMainnet: "https://optimism.blockscout.com",
    chain.EthereumMainnet: "https://eth.blockscout.com",
}
```
사용자가 override 가능 (사내 mirror 등).

### `BlockNumber`
```go
type BlockNumber uint64

func NewBlockNumber(n uint64) BlockNumber
func (b BlockNumber) Uint64() uint64
func (b BlockNumber) Hex() string  // "0x..." 형태 (RPC용)
// JSON (un)marshal: 10진수와 16진수 둘 다 받을 수 있게
```

### `Address`
```go
type Address [20]byte

func NewAddress(s string) (Address, error)   // "0x..." 입력, EIP-55 체크섬 검증
func MustAddress(s string) Address            // 테스트/상수용
func (a Address) Hex() string                 // EIP-55 체크섬 출력
func (a Address) String() string
// JSON (un)marshal 지원
```

### `Hash32`, `TxHash`
```go
type Hash32 [32]byte
type TxHash = Hash32   // alias

func NewHash32(s string) (Hash32, error)
func (h Hash32) Hex() string
```

### `BlockRange`
```go
type BlockRange struct {
    Start BlockNumber
    End   BlockNumber  // inclusive
}

func NewBlockRange(start, end BlockNumber) (BlockRange, error) // start <= end 검증
func (r BlockRange) Len() uint64
func (r BlockRange) Contains(n BlockNumber) bool
```

## 세부 단계 (TDD)

### 1.1 `ChainID`
- [ ] 테스트: 알려진 ID 상수, `IsKnown()`, `Slug()`, `DisplayName()` 매핑
- [ ] 구현

### 1.2 `BlockNumber`
- [ ] 테스트: `Uint64()`, `Hex()`, JSON marshal/unmarshal (10진수·16진수 둘 다)
- [ ] 구현

### 1.3 `Address`
- [ ] 테스트: 성공 케이스, 실패 케이스 (길이·prefix·non-hex·체크섬 불일치), `Hex()` 출력이 EIP-55 준수, JSON
- [ ] 구현 (체크섬 구현은 [EIP-55](https://eips.ethereum.org/EIPS/eip-55) 알고리즘 직접 또는 `go-ethereum/common`에서 뽑아쓰기)

### 1.4 `Hash32` / `TxHash`
- [ ] 테스트: 성공/실패, JSON
- [ ] 구현

### 1.5 `BlockRange`
- [ ] 테스트: 생성 실패 (start > end), `Len()`, `Contains()`, 빈 range
- [ ] 구현

## 의존 Phase

- Phase 0 (프로젝트 골격)

## 주의

- `go-ethereum/common`을 **의존성으로 직접 도입할지** 결정 필요:
  - Pros: 검증 로직·체크섬 성숙, 재구현 불필요
  - Cons: `chain/` 패키지가 외부 라이브러리에 묶임 (도메인 순수성 타협)
- **제안**: `go-ethereum/common`의 체크섬 알고리즘만 참고하여 **순수 Go로 직접 구현**. 단, 구현 부담이 크면 common을 얇게 래핑하되 타입은 외부에 노출하지 않음.

## 확장 포인트 (미래)

- `ChainID` enum 확장 → Optimism 외 다른 체인 추가
- 주소 형식이 다른 체인(Bech32 등) 지원 시 `Address`를 인터페이스로 재설계 고려 (MVP에선 EVM 주소만)

## 참고

- [EIP-55 — Mixed-case checksum address encoding](https://eips.ethereum.org/EIPS/eip-55)
- [go-ethereum common package](https://github.com/ethereum/go-ethereum/tree/master/common)
