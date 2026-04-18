# Source Shapes — 소스별 데이터 형상 매트릭스

각 소스 유형이 어떤 지표(필드)를 어떤 endpoint로 노출하는지 정리. 어댑터 구현·Capability 매트릭스의 근거 문서.

**⚠️ 원칙**: 이 문서는 공개 OSS의 일부가 될 수 있음. 실제 사내 endpoint·호스트명·IP·API 키·스키마 세부는 포함 금지. **공개 API 형상**과 **일반화된 패턴**만 기록.

## 소스 유형

| 유형 | 예시 | 어댑터 패키지 |
|---|---|---|
| JSON-RPC 노드 | Optimism 공식 RPC, Alchemy, Ankr, drpc | `adapters/rpc/` |
| Blockscout v2 REST (+ Etherscan-compat proxy) | `optimism.blockscout.com`, `eth.blockscout.com` | `adapters/blockscout/` |
| Etherscan V2 Multichain | `api.etherscan.io/v2` | `adapters/etherscan/` |
| Custom GraphQL indexer | 사용자 자체 구축 (예시: `examples/custom-graphql-adapter/`) | 사용자 구현 |

## 필드 지원 매트릭스

**범례**
- ✅ 직접 지원
- 🔁 동일 소스 내 다른 endpoint(e.g. proxy 모듈)로 보강
- ⚠️ 조건부 (archive node, PRO tier 등)
- ❌ 미지원

| Capability | RPC | Blockscout v2 | Etherscan V2 | Custom GraphQL (예시) |
|---|:---:|:---:|:---:|:---:|
| `block.hash` | ✅ | ✅ | ✅ | ✅ |
| `block.parent_hash` | ✅ | ✅ | ✅ | ✅ |
| `block.timestamp` | ✅ | ✅ | ✅ | ✅ |
| `block.tx_count` | ✅ | ✅ | ✅ | ✅ |
| `block.gas_used` | ✅ | ✅ | ✅ | ✅ |
| `block.miner` | ✅ | ✅ | ✅ | depends |
| `block.state_root` | ✅ | 🔁 proxy 모듈 | ✅ | depends |
| `block.receipts_root` | ✅ | 🔁 proxy 모듈 | ✅ | depends |
| `block.transactions_root` | ✅ | 🔁 proxy 모듈 | ✅ | depends |
| `address.balance_at_latest` | ✅ | ✅ | ✅ | ✅ |
| `address.nonce_at_latest` | ✅ | ✅ | ✅ | depends |
| `address.tx_count_at_latest` | ✅ (≈nonce) | ✅ | ✅ | ✅ |
| `address.balance_at_block` | ⚠️ archive only | 🔁 proxy 모듈 | ⚠️ PRO tier | 대부분 ❌ |
| `address.nonce_at_block` | ⚠️ archive only | 🔁 proxy 모듈 | ✅ | 대부분 ❌ |
| `snapshot.total_addresses` | ❌ | ✅ `stats` | ⚠️ 제한적 | depends |
| `snapshot.total_txs` | ❌ | ✅ `stats` | ⚠️ | depends |
| `snapshot.total_contracts` | ❌ | ✅ `stats` | ⚠️ | depends |
| `snapshot.erc20_token_count` | ❌ | ✅ `tokens?type=ERC-20` (페이징 합계) | ⚠️ | depends |

## Endpoint 매핑 요약

### RPC 노드 (JSON-RPC)

| Capability | RPC method | 비고 |
|---|---|---|
| block fields 전체 | `eth_getBlockByNumber` (`boolean=false`로 헤더만) | |
| `balance_at_latest/block` | `eth_getBalance(addr, tag)` | archive 필요 (block tag 과거) |
| `nonce_at_latest/block` | `eth_getTransactionCount(addr, tag)` | archive 필요 |

### Blockscout v2 REST

| Capability | Endpoint |
|---|---|
| block fields | `GET /api/v2/blocks/{number_or_hash}` |
| block roots (state/receipts/tx) | `GET /api?module=proxy&action=eth_getBlockByNumber&tag=0x..&boolean=false` (Etherscan-compat) |
| address current state | `GET /api/v2/addresses/{address}` — `coin_balance`, `block_number_balance_updated_at` 등 |
| balance/nonce at block | `GET /api?module=proxy&action=eth_getBalance` / `eth_getTransactionCount` |
| chain stats | `GET /api/v2/stats` — `total_addresses`, `total_transactions`, `total_blocks` 등 |
| ERC-20 list | `GET /api/v2/tokens?type=ERC-20&items_count={page_size}` 페이징 |

Rate limit: 관찰 기준 약 600 req / 2h window (Cloudflare bypass-429 token 제공).

### Etherscan V2 Multichain

Base URL: `https://api.etherscan.io/v2/api`
공통 파라미터: `chainid=<N>` (e.g. 10=Optimism), `apikey=<KEY>`

| Capability | 모듈 / 액션 |
|---|---|
| block fields 전체 | `module=proxy&action=eth_getBlockByNumber&tag=0x..&boolean=false` |
| balance latest | `module=account&action=balance&address=..&tag=latest` |
| balance at block | **⚠️ PRO tier 전용 (`balancehistory`)** — 무료 tier에선 `Supports` false |
| nonce at block | `module=proxy&action=eth_getTransactionCount&address=..&tag=0x..` |
| stats (부분) | `module=stats&action=...` (체인별 커버리지 상이) |

Rate limit (free): 5 req/s, 100k/day.

### Custom GraphQL indexer (일반 패턴)

대개 `POST /graphql` 하나로 통일, 단일 query 문서에 필요한 필드 선택 가능. 실제 query 이름·스키마 세부는 구현마다 다르지만, **전형적으로 아래 종류의 엔트리포인트**를 제공:

- **블록 조회 (번호 기준)** — block hash, parent hash, timestamp, tx count, gas used, roots 등
- **Tip/latest block 조회** — 현재 최신 블록 번호
- **주소 현재 상태 조회** — 현재 잔고·nonce·tx count (latest 기준)
- **체인 누적 스탯 조회** — 전체 address 수, 전체 tx 수, 검증 컨트랙트 수 등
- **토큰 목록/상세 조회** — 페이징된 ERC-20/ERC-721 리스트, 특정 컨트랙트 상세

**역사적 조회(balance at block N 등)는 대부분 미지원** — indexer 구현에 따라 다름. `Supports()`에서 false 반환 또는 custom flag.

**주의**: 실제 사용 중인 내부 indexer의 query 이름·필드 구조는 이 문서에 기록하지 않음 (OSS 보안 원칙 — 스키마 노출은 공격면 매핑 리스크). 실 구현은 `private/adapters/<name>/`에서.

## 관측된 소스 간 차이 패턴

동일 시점·동일 체인에서 두 소스를 비교할 때 **카테고리별로 관찰되는 차이의 유형**:

| 지표 카테고리 | 관찰 패턴 |
|---|---|
| Block 불변 지표 (hash, parent hash 등) | 완전 일치. 정규화만 맞추면 100% 동일 |
| Block 숫자 지표 (gas_used, tx_count 등) | 값 동일. 단 **타입 차이 존재** (string vs number, hex vs decimal) |
| Block timestamp | 값 동일. 단 **표기 형식 차이** (ISO8601 `Z` suffix 유무, 소수점 초 자리 등) |
| Snapshot 누적 카운터 (total tx) | 동기화 지연 정도의 미세 차이 (0.01~0.1% 수준) |
| Snapshot 누적 카운터 (total addresses) | **정의 차이로 큰 격차 가능** (10~20%+ 차이 관측됨) — "address" 정의가 소스마다 다르기 때문 (활성 계정 vs 모든 유니크 주소 vs 재활용 포함 여부 등) |

**시사점**:
1. **BlockImmutable 카테고리는 매우 안정** — 값 정규화(string→uint64, timestamp 파싱 통일)만 맞추면 거의 항상 일치
2. **Snapshot 카테고리는 "의미 정의 차이"로 큰 격차 발생** → Judgement 자동 판정 지양, 관찰용 대시보드로만
3. **타입 정규화 레이어 필수**: 각 어댑터가 Result를 반환할 때 `chain/big.Int/time.Time`으로 통일

## 정규화 규칙 (어댑터 공통 책임)

각 어댑터는 아래 규칙으로 외부 응답을 Result로 변환:

| 필드 | 정규화 규칙 |
|---|---|
| 숫자(u64) | 문자열 `"123"` / 헥스 `"0x7b"` / 숫자 `123` 모두 `uint64(123)` |
| 큰 수 (balance) | 문자열·헥스 모두 `*big.Int` |
| 해시 | 32바이트 헥스 → `chain.Hash32` (소문자 `0x` prefix) |
| 주소 | 20바이트 → `chain.Address` (EIP-55 체크섬 저장, 비교 시 byte-level) |
| 타임스탬프 | ISO8601 / unix epoch / RPC 헥스 모두 `time.Time` (UTC) |
| 누락 필드 | `nil`로 둠 (zero value 금지) |

## 참고

- [Etherscan V2 Migration](https://docs.etherscan.io/v2-migration)
- [Etherscan V2 Multichain](https://info.etherscan.com/etherscan-api-v2-multichain/)
- [Blockscout API v2 Docs](https://docs.blockscout.com/developer-support/api)
- [JSON-RPC eth_getBlockByNumber](https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_getblockbynumber)

## 문서 갱신 규칙

- 어댑터 구현하면서 새 발견사항은 이 문서에 즉시 반영
- 매트릭스 변경 시 `docs/plans/phase-03-source-adapters.md` 매트릭스도 동기화
- 실제 사내 스키마·URL·키는 여기 기록 금지
