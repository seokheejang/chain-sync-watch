# Phase 3 — 번들 Source 어댑터 (bundled adapters)

## 목표

Phase 2에서 정의한 `Source` 포트의 **독립 패키지 번들 구현체** 제공. 사용자가 원하는 것만 선택적 import 가능. `database/sql` 의 드라이버 패턴.

**사용자 정의 indexer(예: 내부 GraphQL)** 는 **이 Phase가 아니라** `examples/custom-graphql-adapter/`에서 패턴만 제공하고, 실사용은 사용자가 자기 repo에서 구현.

## ⚠️ 2026-04-18 업데이트 — Etherscan 후순위 + Routescan 추가

외부 API 조사(`docs/research/external-api-coverage.md`) 결과 초안 구성 변경:

- **Etherscan V2 Free는 Optimism 미지원** — Free tier는 ETH/Polygon/Arbitrum만, Optimism·Base는 paid(Standard $199/mo). **MVP 기본 bundle에서 제외**, opt-in으로 후순위 이동.
- **Routescan 추가** — Etherscan-호환 API를 Optimism free로 keyless 제공. 5 req/s, 100k/day. `internal/ethscan/` 공유 client 그대로 재사용 가능.
- **RPC는 사용자 자체 full-archive 노드** 사용 예정 (공개 RPC는 archive 미지원 or debug_* 차단).
- **Capability 확장 필요**: ERC-20 per-address (balance, holdings), internal_tx (debug_trace 대체) — Phase 2C로 분리 후 본 Phase 시작.

### 수정된 어댑터 라인업 (우선순위 순)

| 어댑터 | 기본 활성화 | 키 필요 | 비고 |
|---|:---:|:---:|---|
| `adapters/rpc/` | ✅ | ❌ | 사용자 archive 노드 |
| `adapters/blockscout/` | ✅ | ❌ | native REST v2 + proxy fallback |
| `adapters/routescan/` | ✅ | ❌ | Etherscan-호환, Optimism 무료 |
| `adapters/etherscan/` | ❌ | ✅ | **후순위** — Etherscan-지원 체인(ETH 등)에서만 가치 |
| `adapters/alchemy/` | ❌ | ✅ | post-MVP opt-in (debug_*, enhanced APIs) |

## 산출물 (DoD)

### 번들 어댑터 (각자 독립 패키지)
- [ ] `adapters/rpc/` — JSON-RPC (ethclient 기반)
- [ ] `adapters/blockscout/` — Blockscout v2 REST (+ Etherscan-호환 proxy 모듈)
- [ ] `adapters/etherscan/` — Etherscan V2 Multichain

### 공용 유틸 (모든 어댑터가 공유)
- [ ] `adapters/internal/httpx/` — HTTP 클라이언트 베이스 (timeout, retry, rate limit, 로깅·메트릭 훅). `internal` 디렉토리 패턴으로 adapters/ 밖에서 import 차단.

### 예시
- [ ] `examples/custom-graphql-adapter/` — GraphQL 기반 어댑터 구현 패턴 (익명 스키마로 시연, 실제 내부 URL·스키마 포함 금지)

### 문서
- [ ] `docs/research/source-shapes.md` — 필드별 지원 매트릭스 (RPC / Blockscout / Etherscan / 예시)
- [ ] `docs/adapters/writing-custom-adapter.md` — 어댑터 작성 가이드

### 테스트
- [ ] 각 어댑터 단위 테스트 (`httptest.Server` + fixture)
- [ ] (선택) 실 네트워크 E2E 스모크 (`-tags=e2e`)

## 공용 HTTP 베이스 (`adapters/internal/httpx/`)

```go
type Client struct {
    base       string
    httpClient *http.Client
    rate       *rate.Limiter  // golang.org/x/time/rate
    retry      RetryPolicy
    logger     *slog.Logger
}

type RetryPolicy struct {
    MaxAttempts int
    BaseDelay   time.Duration
    MaxDelay    time.Duration
    Retryable   func(status int, err error) bool  // 기본: 429, 5xx, net err
}
```

- **Rate limit**: 어댑터별 기본값 + config override
- **Retry**: 429/5xx/네트워크 에러만. 4xx 영구에러는 재시도 X.
- **Timeout**: context 전파 + ceiling
- **context-aware**: `ctx.Done()` 즉시 반영

## 어댑터 1 — `adapters/rpc/`

**라이브러리**: `github.com/ethereum/go-ethereum/ethclient` (인프라 레이어만 사용, 도메인 오염 없음)

**Capability 매핑**:
| Capability | RPC |
|---|---|
| `CapBlockHash/ParentHash/Timestamp/TxCount/GasUsed/StateRoot/ReceiptsRoot/TransactionsRoot/Miner` | `eth_getBlockByNumber` |
| `CapBalanceAtLatest/AtBlock` | `eth_getBalance` (blockTag=latest or hex) |
| `CapNonceAtLatest/AtBlock` | `eth_getTransactionCount` |
| `CapTotalAddressCount / TotalTxCount / ERC20TokenCount / TotalContractCount` | ❌ 불가 (RPC 원천 불가) |

**구성**:
```go
// adapters/rpc/client.go
type Adapter struct { /* ethclient.Client, chain, options */ }

func New(chainID chain.ChainID, rpcURL string, opts ...Option) (*Adapter, error)

func (*Adapter) ID() source.SourceID { return "rpc" }
func (*Adapter) Supports(source.Capability) bool
// Fetch* 메서드 구현
```

**주의**:
- archive node가 아닌 경우 historical balance/nonce 조회 불가 → 어댑터 생성 시 `ArchiveMode` 옵션으로 선언
- archive 아니면 `CapBalanceAtBlock`, `CapNonceAtBlock` false 반환

## 어댑터 2 — `adapters/blockscout/`

**대상**: Blockscout v2 REST. Etherscan-호환 proxy 모듈도 함께 (state/receipts/tx root 등 REST v2엔 없는 필드 보강).

**Endpoint 매핑** (Optimism 예시):
| Capability | Endpoint |
|---|---|
| `CapBlockHash/ParentHash/Timestamp/TxCount/GasUsed/Miner` | `GET /api/v2/blocks/{n}` |
| `CapBlockStateRoot/ReceiptsRoot/TransactionsRoot` | `GET /api?module=proxy&action=eth_getBlockByNumber&tag=0x..&boolean=false` (Etherscan 호환 모듈 fallback) |
| `CapBalanceAtLatest`, `CapTxCountAtLatest` | `GET /api/v2/addresses/{addr}` |
| `CapBalanceAtBlock/NonceAtBlock` | `GET /api?module=proxy&action=eth_getBalance&address=..&tag=0x..` |
| `CapTotalAddressCount/TotalTxCount/TotalContractCount` | `GET /api/v2/stats` |
| `CapERC20TokenCount` | `GET /api/v2/tokens?type=ERC-20` 페이징 합계 (또는 dedicated 엔드포인트) |

**체인별 URL 매핑**:
```go
// adapters/blockscout/chain_map.go
var defaultBaseURL = map[chain.ChainID]string{
    chain.OptimismMainnet: "https://optimism.blockscout.com",
    chain.EthereumMainnet: "https://eth.blockscout.com",
}
// 생성자에서 Option으로 override 가능 (사내 mirror 등)
```

**Rate limit**: 관찰된 600 req / window (2h 추정) — 기본값 5 req/s 보수적 설정.

## 어댑터 3 — `adapters/etherscan/`

**대상**: Etherscan V2 Multichain API. 단일 base URL + `chainid=` 파라미터로 50+ 체인.

**Endpoint 매핑**:
| Capability | Call |
|---|---|
| Block fields (전부) | `GET /v2/api?chainid=N&module=proxy&action=eth_getBlockByNumber&tag=0x..&boolean=false` |
| `CapBalanceAtLatest` | `module=account&action=balance` |
| `CapBalanceAtBlock` | ⚠️ free tier 불가 (`balancehistory`는 PRO 전용) — `Supports()` false |
| `CapNonceAtLatest/AtBlock` | `module=proxy&action=eth_getTransactionCount` |
| 기타 aggregate | 부분 지원 — 구현 후 매트릭스 채움 |

**인증**: `apikey=...` 필수. Options로 주입.
**Rate limit**: free 5 req/s, 100K/day.

## 예시 — `examples/custom-graphql-adapter/`

**목적**: 사용자가 자기 GraphQL indexer를 어떻게 `Source` 포트에 맞게 구현하는지 **구현 패턴** 제시.

**내용**:
- `README.md` — "당신의 indexer를 어댑터로 만드는 법"
- `client.go` — GraphQL 호출 스켈레톤 (graphql client 라이브러리 사용 or 수동 POST JSON)
- `adapter.go` — `Source` 인터페이스 구현
- `schema_example.graphql` — **익명화된 예시 스키마** (실제 내부 스키마와 1:1 매핑 금지, 전형적 explorer 패턴만)
- 단위 테스트

**제약**: 
- 실제 사내 URL / IP / 호스트명 금지
- 실제 내부 스키마 복사 금지 (유사 패턴 예시만)

## Capability 매트릭스

| Capability | `rpc` | `blockscout` | `etherscan` | example `graphql` |
|---|:---:|:---:|:---:|:---:|
| `block.hash` / `parent_hash` / `timestamp` / `miner` / `gas_used` / `tx_count` | ✅ | ✅ | ✅ | ✅ |
| `block.state_root` / `receipts_root` / `transactions_root` | ✅ | ✅ (proxy 모듈 via) | ✅ (proxy 모듈 via) | depends |
| `address.balance_at_latest` | ✅ | ✅ | ✅ | ✅ |
| `address.nonce_at_latest` | ✅ | ✅ | ✅ | depends |
| `address.tx_count_at_latest` | ✅ (nonce 근사) | ✅ | ✅ | ✅ |
| `address.balance_at_block` | ✅ (archive node only) | ✅ (proxy) | ⚠️ PRO | 대부분 ❌ |
| `address.nonce_at_block` | ✅ (archive only) | ✅ (proxy) | ✅ (proxy) | 대부분 ❌ |
| `snapshot.total_addresses` | ❌ | ✅ | ⚠️ | ✅ |
| `snapshot.total_txs` | ❌ | ✅ | ⚠️ | ✅ |
| `snapshot.erc20_token_count` | ❌ | ✅ (페이징 합계) | ⚠️ | ✅ |
| `snapshot.total_contracts` | ❌ | ✅ (stats) | ⚠️ | ✅ |

자세한 필드 매핑은 [docs/research/source-shapes.md](../research/source-shapes.md)에서 갱신 관리.

## 세부 단계

### 3.1 공용 HTTP 베이스
- [ ] 테스트: timeout, 재시도 횟수, backoff 시간, 429 처리, rate limit
- [ ] 구현

### 3.2 `adapters/rpc/`
- [ ] 테스트: httptest 기반 mock RPC 서버 또는 anvil
- [ ] 구현 + Supports() 매핑
- [ ] archive vs non-archive 모드

### 3.3 `adapters/blockscout/`
- [ ] 테스트: 블록/주소/스탯/토큰리스트 각 fixture (**실제 응답을 캡처해 익명화**해 저장)
- [ ] 구현
- [ ] REST v2 + proxy 모듈 hybrid 처리

### 3.4 `adapters/etherscan/`
- [ ] 테스트: fixture
- [ ] 구현
- [ ] API 키 없을 시 명확한 에러

### 3.5 Capability 매트릭스 문서
- [ ] `docs/research/source-shapes.md` 채움
- [ ] 어느 Metric을 3-way / 2-way / 1-way 비교 가능한지 표

### 3.6 Example 커스텀 어댑터
- [ ] `examples/custom-graphql-adapter/` 스켈레톤
- [ ] 익명 스키마 예시
- [ ] README (작성 가이드)

### 3.7 (선택) E2E 스모크
- [ ] `e2e/adapters_smoke_test.go` (`-tags=e2e`)
- [ ] 공개 endpoint에 대해서만 (Blockscout 공개, RPC 공개 엔드포인트)

## 의존 Phase

- Phase 1 (chain 값객체)
- Phase 2 (source 포트)

## 주의

- **API 키 / 시크릿**: `.env`로만. 커밋 금지. `.env.example`에는 placeholder (`your-api-key-here`)
- **rate limit**: 무료 tier 기준으로 보수적 설정
- **raw response 저장**: config flag로 on/off. 기본 off.
- **체인 확장성**: 각 어댑터의 `chain_map.go`에 체인 추가만으로 대응. 코드 분기 최소화.
- **사용자 인덱서 URL은 커밋 안 함**: fixture 파일명·내용에서 사내 호스트명/IP 제거

## 블로커 / 사용자 입력 필요

1. **Etherscan API 키 준비 여부**: 있으면 etherscan 어댑터 우선, 없으면 blockscout 먼저.
2. **사용자의 Optimism RPC 엔드포인트 취향**: 무료 공개 RPC (Optimism 공식, Ankr, drpc 등) 중 선택.

## 참고

- [Etherscan V2 API](https://docs.etherscan.io/etherscan-v2)
- [Blockscout API v2](https://docs.blockscout.com/developer-support/api)
- [go-ethereum ethclient](https://pkg.go.dev/github.com/ethereum/go-ethereum/ethclient)
- [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate)
