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

**최종 업데이트**: 2026-04-23 (Phase 7A~7I + 7I.2 + 7.6 + 8 + 9.1~9.5 + 10a + 10b 완료 — MVP 스택 완성)
**현재 단계**: **Phase 10 완료 — DB-backed sources + AES-GCM 시크릿 + /sources CRUD UI + Docker Compose 통합(server + worker + web + optional caddy basic-auth). `make stack-up` 한 번이면 5-컨테이너 스택이 올라오고 실 RPC + Blockscout + Routescan으로 run 실행됨. 다음은 Phase 11 (Helm chart) 또는 Phase 12 (probe context).**

> Phase 12 (probe context — API 응답시간 / 에러 모니터링)는 별도 bounded context로 분리. 설계 스케치는 [phase-12-probe-context.md](./phase-12-probe-context.md) 참고. Phase 8 이후 착수.

### 완료 (committed)

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
| **Phase 7C.2** — verification.AddressSamplingPlan 4종 (Known/TopN/Random/RecentlyActive) + application.AddressSampler 포트 + FakeAddressSampler | `9699332` |
| **Phase 7C.3** — Run.addressPlans + ExecuteRun AddressLatest 경로 (parallel fan-out, AnchorWindowed-ready snapshots, Budget reserve/refund) | `4bcce6c` |
| **Phase 7C.4** — persistence `address_plans` JSONB 컬럼 + mapper round-trip + integration 테스트 보강 | `5f21b5e` |
| **Phase 7D+7E+7F** — scheduled-run end-to-end 파이프라인: ① `HandleScheduledRun` 실구현 (payload→Run→save→enqueue) + persistence 헬퍼 export + Dispatcher 와이어포맷 통일; ② durable schedule store (마이그레이션 003 `schedules` 테이블, `ScheduleRecord`/`ScheduleRepository` 포트, gorm 구현체, Dispatcher의 in-memory store→DB-backed `dbConfigProvider` refactor, worker main `Scheduler.Start()` 배선); ③ scheduled 경로에 AddressSamplingPlan 전파 (마이그레이션 004 `schedules.address_plans` JSONB, `SchedulePayload`/`ScheduleRecord`/`ScheduleRunInput`/`ScheduledRunPayload` plans 필드, 핸들러와 `ScheduleRun` 유스케이스 pass-through) | `51af054` |
| **Phase 7G** — ExecuteRun AddressAtBlock 경로: `extractAddressAtBlockField` (balance/nonce) + `runAddressAtBlockPass` + `compareAddressAtBlock` + `fetchAddressAtBlockAll` (Budget reserve/refund). 샘플링은 AddressLatest와 동일한 AddressSamplingPlan 집합 재사용, 카티션은 addresses × blocks. Discrepancy.Block = queried block, SaveDiffMeta.AnchorBlock = Run finalized anchor (두 값 분리). 아카이브 미지원 소스는 ErrUnsupported로 skip. | `bbfda00` |
| **Phase 7H** — ExecuteRun ERC20 Holdings 경로: `extractERC20HoldingsField` (정렬된 "contract=balance;..." canonical form) + `runERC20HoldingsLatestPass` + `compareERC20HoldingsLatest` + `fetchERC20HoldingsAll` (Tier B Budget reserve/refund). 필터는 Category가 아닌 Capability 기준(`filterByCapability` 헬퍼 신규) — ERC20* 메트릭은 CatAddressLatest 카테고리를 plain balance/nonce와 공유하기 때문. | `7b40927` |
| **Phase 7I** — ExecuteRun ERC20 Balance(per-token) 경로 + TokenSamplingPlan 도메인: `verification.TokenSamplingPlan` 인터페이스 + `KnownTokens` stratum + `Run.tokenPlans` 필드 + `Run.SetTokenPlans` (StatusPending 한정) + `TokenSampler` 포트 + `FakeTokenSampler` + `extractERC20BalanceField` + `runERC20BalanceLatestPass` (addresses × tokens cartesian) + `compareERC20BalanceLatest` + `fetchERC20BalanceAll` (Budget reserve/refund). 퍼시스턴스 / scheduled-run 전파는 follow-up. | `7143cc3` |
| **Phase 7.6** — handler 메트릭 미들웨어: `queue.LoggingMiddleware(logger)` (task type / duration_ms / 성공-실패 slog 로깅). worker main에서 `mux.Use(queue.LoggingMiddleware(logger))` 로 체인 선두에 배선. asynqmon은 이미 `docker-compose.yml` `tools` profile에 존재. | `76aaedf` |
| **Phase 8.1** — HTTP API 서버 골격: chi 라우터 + huma v2 + humachi adapter, 미들웨어(requestid(X-Request-ID echo)/logging(slog request log)/recover(panic→500+stack)/cors(origin allow-list)), `routes/health.go` (`/healthz` 리브니스 + `/readyz` 리디니스), huma가 자동 서빙하는 `/openapi.json` + `/docs`, `cmd/csw-server/main.go` (DATABASE_URL/REDIS_URL 필수, Postgres PingContext + Redis TCP dial로 readiness 체크), `persistence.Ping(ctx, db)` 헬퍼 신설, `routes.HealthDeps`/`routes.HealthChecker` 포트. | `9aa153c` |
| **Phase 8.2** — /runs 리소스: `internal/application/cancel_run.go` (Run.Cancel coordinator), `httpapi/dto/run.go` (SamplingInput/TriggerInput/ScheduleInput/AddressPlanInput 디스크리미네이터 union + CreateRunRequest/RunView/ListRunsResponse + ToDomain/ToRunView 매퍼), `routes/runs.go` (POST /runs 생성 → ScheduleRun.Execute, GET /runs/{id} 상세, GET /runs?chain_id/status/limit/offset 리스트, POST /runs/{id}/cancel → CancelRun.Execute). `routes.MapError` (`routes/errors.go`) — 애플리케이션 센티넬 에러 → huma HTTP 에러 매핑. Import cycle 방지를 위해 errors.go를 routes 패키지로 이동. httptest 기반 10종 테스트. | (pending commit) |
| **Phase 7I.2** — TokenPlans 퍼시스턴스/전파: migration 005/006 (`runs.token_plans`, `schedules.token_plans` JSONB), `persistence.Marshal/UnmarshalTokenPlans` + `tokenPlanEnvelope`/`knownTokensJSON`, `verification.Rehydrate` variadic→explicit slices + `tokenPlans`, mapper 양방향 라운드트립, `application.SchedulePayload`/`ScheduleRecord`/`ScheduleRunInput` TokenPlans 필드, `queue.ScheduledRunPayload.TokenPlansData` + dispatcher/handler 전파, `dto.TokenPlanInput`/`KnownTokensIn`/`ResolveTokenPlans` + `CreateRunRequest`/`CreateScheduleRequest` `token_plans` 필드 + `RunView`/`ScheduleView.TokenPlanKinds`. 인메모리 + testcontainers DB 라운드트립 테스트 추가. | (pending commit) |
| **Phase 9.1~9.4** — 프론트 골격: `web/` Next.js 16 + Tailwind v4 + shadcn/ui(button/card/table/dialog/dropdown-menu/tabs/badge/skeleton/sonner/input) + Biome(lint/format) + next-themes(다크) + TanStack Query v5(+devtools dev-only) + openapi-typescript(`pnpm gen:api` → `lib/api/schema.ts`) + openapi-fetch(`lib/api/client.ts`) + 공용 훅(`useRuns`/`useDiffs`/`useSchedules`/`useReadiness`). `AppShell` 사이드바+헤더+테마 토글, `StatusBadge`/`SeverityBadge`/`TierBadge`/`EmptyState`. 5개 스텁 페이지(/, /runs, /diffs, /schedules, /sources) 빌드·린트·dev-server 200 응답 확인. `Makefile`에 `web-deps`/`web-gen`/`web-dev`/`web-build`/`web-lint` 타겟. | (pending commit) |

### 진행 중

- ✅ (Phase 7I.2 완료) **TokenPlans 퍼시스턴스 라운드트립** — runs/schedules `token_plans` JSONB 컬럼 (migration 005/006), persistence mapper, `SchedulePayload`/`ScheduleRecord`/`ScheduleRunInput`/`ScheduledRunPayload` TokenPlans 필드, HTTP `TokenPlanInput` DTO, `ScheduleRun` 유스케이스 pass-through 모두 완료.
- (follow-up) **추가 TokenSamplingPlan 스트래텀** — TopNTokens / RandomTokens / FromHoldings (Holdings 결과의 union). 현재 KnownTokens만 구현됨.
- (follow-up) **`csw openapi-dump` 누락 라우트** — `RegisterSources`가 `Gateway == nil`일 때 early-return해서 dumped 스펙에서 `/sources` 제외됨. 유사하게 `RegisterRuns` POST/cancel과 `RegisterSchedules` POST/DELETE도 optional deps 의존. openapi-dump용 stub deps 또는 dry-register 메커니즘 필요. Phase 9.5/9.8/9.9 착수 전 해결 필요.
- (follow-up) Snapshot 경로. 현재 ExecuteRun은 BlockImmutable + AddressLatest + AddressAtBlock + ERC20 Holdings + ERC20 Balance 커버.
- (follow-up) Block fetch 경로에도 Budget 통합 — 현재 Budget은 AddressLatest / AddressAtBlock / ERC20 Holdings / ERC20 Balance fetch에 적용됨, Block fetch만 남음.

### 남은 잔여 & 미구현

- **Phase 3F `adapters/etherscan/`** → **post-MVP로 연기**. Free tier가 Optimism 미커버라 MVP에서 가치 없음. Ethereum mainnet 확장 시점에 구현 (ethscan.Client 재사용이라 1일 이내 추정).
- **Phase 3G `examples/custom-graphql-adapter/`** → 간단 스켈레톤. Phase 4/5 도메인 확정 후 작성하면 예시가 실제와 일치 (Phase 7/8 즈음에 끼워넣기 좋음).
- **ExecuteRun 커버리지 확장** (Phase 7G+):
  - ✅ **AddressAtBlock** 경로 (Phase 7G 완료) — `FetchAddressAtBlock` + `extractAddressAtBlockField` + `runAddressAtBlockPass` 신규. Subject는 Address, Discrepancy.Block은 queried block (anchor와 분리).
  - ✅ **ERC-20 Holdings** (Phase 7H 완료) — `FetchERC20Holdings` + 정렬된 canonical extractor + runERC20HoldingsLatestPass. `filterByCapability` 신설로 Category-공유 문제 해결.
  - ✅ **ERC-20 Balance(per-token)** (Phase 7I 완료) — TokenSamplingPlan 도메인(KnownTokens) + TokenSampler 포트 + FakeTokenSampler + Run.SetTokenPlans + (address × token) cartesian fan-out. 퍼시스턴스 round-trip은 follow-up.
  - **Snapshot** (`CapTotalAddressCount` 등) — Observational 기본이라 현재 `DefaultPolicy`가 Info로 suppress. 관찰용 뷰 필요 시 Phase 8/9 API/UI 시점에 복원.
  - **Block fetch 경로에도 Budget 통합** — 현재 Budget은 AddressLatest/AddressAtBlock/ERC20 Holdings fetch에 적용됨, Block fetch만 남음. 사용자 RPC 엔드포인트도 quota 있을 수 있어 확장 여지.
- ✅ **asynqmon + 핸들러 메트릭** (Phase 7.6 완료) — asynqmon은 `docker-compose.yml` `tools` profile로 이미 존재. `queue.LoggingMiddleware`는 slog 기반 처리시간/성공-실패 로깅. (선택) Prometheus exporter는 Phase 10에서 추가 검토.
- **Phase 6 잔여**:
  - ✅ `DiffRepository.Save` meta 확장 완료 (7C.1).
  - 사용자 정의 Metric 영속화 미지원 — mapper는 `verification.AllMetrics()` 카탈로그 키만 인식. 필요 시 `metric_category` 컬럼을 함께 저장하고 Metric 재구성 로직 추가.
  - 통합 테스트는 `-tags=integration` + Docker 필요. CI 파이프라인(Phase 10)에서 자동 실행되게 훅 걸어야 함.
- **Phase 12 (probe context) — post-Phase 8**: API 응답시간 / 에러 모니터링. [phase-12-probe-context.md](./phase-12-probe-context.md) 스케치만 작성됨. 자체 indexer 1차, 번들 어댑터 2차.

### 다음 세션 재개 절차

1. **Phase 8 (huma HTTP API)** → `/api/runs`, `/api/diffs`, `/api/schedules` 리소스 3개. ScheduleRun 유스케이스 이미 준비됨 (address plans 포함). Schedule 생성 API 붙일 때 TokenPlans 퍼시스턴스 라운드트립(Phase 7I.2)도 같이 배선하는 것을 권장.
2. Phase 9 (Next.js) → Phase 10 (observability + docker-compose 통합) → Phase 11 (Helm).
3. Phase 12 (probe context)는 Phase 8 완료 시점에 본격 설계. 현재는 스케치만.
4. (선택) 중간 어느 시점에 Phase 3G 작성.
5. Phase 7I.2 — TokenPlans 퍼시스턴스/scheduled-run 전파 (Phase 8 schedule API와 같이).

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

### Open Items — Phase 8 착수 전 확정 필요

- [ ] reflected-block 메타 없는 지표의 최종 분류 (제외 vs "관찰 전용") — Phase 4 `DefaultPolicy`는 Snapshot을 Info로 고정했지만 per-metric override 필요할 수 있음. AddressAtBlock 확장 시점에 실측 데이터로 재검토.
- [ ] rate-limit budget 정책: `exhausted_policy` 기본값 (skip/defer/fail) — 현재는 암묵적으로 `skip`(해당 source만 제외). Phase 8 config/API에 노출할 때 명시적으로 선택 가능하게.
- [ ] Blockscout `bypass-429-option` 토큰 취득 절차 (실제 429 히트 시점에 실험)
- [x] ✅ `DiffRepository.Save` 시그니처 확장 — `SaveDiffMeta` 구조체로 Tier/AnchorBlock/SamplingSeed 포함 (Phase 7C.1)
- [x] ✅ `ToleranceResolver` 포트 도입 완료 (Phase 7C.1)
- [ ] Go 툴체인 `covdata` 바이너리 누락 우회 — 현재 3개 패키지에 trivial smoke test로 회피. 장기적으로 Makefile `test` 타겟 재작성 (예: `-coverpkg` 지정) 검토.
- [ ] 시크릿/설정 로딩 — Phase 8 csw-server 기동 시 config.yaml + env 오버라이드 실측 검증 (koanf 레이어링은 Phase 0에 있지만 end-to-end 미검증).

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
| 3G | `examples/custom-graphql-adapter/` + `private/` build-tag pattern + `/sources/types` endpoint | ✅ Done | 2C, 10a | [phase-03-source-adapters.md](./phase-03-source-adapters.md), [README 섹션](../../README.md#adding-a-private-adapter) |
| 4 | `verification/` + `diff/` 도메인 (Metric 카테고리) | ✅ Done | 1, 2C | [phase-04-verification-diff-domain.md](./phase-04-verification-diff-domain.md) |
| 5 | Application (use case) — 5A/5B/5C 완료 (ExecuteRun은 BlockImmutable MVP) | ✅ Done (MVP) | 2, 4 | [phase-05-application.md](./phase-05-application.md) |
| 6 | Persistence (Postgres + gorm + golang-migrate + testcontainers) | ✅ Done | 4, 5 | [phase-06-persistence.md](./phase-06-persistence.md) |
| 7A | Queue — asynq dispatcher + worker + scheduler + health | ✅ Done | 5 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7B | Queue — RedisBudget (RateLimitBudget 구현체) | ✅ Done | 5, 7A | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7C.1 | Application — ToleranceResolver + DiffRepository.Save meta | ✅ Done | 5, 6 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7C.2 | Application — 4-stratum 주소 샘플링 (AddressSamplingPlan + AddressSampler 포트) | ✅ Done | 5, 7C.1 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7C.3 | Application — ExecuteRun AddressLatest 경로 + Budget reserve/refund 통합 | ✅ Done (AddressLatest) | 5, 7B, 7C.2 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7C.4 | Persistence — `runs.address_plans` 컬럼 + mapper round-trip | ✅ Done | 6, 7C.3 | [phase-06-persistence.md](./phase-06-persistence.md) |
| 7D | Queue — ScheduledRun handler 실구현 (payload → Run → ExecuteRun enqueue) | ✅ Done | 5, 6, 7A | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7E | Durable schedule store (schedules 테이블 + ScheduleRepository + DB-backed provider) | ✅ Done | 6, 7D | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7F | Scheduled payload + ScheduleRecord에 AddressSamplingPlan 포함 (schedules.address_plans 컬럼) | ✅ Done | 7C.3, 7E | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7G | Application — ExecuteRun AddressAtBlock 경로 (extractor + runAddressAtBlockPass + fetchAll with Budget) | ✅ Done | 7C.3 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7H | Application — ExecuteRun ERC20 Holdings 경로 (canonical extractor + runERC20HoldingsLatestPass + filterByCapability) | ✅ Done | 7C.3 | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7I | Application — ERC20 Balance(per-token) 경로 + TokenSamplingPlan + TokenSampler 포트 | ✅ Done (MVP) | 7H | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7I.2 | Persistence / scheduled-run — TokenPlans round-trip (runs/schedules.token_plans JSONB, SchedulePayload/ScheduleRecord/ScheduledRunPayload 필드, HTTP TokenPlanInput, pass-through) | ✅ Done | 7I | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 7.6 | Observability — `queue.LoggingMiddleware` (slog duration/outcome) + asynqmon docker-compose profile | ✅ Done | 7A | [phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) |
| 8.1 | HTTP API — server skeleton (chi + huma) + 미들웨어(requestid/logging/recover/cors) + /healthz /readyz + /openapi.json + cmd/csw-server | ✅ Done | 5, 6 | [phase-08-http-api.md](./phase-08-http-api.md) |
| 8.2 | HTTP API — /runs 라우트 (POST/GET list/GET detail/POST cancel) + dto.{Sampling,Trigger,Schedule,AddressPlan}Input + CancelRun 유스케이스 | ✅ Done | 8.1 | [phase-08-http-api.md](./phase-08-http-api.md) |
| 8.3 | HTTP API — /diffs 라우트 (GET list/GET detail/POST replay) + /runs/{id}/diffs | ✅ Done | 8.1 | [phase-08-http-api.md](./phase-08-http-api.md) |
| 8.4 | HTTP API — /schedules 라우트 (POST/GET/DELETE) + QuerySchedules 유스케이스 (TokenPlans 라운드트립은 7I.2에서) | ✅ Done | 8.1 | [phase-08-http-api.md](./phase-08-http-api.md) |
| 8.5 | HTTP API — /sources 라우트 (capability matrix + tier); chain_id 필수 쿼리 파라미터 | ✅ Done | 8.1 | [phase-08-http-api.md](./phase-08-http-api.md) |
| 8.7 | csw openapi-dump 서브커맨드 (+ `make openapi`) — `--format=json\|yaml`, `--output=path`, 기본 stdout json | ✅ Done | 8.1 | [phase-08-http-api.md](./phase-08-http-api.md) |
| 9.1-9.4 | Frontend skeleton — Next.js 16 + shadcn/ui + TanStack Query + Biome + openapi-typescript + 공용 레이아웃 + 5개 스텁 페이지 | ✅ Done | 8 | [phase-09-frontend.md](./phase-09-frontend.md) |
| 9.5-9.9 | Frontend pages — /runs 상세 + 생성 폼, /diffs 상세 + replay, /schedules CRUD, /sources capability matrix | ⬜ Not started | 9.1-9.4, openapi-dump 완성 | [phase-09-frontend.md](./phase-09-frontend.md) |
| 9.10 | Integration & deploy — docker-compose에 web 서비스 / runtime env | ⬜ Not started | 9.5-9.9 | [phase-09-frontend.md](./phase-09-frontend.md) |
| 10a | Source configuration store (DB-backed + encrypted secrets + YAML seed + CRUD UI) | ✅ Done | 6, 8, 9.1-9.4 | [phase-10-integration-observability.md](./phase-10-integration-observability.md#phase-10a--source-configuration-store) |
| 10b | Integration (Docker Compose 통합 + 바이너리/web Dockerfile + optional caddy basic-auth + 배포 가이드) | ✅ Done (MVP) | 10a | [phase-10-integration-observability.md](./phase-10-integration-observability.md#phase-10b--observability--deploy-이하-기존-내용) |
| 10b+ | Observability (Prometheus/Grafana) + E2E 자동화 + OIDC auth (Option C) | ⬜ Post-MVP | 10b | 계획만 — 후속 릴리스 |
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
