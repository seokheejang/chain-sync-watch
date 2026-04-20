# External API Coverage — Research & Resume Notes

데이터 소스별 실제 커버리지·엔드포인트 조사. Phase 3 어댑터 구현 전에 완료해야 할 검증 작업.

**RPC는 사용자가 보유한 full-archive 노드 사용 예정** — 이 문서는 **외부 API 대안**에 집중.

---

## 배경 — 왜 이 문서가 필요한가

### 발견된 제약
- **Etherscan V2 Free tier는 Optimism(10)·Base(8453) 미지원**. "Free API access is not supported for this chain" 명시적 거부.
  - Free 지원: Ethereum(1), Polygon(137), Arbitrum(42161) 등
  - 유료 Standard+($199/mo) 티어에서만 Optimism 커버
- **공개 RPC(publicnode, mainnet.optimism.io) 대부분 `debug_*` whitelist 차단**
- **publicnode는 archive 미지원** ("no historical RPC"), mainnet.optimism.io는 archive 지원 확인

### 결론
- Optimism MVP는 **Etherscan 없이 keyless 3-way** 달성 필요
- **Routescan**이 Etherscan-호환 API를 Optimism free로 제공 → 핵심 대안
- 사용자 요구 지표(ERC-20 per-address, internal tx 등)를 외부 API가 어떻게 커버하는지 체계적 정리 필요

---

## 검증 Tier 분류 (2026-04-20)

정합성 검증은 "진실의 원천(source of truth)"이 무엇이냐에 따라 **3-tier**로 갈린다. tier는 분류일 뿐 어댑터 구현을 3벌 만드는 게 아니다 — 동일 어댑터가 여러 tier를 커버하며, Capability에 tier 태그가 붙는다.

| Tier | 진실의 원천 | 대상 지표 | 커버 방법 | 비교 모드 |
|---|---|---|---|---|
| **A. RPC-canonical** | RPC (archive) | block header · tx · receipt · logs · roots · `balance`/`nonce` at any block · `balanceOf` eth_call | **사용자 archive 노드 전수** | 1 vs 1 (indexer ↔ RPC) |
| **B. Indexer-derived** | 없음 (cross-indexer 비교) | "addr가 보유한 전체 토큰 목록" · "token X 홀더 목록" · chain total_addresses/txs · top-N 쿼리 | **3rd-party indexer 샘플링** | N vs M (indexer ↔ Blockscout ↔ Routescan) |
| **C. Mixed** | RPC + 3rd-party 양쪽 | internal tx (debug_trace) · ERC-20 Transfer 파생 지표 · 특정 토큰 balance at block | RPC 전수 가능하나 비쌈 → 정책 결정 | 1~3 way |

**MVP 운영 전략 (사용자 합의, 2026-04-20)**:
- **Tier A = main**. 모든 finalized 블록 전수. 비용 0 (자체 노드).
- **Tier B = 보조**. rate-limit 예산 안에서 샘플링만. 빠지는 블록 있어도 무방.
- **Tier C = 지표별 결정**. 기본은 3rd-party 빠른 샘플링. 정기 cross-check(주 1회 등) 시에만 RPC 재구성.

"비쌈"의 의미: 금전 비용 아님. **시간/CPU/노드 IO** (예: holdings 재구성 = Transfer 로그 장범위 스캔 + 후보 토큰별 `balanceOf` 호출).

---

## 지표 우선순위 원칙 (Tier B 샘플링 대상)

**기준이 확실하면 채택, latest-only는 후순위로 강등.**

| 쿼리 유형 | 판정 | 비고 |
|---|---|---|
| `at block N` 지원 (blockTag / blockno 파라미터) | ✅ 채택 | RPC proxy 엔드포인트(eth_getBalance, eth_getTransactionCount, eth_call) |
| `startblock`/`endblock` 범위 필터 | ✅ 채택 | Routescan `txlist`, `txlistinternal`, `tokentx` 등 — "범위 내 이벤트" 비교 기준 명확 |
| 응답 메타에 **reflected block** 필드 노출 (`block_number_balance_updated_at` 등) | ✅ 조건부 채택 | tolerance window 안에 들어오는 샘플만 유효, 벗어나면 discard |
| latest only · 메타 없음 | ⏬ **후순위로 미룸** | anchor 기준 검증 불가. "관찰(observe)" 카테고리로만 활용 |

이 원칙으로 지표 선택이 정리된다. 검증 가능한 자원에 집중 투자.

---

## Tier B Anchor 전략 (at-block 미지원 API 대처)

**문제**: Free tier 대부분의 cumulative 엔드포인트는 latest only (`addresstokenbalance`, `stats`, `token-balances` 등). historical at-block 조회 불가.

**해법 — finalized anchor + 응답 메타 사후 대조**:

1. verification run 시작 시점에 **`finalized` block을 anchor로 고정** (RPC `eth_getBlockByNumber("finalized")`)
2. indexer 쪽은 해당 blockNumber 기준 스냅샷 조회
3. 3rd-party는 latest 호출
4. 3rd-party 응답에 **reflected-block 메타**가 있으면:
   - tolerance: `reflected_block ∈ [anchor - tol_back, anchor + tol_fwd]` 범위면 유효
   - 범위 밖이면 해당 샘플 **discard** (false positive 방지)
5. 메타 없으면 해당 소스에서 해당 지표는 **검증 대상에서 제외**하거나 "관찰만" 처리

**tolerance 기본값 (MVP 제안)**:
- `tol_back = 0` (과거 상태 역반영은 허용 안 함)
- `tol_fwd = 64 블록` (≈ 2분 / Optimism 2s block time)

**reflected-block 메타 확인 상태 (2026-04-20 curl 실측)**:
- Blockscout `GET /addresses/{addr}` → ✅ `block_number_balance_updated_at` (native coin_balance 기준)
- Blockscout `GET /addresses/{addr}/token-balances` → ❌ per-item 필드 없음. Workaround: `/addresses/{addr}` 선행 호출로 inferred reflected_block 획득 (인덱서 원자성 가정, API 보증 아님)
- Blockscout `GET /addresses/{addr}/internal-transactions` → ✅ 각 item에 `block_number` + `timestamp` (event 기준 블록)
- Blockscout `/stats` → 응답 timestamp만, 블록 메타 없음 → **관찰 전용** (자동 judgement 없음)
- Routescan `account/balance`, `tokenbalance`, `addresstokenbalance` → ❌ 메타 없음 → anchor window **불가**, `ReflectedBlock = nil`로 표기
- Routescan `account/balancehistory` → ✅ blockno 파라미터 자체가 anchor (요청 시점 고정) — Tier A/C fallback 경로로 사용 가능

**finalized 정의** (Optimism L2):
- L2 `finalized` tag = L1에 배치 제출·challenge window 통과한 블록. 7일 challenge window (Fault Proof 이전) or 짧아진 window (Fault Proof 이후).
- MVP 단순화: RPC `finalized` tag 그대로 사용. 추가 reorg safety 계산 안 함.

---

## L2 특이필드 백로그 (deferred)

Optimism·L2 고유 필드. **MVP 대상 아님**, post-MVP 확장 시점에 다룸.

| 필드 | 원천 | 비고 |
|---|---|---|
| `l1GasUsed` / `l1Fee` / `l1FeeScalar` | Optimism RPC tx receipt 확장 필드 | indexer가 저장하는지 확인 필요 |
| deposit tx flag (L1→L2 deposit) | `type=0x7e` transaction | 일반 tx와 구분 처리 필요 |
| `l1BlockNumber` (해당 L2 블록이 참조한 L1 블록) | 각 L2 블록 header | bridge·L1 이벤트 앵커에 필요 |
| sequencer batch index | L1 batch submit 트랜잭션 | post-MVP |

→ Capability enum에 추가 시점 결정은 사용자가 Optimism L2 특이 지표 검증 필요 시점.

---

## 대상 지표 — "우리가 비교하고 싶은 것"

Phase 2 기존 Capability + 사용자 요구 추가 반영:

| 지표 카테고리 | 지표 | Phase 2 있음? |
|---|---|:---:|
| Block 불변 | hash, parent_hash, timestamp, tx_count, gas_used, state/receipts/transactions_root, miner | ✅ |
| Address latest | balance, nonce, tx_count | ✅ |
| Address at block | balance_at_block, nonce_at_block (archive 필요) | ✅ |
| Snapshot | total_addresses, total_txs, erc20_token_count, total_contracts | ✅ |
| **Address ERC-20 (신규)** | **ERC-20 balance of specific token**, **ERC-20 holdings (전체 토큰 목록)** | ❌ 추가 필요 |
| **Trace (신규)** | **internal tx by block**, **internal tx by tx hash** (debug_trace 대체) | ❌ 추가 필요 |

---

## 실측 커버리지 매트릭스

2026-04-20 curl 검증 후 confirmed 표기. 세부 헤더·응답 스키마는 하위 "Open Questions 우선순위 A" 체크리스트 참조.

| 지표 | User RPC (archive) | Blockscout v2 REST | Routescan (Etherscan-compat) | Etherscan V2 (paid req'd for OP) | Alchemy (free tier) |
|---|:---:|:---:|:---:|:---:|:---:|
| block.hash / parent_hash / timestamp / tx_count | ✅ eth_getBlockByNumber | ✅ `/api/v2/blocks/{n}` | ✅ proxy module | ✅ proxy | ✅ |
| block.state_root / receipts_root / transactions_root | ✅ | 🔁 proxy module (REST v2엔 없음) | 🔁 proxy | ✅ | ✅ |
| block.miner / gas_used | ✅ | ✅ REST v2 | ✅ | ✅ | ✅ |
| address.balance_at_latest | ✅ eth_getBalance | ✅ `/addresses/{addr}.coin_balance` | ✅ `account/balance` | ✅ (ETH 체인) | ✅ |
| address.nonce_at_latest | ✅ eth_getTransactionCount | ✅ REST v2 | ✅ proxy | ✅ | ✅ |
| address.balance_at_block | ✅ (archive) | 🔁 proxy | 🔁 proxy | ⚠️ PRO | ✅ |
| address.nonce_at_block | ✅ (archive) | 🔁 proxy | 🔁 proxy | ✅ | ✅ |
| snapshot.total_addresses | ❌ | ✅ `/stats.total_addresses` | ⚠️ 제한적 | ⚠️ | ⚠️ |
| snapshot.total_txs | ❌ | ✅ `/stats.total_transactions` | ⚠️ | ⚠️ | ⚠️ |
| snapshot.total_blocks | ❌ | ✅ `/stats.total_blocks` | ⚠️ | ⚠️ | ⚠️ |
| snapshot.erc20_token_count | ❌ | ✅ `/tokens?type=ERC-20` 페이징 | ⚠️ 확인 필요 | ⚠️ | ⚠️ |
| **address.erc20_balance (특정 토큰)** | ✅ eth_call balanceOf | ✅ REST v2 `/token-balances` 필터 | ✅ `account/tokenbalance` | ✅ | ✅ |
| **address.erc20_holdings (전체 목록)** | ⚠️ 복잡 (이벤트 스캔 필요) | ✅ `/addresses/{addr}/token-balances` | ✅ `account/addresstokenbalance` (이름·심볼·소수점 포함) | ✅ | ✅ |
| **trace.internal_tx_by_tx** | ✅ debug_traceTransaction | ✅ `/transactions/{hash}/internal-transactions` | ✅ `account/txlistinternal` | ⚠️ | ✅ |
| **trace.internal_tx_by_block** | ✅ debug_traceBlockByNumber | ⚠️ 확인 필요 | ✅ `account/txlistinternal` (블록 범위) | ⚠️ | ✅ |

**범례**: ✅ 직접 지원 · 🔁 동일 소스 내 다른 엔드포인트로 보강 · ⚠️ 조건부/불확실 · ❌ 원천 불가

---

## 소스별 엔드포인트 레퍼런스

### Blockscout v2 (keyless)

배포 URL: `https://optimism.blockscout.com` (리다이렉트 → `https://explorer.optimism.io`)

공식 문서:
- https://docs.blockscout.com/devs/apis
- https://docs.blockscout.com/devs/apis/rest (REST v2 — 권장)
- https://docs.blockscout.com/devs/apis/rpc (Etherscan-호환 V1 스타일)
- Swagger UI: https://optimism.blockscout.com/api-docs

#### REST v2 주요 엔드포인트

| 용도 | Method + Path | 응답 주요 필드 |
|---|---|---|
| 체인 통계 | `GET /api/v2/stats` | total_addresses, total_transactions, total_blocks, gas_used_today, average_block_time, coin_price |
| 블록 상세 | `GET /api/v2/blocks/{number_or_hash}` | hash, parent_hash, timestamp, transactions_count, gas_used, miner, size, total_difficulty (state_root 등 roots 없음) |
| 블록 목록 | `GET /api/v2/blocks?type=block` | items[], next_page_params |
| 주소 상세 | `GET /api/v2/addresses/{addr}` | coin_balance, block_number_balance_updated_at, is_contract, has_tokens, has_logs, has_token_transfers, implementations, token (if contract) |
| 주소의 **ERC-20 holdings** | `GET /api/v2/addresses/{addr}/token-balances` | Array: [{token, token_id, token_instance, value}] — 확인됨 248개 반환 |
| 주소의 tx 목록 | `GET /api/v2/addresses/{addr}/transactions` | items[], next_page_params |
| 주소의 internal tx | `GET /api/v2/addresses/{addr}/internal-transactions` | ✅ 존재 확인. items: {block_number, block_index, transaction_hash, from, to, value, type, gas_limit, error, success, timestamp, index, transaction_index}, `next_page_params`로 페이징 |
| 트랜잭션 상세 | `GET /api/v2/transactions/{hash}` | hash, block_number, from, to, value, gas_used, status, method |
| 트랜잭션의 internal tx | `GET /api/v2/transactions/{hash}/internal-transactions` | items[] — 트랜잭션별 internal call |
| ERC-20 토큰 목록 | `GET /api/v2/tokens?type=ERC-20` | items[], next_page_params (페이징 total 확인 가능) |
| 토큰 상세 | `GET /api/v2/tokens/{contract}` | name, symbol, total_supply, holders, type |
| 토큰 홀더 | `GET /api/v2/tokens/{contract}/holders` | items[], next_page_params |

#### Etherscan-compat proxy fallback

`GET /api?module=proxy&action=<method>&<params>` — state_root 등 REST v2가 노출 안 하는 raw RPC 필드 조회용:
- `action=eth_getBlockByNumber&tag=0x..&boolean=false` — full 블록 헤더 (all roots 포함)
- `action=eth_getBalance&address=..&tag=0x..` — balance at block

#### 관찰된 rate limit (2026-04-20 curl 실측 갱신)
- `x-ratelimit-limit: 600`
- `x-ratelimit-reset`: **ms 단위** 잔여. 연속 호출 시 3.3초 wall-clock에 reset 값이 3332ms 감소 관찰 → **window ≈ 60초**
- 실측 한도: **10 req/s sustained** (600 / 60s). 기존 2~3h+ 추정은 오류 (reset unit 오해)
- `access-control-expose-headers`에 `bypass-429-option, x-ratelimit-reset, x-ratelimit-limit, x-ratelimit-remaining, api-v2-temp-token`
- `bypass-429-option: temporary_token` 메커니즘 활성 — 실제 토큰은 429 히트 시점에 `api-v2-temp-token` 헤더로 발급될 것으로 추정, 공식 문서 확인 또는 어댑터 구현 시 실험 필요

---

### Routescan (Etherscan-compat, keyless)

배포 URL: `https://api.routescan.io/v2/network/mainnet/evm/{chainid}/etherscan/api`

공식 문서:
- https://routescan.io/documentation
- https://routescan.io/documentation/api/etherscan-like/accounts

#### 주요 엔드포인트 (쿼리 파라미터)

| 용도 | Query |
|---|---|
| 최신 블록 번호 | `?module=proxy&action=eth_blockNumber` |
| 블록 상세 | `?module=proxy&action=eth_getBlockByNumber&tag=0x..&boolean=false` |
| balance | `?module=account&action=balance&address=..&tag=latest` |
| balance at block | `?module=account&action=balancehistory&address=..&blockno=N` ✅ Optimism free 동작 확인 (2026-04-20) |
| 다수 balance | `?module=account&action=balancemulti&address=A,B,C&tag=latest` |
| tx 목록 | `?module=account&action=txlist&address=..&startblock=..&endblock=..` |
| **internal tx (by address)** | `?module=account&action=txlistinternal&address=..` |
| **internal tx (by tx hash)** | `?module=account&action=txlistinternal&txhash=..` |
| **internal tx (by block range)** | `?module=account&action=txlistinternal&startblock=..&endblock=..` |
| **ERC-20 특정 토큰 balance** | `?module=account&action=tokenbalance&contractaddress=..&address=..&tag=latest` |
| **ERC-20 holdings (전체 목록)** | `?module=account&action=addresstokenbalance&address=..&page=1&offset=100` — 확인됨 |
| ERC-20 transfer 목록 | `?module=account&action=tokentx&address=..` |
| ERC-721 transfer 목록 | `?module=account&action=tokennfttx&address=..` |
| gas oracle | `?module=gastracker&action=gasoracle` |
| ETH supply | `?module=stats&action=ethsupply` |
| total_addresses / total_txs | ❌ **action 없음 (2026-04-20 확인)** — `stats` 모듈은 `ethsupply`/`tokensupply`/`ethprice`만. chain-wide stats는 Blockscout 전담 |
| 토큰 total supply | `?module=stats&action=tokensupply&contractaddress=..` |

#### 관찰된 rate limit (2026-04-20 curl 실측)
- 응답 헤더: `x-ratelimit-rpm-limit: 120` / `x-ratelimit-rpd-limit: 10000`
- 즉 **분당 120 (= 2 req/s), 일 10,000** — 공식 "5 req/s, 100k/day" 문구와 차이 있음 (정책 다운그레이드 또는 IP별 기본 티어?)
- 키·가입 불필요
- Conservative 어댑터 설정 권장: **2 req/s** (observed 기준), 일 10k

#### 알려진 제약 (2026-04-20 확정)
- **chain stats**: `stats` 모듈에 chain-wide 카운터 action 없음 — **Blockscout이 `snapshot.total_*` 카테고리의 유일 공급자**
- **응답 reflected-block 메타 0**: `account/balance`, `tokenbalance`, `addresstokenbalance` 모두 값(result 문자열)만 반환. Tier B anchor window 불가 → 해당 Capability에서 Routescan은 `Supports()=false` 또는 "관찰 전용"
- **스팸 토큰 필터링 없음**: `addresstokenbalance` 응답에 airdrop 스팸/피싱 토큰(이름·심볼에 URL/claim 문구 포함) 그대로 포함. 저장·비교 시 반드시 cross-check(Blockscout `is_scam`/`reputation`) 또는 allowlist 필터 적용. **피싱 URL은 fixture·log·memory에 저장 금지**.

---

### Etherscan V2 Multichain (API key 필요, Optimism은 paid)

Base: `https://api.etherscan.io/v2/api`
Auth: `&apikey=XXX` 쿼리 파라미터

공식 문서:
- https://docs.etherscan.io/etherscan-v2
- https://docs.etherscan.io/support/rate-limits

#### Free tier 커버
- Ethereum(1), Polygon(137), Arbitrum(42161) 등 — **Optimism / Base 제외**
- 5 req/s, 100k/day

#### 지원 action은 Routescan과 거의 동일한 Etherscan-compat 스키마

→ 우리 프로젝트 관점에서는 **Routescan의 "paid chains + 다른 chains" 판 쌍둥이**. 같은 어댑터 로직에 URL/키만 다름.

→ **옵션 B의 `adapters/internal/ethscan/` 공통 client 재사용 완벽 적합**.

---

### Alchemy (API key 필요, 자유도 높음)

공식 문서:
- https://www.alchemy.com/pricing
- https://docs.alchemy.com/reference/optimism-api-quickstart

#### Free tier
- **30M Compute Units / month** (CU) — 단순 RPC 요청 기준 약 1.8M/월 (~60k/day 평균)
- Optimism, Base, Arbitrum, Polygon, Ethereum 모두 포함

#### 고유 기능 (Enhanced APIs)
- `alchemy_getTokenBalances(address, [contracts])` — 한 번에 여러 ERC-20 balance
- `alchemy_getTokensForOwner(address)` — 보유 토큰 전체 메타데이터 포함
- `alchemy_getAssetTransfers(params)` — tx history 필터링 강력
- `debug_*` 전 라인업 — **debug_traceTransaction/Block 지원 확인** (paid tier에서)
- `trace_*` (OpenEthereum-style) 일부 지원

#### 상태
- Phase 3에서 **opt-in adapter**로 구현 가치 있음
- 사용자가 키 있으면 4-way 비교 또는 debug_trace 전용 소스로

---

### Covalent (Unified API, 키 필요, free 100k/month)

공식 문서:
- https://www.covalenthq.com/docs/api/
- https://goldrush.dev/platform

#### Free tier (goldrush.dev로 리브랜딩됨)
- 100,000 req/month, 제한된 endpoint set

#### 고유 기능
- 100+ 체인 통합 unified response
- `/v1/{chainid}/address/{addr}/balances_v2/` — 모든 토큰(ERC-20/721/1155) 한 번에
- `/v1/{chainid}/block/{n}/` — 블록 상세
- `/v1/{chainid}/address/{addr}/transactions_v3/` — tx history
- 체인별 pricing·holders 포함

#### 조사 TODO
- Free tier에 Optimism 포함되는지 확인 (최근 정책 변경 가능)
- 어떤 지표가 free tier에서 접근 가능한지 세부

---

### Moralis (키 필요, free 40k/day)

공식 문서:
- https://docs.moralis.io/

#### Free tier
- 40,000 req/day
- Optimism 포함 지원

#### 조사 TODO
- Optimism 지원 정도 (전체 endpoint vs 제한)
- debug_trace 지원 여부

---

### 기타 조사 대상 (우선순위 낮음)

| 소스 | 특징 | 조사 필요성 |
|---|---|---|
| QuickNode | Marketplace + add-on APIs, free 10M req/month | 중간 |
| BlockPI | 공개 RPC + API, free tier | 낮음 |
| OnFinality | Substrate 중심, EVM도 일부 | 낮음 |
| The Graph (subgraph) | GraphQL, 주문형 인덱싱 | Phase post-MVP |
| Dune | SQL, 집계용 | 대시보드 관점만 |

---

## RPC (사용자 archive 노드)

**사용자가 다음 세션에 URL 제공 예정.** 이 문서에선 다루지 않음.

필수 요구:
- Archive mode (eth_getBalance at any block)
- `debug_*` 활성화 (debug_traceTransaction, debug_traceBlockByNumber)
- WebSocket 가능하면 좋음 (실시간 streaming 확장 대비)

---

## Open Questions — 다음 세션 조사할 것

### 우선순위 A (Phase 3 전 반드시)

**2026-04-20 curl 검증 완료**. 하위 체크리스트는 완료 + 결과 요약.

- [x] **Routescan 체인 통계** → ❌ **action 없음**. `stats` 모듈은 `ethsupply`/`tokensupply`/`ethprice`만 존재. `chainsupply`·`nodecount`는 "Missing Or invalid Action". **결론**: `total_addresses`/`total_transactions`/`total_blocks`는 **Blockscout이 유일 공급자**.
- [x] **Routescan `balancehistory` (archive)** → ✅ **Optimism free에서 동작**. `?module=account&action=balancehistory&address=..&blockno=N` → native balance 반환. Etherscan free는 PRO 전용이지만 Routescan은 open. **Tier A/C fallback 가치 높음** (우리 archive RPC 없을 때 보조 경로).
- [x] **Blockscout `/addresses/{addr}/internal-transactions`** → ✅ **존재 확인**. 응답 스키마: `{items: [...], next_page_params}`. per-item: `block_number`, `block_index`, `transaction_hash`, `from`, `to`, `value`, `type` (call/delegatecall/create/...), `gas_limit`, `error`, `success`, `timestamp`, `index`, `transaction_index`, `created_contract`. `next_page_params`: `{index, block_number, transaction_index, items_count}`.
- [x] **Blockscout rate limit window**:
  - `x-ratelimit-limit: 600`
  - `x-ratelimit-reset`: **ms 단위** 잔여 (3.3초 wall-clock에 reset 값이 3332ms 감소 관찰)
  - **window ≈ 60 초**, 즉 실측 **10 req/s sustained** (기존 5 req/s 추정의 2배 — conservative default 유지 권장)
  - `bypass-429-option: temporary_token` + `api-v2-temp-token` 헤더 **expose** 확인. 실제 토큰은 429 히트 시점에 발급될 것으로 추정 — 취득 절차는 별도 실험 필요(Phase 3 어댑터 구현 시)
- [x] **reflected-block 메타**:
  - **Blockscout `/addresses/{addr}`** → ✅ `block_number_balance_updated_at` 필드 존재 (native coin_balance 반영 블록)
  - **Blockscout `/addresses/{addr}/token-balances`** → ❌ **per-item 필드 없음**. items = `{token, token_id, token_instance, value}`만. **Workaround**: 같은 주소 `/addresses/{addr}` 선행 호출 → `block_number_balance_updated_at`를 proxy reflected_block으로 사용 (Blockscout 인덱서 원자성 가정 — **API 보증 아님**, "inferred" 표기)
  - **Routescan `account/balance`, `tokenbalance`, `addresstokenbalance`** → ❌ **전부 값(result 문자열)만**, 블록 메타 없음. **Tier B anchor window 전략 불가** → Routescan 대응 Capability는 `Supports() = false` 또는 `ReflectedBlock = nil` + "관찰 전용"
- [x] **부수 발견 — 스팸 토큰 필터링**:
  - Routescan `addresstokenbalance`: 응답에 airdrop 스팸/피싱 토큰 필터링 없음 (이름·심볼에 외부 URL·claim 문구 포함된 가짜 토큰들 그대로 반환). 피싱 URL을 fixture·로그에 저장 금지.
  - Blockscout: 주소·토큰에 `reputation` + `is_scam` 필드 제공 → 자체 필터 사용 가능.
  - 검증 정책: `is_scam=true` 또는 `reputation != "ok"` 토큰은 비교 대상에서 **제외** 권장 (Phase 4 Judgement 레이어). 스팸으로 인한 cross-source 자동 false positive 방지.
- [x] **부수 발견 — Blockscout token metadata**: `/addresses/{addr}` 응답에 `has_tokens`/`has_token_transfers`/`has_logs`/`has_beacon_chain_withdrawals` boolean flag 제공 → capability 부분 pre-check 가능. `token.holders_count` 필드도 snapshot 검증 보조 지표로 활용 가능.

### 우선순위 B (가능하면)

- [ ] Covalent/goldrush Free tier에 Optimism 확실히 포함되는지
- [ ] Alchemy Optimism free tier에서 debug_* 실제 동작 (일부 enhanced method는 paid)
- [ ] Moralis Optimism 커버 정도
- [ ] Bypass token 취득 후 Blockscout rate limit 어디까지 올라가는지

### 우선순위 C (post-MVP)

- [ ] Self-hosted Blockscout 구축 비용·난이도 평가
- [ ] The Graph subgraph 자체 배포 검토 (커스텀 지표용)

---

## Phase 2 추가 필요 항목 (요약)

본 조사 결과 Phase 2의 Capability/Query/Result에 다음 추가 필요:

```go
// === Capability 추가 ===

// Per-address ERC-20
CapERC20BalanceAtLatest   Capability = "address.erc20_balance_at_latest"
CapERC20HoldingsAtLatest  Capability = "address.erc20_holdings_at_latest"

// Internal transactions (debug_trace 대체 레이어)
CapInternalTxByBlock Capability = "trace.internal_tx_by_block"
CapInternalTxByTx    Capability = "trace.internal_tx_by_tx"
```

```go
// === Query/Result 추가 ===

type ERC20BalanceQuery struct {
    Address      chain.Address
    TokenAddress chain.Address
}
type ERC20BalanceResult struct {
    Balance  *big.Int
    Decimals uint8
    SourceID SourceID
    // ...metadata
}

type ERC20HoldingsQuery struct { Address chain.Address }
type ERC20HoldingsResult struct {
    Tokens   []TokenHolding
    SourceID SourceID
}
type TokenHolding struct {
    Contract chain.Address
    Name     string
    Symbol   string
    Decimals uint8
    Balance  *big.Int
}

type InternalTxByBlockQuery struct { Block chain.BlockNumber }
type InternalTxByTxQuery    struct { TxHash chain.Hash32 }
type InternalTxResult struct {
    Traces   []InternalTx
    SourceID SourceID
}
type InternalTx struct {
    From, To chain.Address
    Value    *big.Int
    GasUsed  uint64
    CallType string  // "call", "delegatecall", "create", etc.
    Error    string
}
```

→ **Phase 2C**로 분리해서 진행 권장 (Phase 3 전).

---

## Phase 3 수정 제안 — 어댑터 구성

Keyless 3-way 기본 + opt-in 확장:

```
adapters/
  internal/
    httpx/          HTTP 공용 base (timeout/retry/rate limit/logging)
    ethscan/        Etherscan-compat 공용 client (Routescan + Etherscan + Blockscout proxy)
  rpc/              사용자 archive 노드 (debug_trace + archive 지원)
  blockscout/       REST v2 primary + ethscan fallback
  routescan/        ethscan + Routescan URL + Optimism free 지원
  etherscan/        ethscan + Etherscan V2 + chainid + apikey (opt-in)
  alchemy/          opt-in, debug_* + enhanced APIs
```

### 기본 활성화 (OSS 공개 안전)

```yaml
adapters:
  rpc:        { enabled: true }    # 사용자 archive URL (로컬 override)
  blockscout: { enabled: true }
  routescan:  { enabled: true }
  etherscan:  { enabled: false }   # 키+ paid chain일 때만
  alchemy:    { enabled: false }   # opt-in
```

**Optimism 실무 신뢰도 순서**: user-RPC (archive) > Routescan (indexer #1) > Blockscout (indexer #2).

---

## 다음 세션 재개 체크리스트

세션 재개 시 이 순서:

1. [ ] 사용자가 archive RPC URL 제공 → `.env`의 `CSW_ADAPTERS__RPC__ENDPOINTS__10` 에 세팅
2. [ ] 위 "Open Questions 우선순위 A" 4항목 실제 curl로 최종 확인
3. [ ] 확인 결과 이 문서(`docs/research/external-api-coverage.md`) 업데이트
4. [ ] Phase 2 확장 결정 (2C 별도 or Phase 3 내 포함)
5. [ ] 결정에 따라 코드 작업 시작:
   - Phase 2C: Capability/Query/Result 추가 + fake 확장
   - Phase 3: 어댑터 구현 (3A~3G 순)

---

## 참고 자료 (공식 링크 모음)

### Blockscout
- 문서: https://docs.blockscout.com/devs/apis
- REST v2: https://docs.blockscout.com/devs/apis/rest
- Optimism Swagger: https://optimism.blockscout.com/api-docs

### Routescan
- 문서: https://routescan.io/documentation
- Etherscan-like: https://routescan.io/documentation/api/etherscan-like/accounts
- Snowtrace (같은 플랫폼): https://snowtrace.io/documentation

### Etherscan V2
- 문서: https://docs.etherscan.io/etherscan-v2
- Rate limits: https://docs.etherscan.io/support/rate-limits
- 가격: https://etherscan.io/apis

### Alchemy
- 문서: https://docs.alchemy.com/
- Optimism: https://docs.alchemy.com/reference/optimism-api-quickstart
- 가격: https://www.alchemy.com/pricing

### Covalent / GoldRush
- 문서: https://www.covalenthq.com/docs/api/
- 신 플랫폼: https://goldrush.dev/platform

### JSON-RPC 표준
- Ethereum JSON-RPC spec: https://ethereum.org/en/developers/docs/apis/json-rpc/
- Optimism 추가 methods: https://docs.optimism.io/builders/node-operators/json-rpc

---

**문서 상태**: Phase 3 시작 **전** 조사 단계. 내용은 2026-04-18 시점의 관측 + 공식 문서 인용. 재개 시 "Open Questions" 확인부터.

---

## 확정 우선순위 (2026-04-18)

사용자 명시:
1. **기본 전략 = 키 없는 공개 RPC + Blockscout + Routescan**
2. **Etherscan은 후순위** — Optimism 미커버 이슈 확정, MVP 기본 bundle 제외
3. **RPC는 사용자 자체 archive 노드** (외부 공개 RPC 아님)
4. Alchemy/Covalent/Moralis는 **post-MVP** opt-in

따라서 Phase 3 구현 집중 순서:
- 1순위: `adapters/rpc/`, `adapters/blockscout/`, `adapters/routescan/` (3-way keyless)
- 2순위: `adapters/etherscan/` (ETH-mainnet 확장 시점)
- 3순위: `adapters/alchemy/` 등 opt-in 어댑터

---

## 추가 합의 (2026-04-20)

1. **3-tier 모델 채택**. Tier A(RPC 전수) main + Tier B(3rd-party 샘플링) 보조 + Tier C(지표별 결정).
2. **anchor block 전략**: finalized 고정 + 응답 메타 기반 사후 대조 + tolerance window(`tol_back=0`, `tol_fwd=64` 기본).
3. **지표 우선순위 원칙**: `at block N` 또는 `startblock/endblock` 같이 **기준이 확실한 쿼리** 우선. **latest-only**는 후순위로 강등하거나 "관찰" 전용.
4. **샘플링 4-stratum**: known addresses + top-N + random + recently-active.
5. **rate-limit 예산 엔진**: Phase 7 scheduler에 필수 port로 추가 (Tier B 전용).
6. **reorg safety**: MVP는 `finalized` tag만 사용, 추가 계산 안 함.
7. **카테고리 재분류 (`Snapshot` 분할 등)**: 필요 시점에 결정, 지금은 보류.
8. **L2 특이필드**: backlog 기록, MVP 범위 아님.
9. **indexer 측 Capability**: 특정 필드 누락 허용("minor 정보 indexer에도 빠질 수 있음"). 구현 시 확인·문서화만.

구현 착수 Gate: 위 "우선순위 A" Open Q 5항목 curl 검증 → research doc 업데이트 → Phase 2C 진행.
