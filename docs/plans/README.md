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

**최종 업데이트**: 2026-04-22 (Phase 7A/7B/7C.1/7C.2/7C.3 완료, Phase 12 probe context 스케치 추가)
**현재 단계**: **Phase 7 진행 중 — asynq queue 배선 + RedisBudget + ToleranceResolver + DiffRepository.Save meta + AddressSamplingPlan 도메인 + AddressSampler 포트 + ExecuteRun AddressLatest 경로(+ Budget 통합) 완료. 다음은 Phase 7D (ScheduledRun 실구현 + durable schedule store)**

> Phase 12 (probe context — API 응답시간 / 에러 모니터링)는 별도 bounded context로 분리. 설계 스케치는 [phase-12-probe-context.md](./phase-12-probe-context.md) 참고. Phase 8 이후 착수.

### 완료 (committed, origin/main 대비 16 커밋 앞섬)

| 구분 | 커밋 |
|---|---|
| Phase 0 Foundations | `ac4b50e` · `4eab3cd` |
| Phase 1 `chain/` 도메인 (값객체 5종) | `498c09b` |
| Phase 2 `source/` 포트 + Fake | `a8b9b20` · `cfd7549` |
| 테스트 fixture 합성화 + Ralph 셀프 리뷰 | `725b063` · `f939232` |
| CLAUDE.md rule 6 (.env secret 비재출력) | `6d27c4c` |
| 외부 API 커버리지 리서치 | `50b3771` |
| 3-tier 모델 + anchor 전략 문서 | `c82e62e` |
| Open Q A 5항목 curl 검증 결과 | `79256fe` |
| **Phase 2C** — Tier / BlockTag / ReflectedBlock / Capability 4종 | `96f8803` |
| Lint false-positive suppression | `099c377` |
| **Phase 3A** — `adapters/internal/httpx/` | `5469119` |
| **Phase 3B** — `adapters/rpc/` (JSON-RPC) | `01460dd` |
| **Phase 3C** — `adapters/internal/ethscan/` | `4639321` |
| CLAUDE.md rule 7 (레이어별 comment discipline) | `b0ffde3` |
| **Phase 3D + 3E** — `adapters/blockscout/` + `adapters/routescan/` | `679dd61` |
| **Phase 4** — `verification/` + `diff/` 순수 도메인 (Metric / Sampling / Trigger / Run / Tolerance / Judgement) | `9cd1ce5` |
| **Phase 5A** — application ports / errors / testsupport fakes / ScheduleRun / QueryRuns / QueryDiffs | `3eb9d9a` |
| **Phase 5B + 5C** — ExecuteRun 엔진 + ReplayDiff (BlockImmutable 전용 MVP) | `a8f29c2` |
| **Phase 6** — `cmd/csw migrate` CLI + golang-migrate 임베드 + `internal/infrastructure/persistence/` gorm 구현체 + testcontainers 통합 테스트 | `173193f` |
| **Phase 7A** — asynq dispatcher + worker skeleton + handlers + scheduler + health endpoints | `48b8335` |
| **Phase 7B** — RedisBudget for RateLimitBudget port | `72bb57d` |
| **Phase 7C.1** — application.ToleranceResolver + DiffRepository.Save meta (Tier/AnchorBlock/SamplingSeed) | `d837cdc` |
| **Phase 7C.2** — verification.AddressSamplingPlan 4종 (Known/TopN/Random/RecentlyActive) + application.AddressSampler 포트 + FakeAddressSampler | _uncommitted_ |
| **Phase 7C.3** — Run.addressPlans + ExecuteRun AddressLatest 경로 (parallel fan-out, AnchorWindowed-ready snapshots, Budget reserve/refund) | _uncommitted_ |

### 진행 중

- Phase 7D — `ScheduledRun` handler 실구현 (현재 7A stub). durable schedule store 설계 포함.
- (follow-up) Run persistence 스키마에 `addressPlans` 열 추가 — 현재 mapper는 rehydrate 시 plans를 빈 슬라이스로 돌려주므로 enqueue → rehydrate → ExecuteRun 경로에서 AddressLatest 커버리지가 끊김. 인메모리 테스트는 정상 동작.
- (follow-up) AddressAtBlock / ERC-20 balance+holdings / Snapshot 경로. 현재 ExecuteRun은 AddressLatest만 비교.
- (follow-up) Block fetch 경로에도 Budget 통합 — 현재 Budget은 AddressLatest fetch에만 적용됨.

### 남은 잔여 & 미구현

- **Phase 3F `adapters/etherscan/`** → **post-MVP로 연기**. Free tier가 Optimism 미커버라 MVP에서 가치 없음. Ethereum mainnet 확장 시점에 구현 (ethscan.Client 재사용이라 1일 이내 추정).
- **Phase 3G `examples/custom-graphql-adapter/`** → 간단 스켈레톤. Phase 4/5 도메인 확정 후 작성하면 예시가 실제와 일치 (Phase 7/8 즈음에 끼워넣기 좋음).
- **Phase 7 잔여 (진행 중)**:
  - **7C.2**: 4-stratum 주소 샘플링 (known / top-N / random / recently-active). `AddressSamplingPlan` 도메인 값객체 + `AddressSampler` 포트 + 인프라 구현체.
  - **7C.3**: ExecuteRun AddressLatest / AddressAtBlock Tier B 경로 + budget `Reserve/Refund` 통합.
  - **ScheduledRun handler 실구현** — 현재 7A stub. durable schedule store (Postgres `schedules` 테이블) + `HandleScheduledRun`이 payload로 Run을 materialize → ExecuteRun enqueue.
  - **asynqmon / observability 7.6** — docker-compose override + 핸들러 metrics (성공/실패 카운트, 처리 시간).
- **Phase 6 잔여 — Phase 7/10에서 해소**:
  - ✅ `DiffRepository.Save` meta 확장 완료 (7C.1).
  - 사용자 정의 Metric 영속화 미지원 — mapper는 `verification.AllMetrics()` 카탈로그 키만 인식. 필요 시 `metric_category` 컬럼을 함께 저장하고 Metric 재구성 로직 추가.
  - 통합 테스트는 `-tags=integration` + Docker 필요. CI 파이프라인(Phase 10)에서 자동 실행되게 훅 걸어야 함.
- **Phase 12 (probe context) — post-Phase 8**: API 응답시간 / 에러 모니터링. [phase-12-probe-context.md](./phase-12-probe-context.md) 스케치만 작성됨. 자체 indexer 1차, 번들 어댑터 2차.

### 다음 세션 재개 절차

1. **Phase 7C.2 착수** — `internal/verification/address_sampling.go` 값객체 (known / top_n / random / recently_active) + black-box 테스트. 그 다음 `internal/application/ports.go`에 `AddressSampler` 포트 추가, 그 다음 `internal/infrastructure/sampling/` 구현체.
2. 7C.3 — ExecuteRun AddressLatest fan-out + budget 통합.
3. 이후 Phase 8 (huma HTTP API) → Phase 9 (Next.js) → Phase 10 (observability + docker-compose) → Phase 11 (Helm).
4. Phase 12 (probe context)는 Phase 8 완료 시점에 본격 설계. 현재는 스케치만.
5. (선택) 중간 어느 시점에 Phase 3G 작성.

### 확정 결정 (구현 완료된 것 포함)

- **3-tier 모델** ✅ 구현: Tier A(RPC 전수) / Tier B(3rd-party 샘플링) / Tier C(지표별) — `internal/source/tier.go` + `Capability.Tier()`
- **anchor 전략** ✅ 구현: `BlockTag` 값객체 · `CompareContext.Anchor/AnchorBlock` · `ResultMeta.ReflectedBlock` · Blockscout `block_number_balance_updated_at` 실측 반영
- **4-stratum 샘플링**: Phase 7에서 구현 예정 (Phase 5 초기 계획에서 이동)
- **기본 OSS 공개 구성 = User-RPC(archive) + Blockscout + Routescan** 3-way ✅ 모든 어댑터 구현 완료
- **Routescan-specific 성과**: `account/balancehistory` Optimism free 동작 → Tier A fallback 경로 확보
- **Blockscout 스팸 필터**: `is_scam` / `reputation != "ok"` 토큰 자동 제외 (ERC-20 holdings)
- **MetricCategory ↔ Severity 기본 매핑** ✅ 구현: BlockImmutable/AddressAtBlock → Critical, AddressLatest → Warning, Snapshot → Info (`diff.DefaultPolicy`)
- **신뢰 클러스터 선정** ✅ 구현: `DefaultPolicy.SourceTrust` 리스트에서 가장 높은 우선순위 소스가 속한 클러스터가 trusted. 랭크된 소스 없으면 최대 클러스터(lex tiebreak).
- **영속화 도구 선택** ✅: golang-migrate (embedded) + gorm + lib/pq. AutoMigrate 금지.
- **testcontainers 전략** ✅: `TestMain` 1회 기동 + 케이스간 TRUNCATE. `-tags=integration`로 기본 CI에서는 분리.
- **L2 특이필드**: backlog 유지 (post-MVP)
- **indexer Capability 선언**: 필요 시 Phase 7에서 도입

### Open Items — Phase 7 착수 전 확정 필요

- [ ] reflected-block 메타 없는 지표의 최종 분류 (제외 vs "관찰 전용") — Phase 7 실제 비교 시점에 결정. Phase 4 `DefaultPolicy`는 Snapshot을 Info로 고정했지만 per-metric override 필요할 수 있음.
- [ ] rate-limit budget 정책: `exhausted_policy` 기본값 (skip/defer/fail) — Phase 7 config에 노출
- [ ] Blockscout `bypass-429-option` 토큰 취득 절차 (실제 429 히트 시점에 실험)
- [ ] `DiffRepository.Save` 시그니처 확장 여부 — Tier/AnchorBlock/SamplingSeed를 Save에 인자로 넣을지, 혹은 `SaveWithMeta` 별도 메서드로 둘지. Phase 7 ExecuteRun 업데이트 때 결정.
- [ ] `ToleranceResolver` 포트 도입 시점 — NumericTolerance/AnchorWindowed가 per-metric으로 필요해지는 순간. Phase 7 AddressLatest 비교 착수와 맞물림.
- [ ] Go 툴체인 `covdata` 바이너리 누락 우회 — 현재 3개 패키지에 trivial smoke test로 회피. 장기적으로 Makefile `test` 타겟 재작성 (예: `-coverpkg` 지정) 검토.

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
| 4 | `verification/` + `diff/` 도메인 (Metric 카테고리) | ✅ Done | 1, 2C | [phase-04-verification-diff-domain.md](./phase-04-verification-diff-domain.md) |
| 5 | Application (use case) — 5A/5B/5C 완료 (ExecuteRun은 BlockImmutable MVP) | ✅ Done (MVP) | 2, 4 | [phase-05-application.md](./phase-05-application.md) |
| 6 | Persistence (Postgres + gorm + golang-migrate + testcontainers) | ✅ Done | 4, 5 | [phase-06-persistence.md](./phase-06-persistence.md) |
| 7A | Queue — asynq dispatcher + worker + scheduler + health | ✅ Done | 5 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7B | Queue — RedisBudget (RateLimitBudget 구현체) | ✅ Done | 5, 7A | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7C.1 | Application — ToleranceResolver + DiffRepository.Save meta | ✅ Done | 5, 6 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7C.2 | Application — 4-stratum 주소 샘플링 (AddressSamplingPlan + AddressSampler 포트) | ✅ Done | 5, 7C.1 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7C.3 | Application — ExecuteRun AddressLatest 경로 + Budget reserve/refund 통합 | ✅ Done (AddressLatest) | 5, 7B, 7C.2 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7D | Queue — ScheduledRun handler 실구현 + durable schedule store | ⬜ Not started | 5, 6, 7A | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 8 | HTTP API (chi + huma) | ⬜ Not started | 5, 6 | [phase-08-http-api.md](./phase-08-http-api.md) |
| 9 | Frontend (Next.js 15) | ⬜ Not started | 8 | [phase-09-frontend.md](./phase-09-frontend.md) |
| 10 | Integration / Observability / Local Deploy | ⬜ Not started | 3, 6, 7, 8, 9 | [phase-10-integration-observability.md](./phase-10-integration-observability.md) |
| 11 | Kubernetes 배포 (Helm) | ⬜ Not started | 10 | [phase-11-kubernetes-deploy.md](./phase-11-kubernetes-deploy.md) |
| 12 | Probe Context — API 응답시간 / 에러 모니터링 (별도 bounded context) | ⬜ Sketch only | 7, 8 | [phase-12-probe-context.md](./phase-12-probe-context.md) |

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
