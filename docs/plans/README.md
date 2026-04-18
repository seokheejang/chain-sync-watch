# chain-sync-watch — Plan Index

Chain indexer의 데이터 정합성/누락을 N-way 비교(복수 소스)로 검증하는 **범용 OSS 도구**.

- **체인(MVP)**: Optimism 메인넷 (추후 멀티체인 확장)
- **아키텍처**: Go + DDD + TDD, `database/sql` 드라이버 패턴 (코어 ↔ 어댑터 분리)
- **스택**: Go 1.24 / chi + huma (OpenAPI 3.1) / Redis 7.4 + asynq / Postgres 17 + gorm / Next.js 15 + shadcn/ui + TanStack Query (Node 22 LTS, pnpm 10)
- **번들 어댑터**: `adapters/rpc`, `adapters/blockscout`, `adapters/etherscan` (독립 패키지, 선택 import)
- **커스텀 어댑터**: `examples/custom-graphql-adapter/`를 참고해 사용자가 자기 저장소에 구현 (또는 로컬 `private/` 디렉토리에서 개발)
- **샘플링 모드**: 고정 리스트 · latest N · 랜덤 · 등간격 sparse (실시간 streaming은 post-MVP)
- **배포**: 로컬 docker-compose (Phase 10), 프로덕션 K8s Helm chart (Phase 11)

## 아키텍처 개요

```
internal/                    [코어 — 추상만, 구체 어댑터 0]
  chain/                     값객체
  source/                    Source 포트 + 필드 단위 Capability
  verification/ diff/        도메인
  application/               use case + 포트
  infrastructure/            persistence·queue·http

adapters/                    [번들 구현체 — 각자 독립 패키지]
  rpc/ blockscout/ etherscan/

examples/
  custom-graphql-adapter/    [사용자 정의 어댑터 작성 가이드 + 스켈레톤]
```

## Bounded Contexts

| Context | 역할 |
|---|---|
| [chain](../../internal/chain) | 체인 세계 원시 값객체 (BlockNumber, Address, ChainID, TxHash, BlockRange) |
| [source](../../internal/source) | 외부 데이터 소스 **추상** (Source 포트, 필드 단위 Capability, Query/Result) |
| [verification](../../internal/verification) | 검증 세계 (Run, SamplingStrategy, MetricCategory, Schedule) |
| [diff](../../internal/diff) | 불일치 판정 세계 (Discrepancy, Tolerance, Judgement) |

## Metric 카테고리 (지표 분류)

| 카테고리 | 설명 | 비교 정책 |
|---|---|---|
| `BlockImmutable` | 블록 번호로 앵커, 온체인 불변 (hash, roots, timestamp, tx_count 등) | ExactMatch, 불일치 시 Critical |
| `AddressLatest` | 현 시점 address 상태 (balance/nonce at latest) | ExactMatch, Warning |
| `AddressAtBlock` | 과거 블록 시점 address (archive RPC 필요) | ExactMatch, Critical |
| `Snapshot` | 체인 누적량 (total addresses, total txs, erc20 token count) | 자동 판정 없음, 대시보드 관찰용 |

## 🔖 현재 작업 시점 (Checkpoint)

**최종 업데이트**: 2026-04-18
**현재 단계**: **Phase 3 시작 전 — 외부 API 조사 진행 중**

### 완료 (committed)

- Phase 0 Foundations — commit `ac4b50e` + `4eab3cd`
- Phase 1 `chain/` 도메인 (값객체 5종) — commit `498c09b`
- Phase 2 `source/` 포트 + Fake — commit `a8b9b20` + `cfd7549`
- Ralph 셀프 리뷰 + 테스트 fixture 합성화 — commit `725b063` + `f939232`
- CLAUDE.md rule 6 (.env secret 비재출력 규칙) — commit `6d27c4c`

### 진행 중 (uncommitted, WIP)

- `docs/research/external-api-coverage.md` (413줄, staged) — 외부 API 커버리지 체계 조사 문서
- Phase 3 시작 대기 중

### 다음 세션 재개 절차

1. [docs/research/external-api-coverage.md](../research/external-api-coverage.md) **전문 정독** (이번 세션 조사 결과 집대성)
2. **사용자가 자체 full-archive RPC URL 제공** → `.env`의 `CSW_ADAPTERS__RPC__ENDPOINTS__10`에 세팅
3. 위 문서의 **"Open Questions 우선순위 A"** 4항목 curl로 최종 확인:
   - Routescan `stats` 모듈의 chain-wide total_addresses/total_txs action 존재 여부
   - Routescan `balancehistory` (archive) Optimism free 동작 확인
   - Blockscout v2 `address/internal-transactions` 엔드포인트 공식 존재 여부
   - Blockscout rate limit 실제 window + `bypass-429-option: temporary_token` 취득 절차
4. 조사 결과 반영해 research doc 업데이트 + 커밋
5. **Phase 2C** 진행: Capability 4개 추가 (ERC-20 per-address 2개, internal_tx 2개) + 대응 Query/Result + fake 확장
6. **Phase 3A** 시작: `adapters/internal/httpx/` HTTP 공용 base (TDD Red → Green)

### 확정 결정

- **RPC 기본 소스**: 사용자 자체 full-archive 노드 (다음 세션에 URL 전달 예정) — 외부 공개 RPC는 archive 미지원 or debug_* 차단
- **External API 우선순위** (검증 도구 관점):
  1. **Blockscout** (keyless, native REST v2, 풍부한 필드, chain stats 유일 공급자)
  2. **Routescan** (keyless, Etherscan-compat, Optimism free 커버, internal_tx + ERC-20 holdings 강력)
  3. **Etherscan V2** — **후순위 (opt-in only)**. Free가 Optimism 미커버·paid 필요. Ethereum-mainnet 확장 시점에만 활성화 가치 있음.
  4. Alchemy / Covalent / Moralis — post-MVP opt-in
- **기본 OSS 공개 구성**: User-RPC (archive) + Blockscout + Routescan = **keyless 3-way**
- **Phase 3 어댑터 순서**: `httpx` → `rpc` → `internal/ethscan` → `blockscout` → `routescan` → `etherscan` (후순위) → docs/examples

### Open Items — 코드 작업 전 확정 필요

- [ ] Phase 2C (Capability 확장)를 Phase 3 *전*에 완료할지, Phase 3과 병행할지
- [ ] Routescan의 chain stats 실제 action 이름·커버리지 (위 Open Questions A)
- [ ] Bypass-429 token으로 Blockscout rate limit 얼마나 올라가는지

---

## 진행도

| Phase | 제목 | 상태 | 의존 Phase | 문서 |
|---|---|---|---|---|
| 0 | Foundations | ✅ Done | — | [phase-00-foundations.md](./phase-00-foundations.md) |
| 1 | `chain/` 도메인 (ChainID slug/name 매핑) | ✅ Done | 0 | [phase-01-chain-domain.md](./phase-01-chain-domain.md) |
| 2 | `source/` 포트 (필드 단위 Capability, 코어 추상) | ✅ Done | 1 | [phase-02-source-ports.md](./phase-02-source-ports.md) |
| 2C | Capability 확장 (ERC-20 per-address + internal_tx) | 🟡 Proposed | 2 | 문서화 예정 (현재는 research doc에만 기술) |
| 3 | 번들 어댑터 (`rpc`, `blockscout`, **`routescan`**, `etherscan` 후순위) + 커스텀 예시 | ⛔ Blocked (외부 API 조사 중) | 2 / 2C | [phase-03-source-adapters.md](./phase-03-source-adapters.md) |
| 4 | `verification/` + `diff/` 도메인 (Metric 카테고리) | ⬜ Not started | 1, 2 | [phase-04-verification-diff-domain.md](./phase-04-verification-diff-domain.md) |
| 5 | Application (use case) | ⬜ Not started | 2, 4 | [phase-05-application.md](./phase-05-application.md) |
| 6 | Persistence (Postgres + gorm) | ⬜ Not started | 4, 5 | [phase-06-persistence.md](./phase-06-persistence.md) |
| 7 | Queue / Scheduler (Redis + asynq) | ⬜ Not started | 5 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 8 | HTTP API (chi + huma) | ⬜ Not started | 5, 6 | [phase-08-http-api.md](./phase-08-http-api.md) |
| 9 | Frontend (Next.js 15) | ⬜ Not started | 8 | [phase-09-frontend.md](./phase-09-frontend.md) |
| 10 | Integration / Observability / Local Deploy | ⬜ Not started | 3, 6, 7, 8, 9 | [phase-10-integration-observability.md](./phase-10-integration-observability.md) |
| 11 | Kubernetes 배포 (Helm) | ⬜ Not started | 10 | [phase-11-kubernetes-deploy.md](./phase-11-kubernetes-deploy.md) |

### 상태 아이콘

- ⬜ Not started
- 🟡 In progress / Proposed
- ✅ Done
- ⛔ Blocked (외부 입력/조사 대기)

## 원칙

- **TDD 우선**: domain → application → infra 순서로 테스트 먼저 쓴다
- **DDD 경계 준수**: 도메인 패키지(`chain`, `source`, `verification`, `diff`)는 **프레임워크 import 금지** (gorm, huma, asynq, ethclient 모두 infra/adapters 레이어로만)
- **코어 ↔ 어댑터 분리**: `internal/source/`는 구체 어댑터 import 0. `database/sql`이 `mysql`을 모르듯.
- **OSS 친화**: 내부 민감정보(URL/IP/API 키) 코드·문서·fixture 어디에도 포함 금지. 사용자 고유 indexer는 `examples/` 패턴을 참고해 사용자 repo에 구현.
- **확장점 미리**: 체인·소스·샘플링 모드·trigger 종류·Metric 모두 인터페이스 or sealed-type (실시간 streaming·멀티체인 확장 대비)
- **블랙박스 테스트**: `package <name>_test` 패턴으로 public API만 테스트 → DDD 경계 자동 강제
- **Phase 독립성**: Phase N 완료하지 않아도 N+1을 mock/fake로 먼저 설계·테스트 가능 (TDD 외부-in / 내부-out 모두 허용)

## 참고 문서

- [CLAUDE.md](../../CLAUDE.md) — (생성 예정) 코드베이스 가이드
- [docs/architecture.md](../architecture.md) — (생성 예정) 아키텍처 결정 기록(ADR)
- [docs/research/source-shapes.md](../research/source-shapes.md) — 소스별 필드 매핑 매트릭스
