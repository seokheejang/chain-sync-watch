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

**최종 업데이트**: 2026-04-20 (Phase 3D+3E 완료 시점)
**현재 단계**: **Phase 3 거의 완료 — 3F(etherscan) 보류, 3G(custom-graphql 예시) 남음 → Phase 4 진입 준비**

### 완료 (committed, origin/main 대비 10 커밋 앞섬)

| 구분 | 커밋 |
|---|---|
| Phase 0 Foundations | `ac4b50e` · `4eab3cd` |
| Phase 1 `chain/` 도메인 (값객체 5종) | `498c09b` |
| Phase 2 `source/` 포트 + Fake | `a8b9b20` · `cfd7549` |
| 테스트 fixture 합성화 + Ralph 셀프 리뷰 | `725b063` · `f939232` |
| CLAUDE.md rule 6 (.env secret 비재출력) | `6d27c4c` |
| 외부 API 커버리지 리서치 | `50b3771` |
| 3-tier 모델 + anchor 전략 문서 (Phase 2C/3/7 doc) | `c82e62e` |
| Open Q A 5항목 curl 검증 결과 | `79256fe` |
| **Phase 2C** — Tier / BlockTag / ReflectedBlock / Capability 4종 | `96f8803` |
| Lint false-positive suppression | `099c377` |
| **Phase 3A** — `adapters/internal/httpx/` | `5469119` |
| **Phase 3B** — `adapters/rpc/` (JSON-RPC) | `01460dd` |
| **Phase 3C** — `adapters/internal/ethscan/` | `4639321` |
| CLAUDE.md rule 7 (레이어별 comment discipline) | `b0ffde3` |
| **Phase 3D + 3E** — `adapters/blockscout/` + `adapters/routescan/` | `679dd61` |

### 진행 중

- 없음. origin/main 대비 10 커밋 앞섬 — push는 사용자 판단에 맡김.

### 남은 Phase 3 잔여

- **Phase 3F `adapters/etherscan/`** → **post-MVP로 연기**. Free tier가 Optimism 미커버라 MVP에서 가치 없음. Ethereum mainnet 확장 시점에 구현 (ethscan.Client 재사용이라 1일 이내 추정).
- **Phase 3G `examples/custom-graphql-adapter/`** → 간단 스켈레톤만 (README + 50줄 골격). 다음 세션 착수 후보.

### 다음 세션 재개 절차

1. (선택) **Phase 3G** — `examples/custom-graphql-adapter/` 스켈레톤
2. **Phase 4 착수** — `internal/verification/` + `internal/diff/` 도메인. Phase 2C의 `Tolerance.Judge(ctx CompareContext)` 확장 시그니처 + `AnchorWindowed` / `Observational` Tolerance + MetricCategory ↔ Tier 매핑이 이미 [phase-04-verification-diff-domain.md](./phase-04-verification-diff-domain.md) 에 반영됨
3. 이후 Phase 5 (Application use case: Tier 분기 + SamplingStrategy + RateLimitBudget port)

### 확정 결정 (구현 완료된 것 포함)

- **3-tier 모델** ✅ 구현: Tier A(RPC 전수) / Tier B(3rd-party 샘플링) / Tier C(지표별) — `internal/source/tier.go` + `Capability.Tier()`
- **anchor 전략** ✅ 구현 기반: `BlockTag` 값객체 · `ResultMeta.ReflectedBlock` · Blockscout `block_number_balance_updated_at` 실측 반영
- **4-stratum 샘플링**: Phase 5에서 구현 예정
- **기본 OSS 공개 구성 = User-RPC(archive) + Blockscout + Routescan** 3-way ✅ 모든 어댑터 구현 완료
- **Routescan-specific 성과**: `account/balancehistory` Optimism free 동작 → Tier A fallback 경로 확보
- **Blockscout 스팸 필터**: `is_scam` / `reputation != "ok"` 토큰 자동 제외 (ERC-20 holdings)
- **L2 특이필드**: backlog 유지 (post-MVP)
- **indexer Capability 선언**: 필요 시 Phase 4/5에서 도입

### Open Items — Phase 4/5 착수 전 확정 필요

- [ ] reflected-block 메타 없는 지표의 최종 분류 (제외 vs "관찰 전용") — Phase 4 `JudgementPolicy` 설계에서 결정
- [ ] rate-limit budget 정책: `exhausted_policy` 기본값 (skip/defer/fail) — Phase 7에서 실제 config 노출
- [ ] Blockscout `bypass-429-option` 토큰 취득 절차 (실제 429 히트 시점에 실험)

---

## 진행도

| Phase | 제목 | 상태 | 의존 Phase | 문서 |
|---|---|---|---|---|
| 0 | Foundations | ✅ Done | — | [phase-00-foundations.md](./phase-00-foundations.md) |
| 1 | `chain/` 도메인 (ChainID slug/name 매핑) | ✅ Done | 0 | [phase-01-chain-domain.md](./phase-01-chain-domain.md) |
| 2 | `source/` 포트 (필드 단위 Capability, 코어 추상) | ✅ Done | 1 | [phase-02-source-ports.md](./phase-02-source-ports.md) |
| 2C | Capability 확장 + Tier 체계 + Anchor BlockTag | ✅ Done | 2 | [phase-02-source-ports.md](./phase-02-source-ports.md) (Phase 2C 섹션) |
| 3A | `adapters/internal/httpx/` (공용 HTTP base) | ✅ Done | 2C | [phase-03-source-adapters.md](./phase-03-source-adapters.md) |
| 3B | `adapters/rpc/` (JSON-RPC, archive+debug opt-in) | ✅ Done | 3A | [phase-03-source-adapters.md](./phase-03-source-adapters.md) |
| 3C | `adapters/internal/ethscan/` (Etherscan-compat base) | ✅ Done | 3A | [phase-03-source-adapters.md](./phase-03-source-adapters.md) |
| 3D | `adapters/blockscout/` (REST v2 + proxy hybrid) | ✅ Done | 3C | [phase-03-source-adapters.md](./phase-03-source-adapters.md) |
| 3E | `adapters/routescan/` (Etherscan-compat) | ✅ Done | 3C | [phase-03-source-adapters.md](./phase-03-source-adapters.md) |
| 3F | `adapters/etherscan/` | ⏸️ Deferred (post-MVP, ETH-mainnet 확장 시) | 3C | [phase-03-source-adapters.md](./phase-03-source-adapters.md) |
| 3G | `examples/custom-graphql-adapter/` | ⬜ Not started | 2C | [phase-03-source-adapters.md](./phase-03-source-adapters.md) |
| 4 | `verification/` + `diff/` 도메인 (Metric 카테고리) | ⬜ Not started | 1, 2C | [phase-04-verification-diff-domain.md](./phase-04-verification-diff-domain.md) |
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
- ⏸️ Deferred (post-MVP)

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
