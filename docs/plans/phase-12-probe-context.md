# Phase 12 — Probe Context (별도 Bounded Context)

> 이 문서는 **Phase 7 이후** 착수할 신규 bounded context의 스케치이다.
> 구현은 Phase 8 (HTTP API) 이후 원하는 시점에 시작한다. 여기서는 설계 방향과
> 결정사항만 기록해 Phase 8 API 설계 / Phase 9 프론트 설계가 이 영역의
> 존재를 알고 진행하도록 한다.

## 진행 현황 (2026-04-27)

작업을 7개 슬라이스로 쪼개 진행. 슬라이스 1 완료, 2~7 미착수.

- ✅ **슬라이스 12.1 — Domain** (2026-04-27): `internal/probe/` 순수 도메인. `Probe` 애그리게이트(ID + Target + Schedule + Thresholds + Enabled), `ProbeTarget`(http/rpc/graphql kind, URL/Method/Headers/Body), `ProbeSchedule`(cron XOR interval, ≥1s), `Threshold`(latency_p95_ms / latency_p99_ms / error_rate_pct / consecutive_failures + WindowSec + Label), `Observation`(immutable, 6-class `ErrorClass` enum: none/network/timeout/http_4xx/http_5xx/protocol, status/class consistency check, ErrMsgMaxLen=512 truncation), `Incident`(open/close 라이프사이클 + Breach + Evidence). 모든 애그리게이트 defensive copy + Rehydrate (validation skip) + black-box `_test` 패키지. depguard 룰 (`domain-purity` + `application-boundary`)에 `internal/probe` 추가. 테스트 커버리지 95.7%, race+vet+lint 통과.
- ✅ **슬라이스 12.2 — Application** (2026-04-27): `internal/application/probe/` (패키지명 `probeapp` — verification 의 `application` 과 분리). 4 ports: `ProbeRepository` / `ObservationRepository` / `IncidentRepository` / `HTTPProber` / `IDGen`. 4 use cases: `RunProbe` (probe 1회 실행 → Observation 저장, 비활성/inconsistent result fail-fast), `EvaluateWindow` (threshold 별 metric 계산 → Incident open/close, idempotent), `QueryProbes` / `QueryIncidents` (read-side). Metric 계산 헬퍼: `percentileMS` (nearest-rank), `errorRatePct`, `consecutiveFailures` (트레일링 run-length). `application.Clock` 재사용. testsupport in-memory fakes (FakeProbeRepo/FakeObservationRepo/FakeIncidentRepo/FakeHTTPProber/FakeIDGen). 18 테스트 케이스 (트립/회복/idempotency/멀티-threshold/empty window/consecutive failures broken streak/evidence cap), 커버리지 86.5%, race+vet+lint 통과.
- ⬜ **슬라이스 12.3 — Adapter**: `adapters/httpprobe/` — RTT 측정 + 에러 분류, `adapters/internal/httpx/` 재사용. probeapp.HTTPProber 인터페이스 구현.
- ⬜ **슬라이스 12.4 — Persistence**: migration + gorm models + 3 repository (probes/observations/incidents).
- ⬜ **슬라이스 12.5 — Queue**: asynq `probe:run` task + handler + scheduler config provider 통합.
- ⬜ **슬라이스 12.6 — HTTP API**: `/probes` / `/observations` / `/incidents` (read-first).
- ⬜ **슬라이스 12.7 — Frontend**: "서비스 헬스" 탭, probe overview + incident list.

## 왜 별도 context인가

기존 `verification/` + `diff/` 도메인은 **"여러 소스의 같은 데이터 값이
일치하는가"** — 데이터 정합성 축에 서 있다.

반면 다음 두 요구는 결이 다르다:

1. **자체 indexer API 응답 시간 모니터링** — long query 탐지
2. **API 서버 에러 모니터링** — 장애/이상 탐지

이들은:
- **소스 간 비교가 아니라 단일 소스 관찰** → N-way 계약이 깨짐.
- **임계값 기반 판정** (p95 > 2s, error_rate > 1% 등) → 기존 Tolerance 문법
  (값 equality + numeric slack)과 안 맞음.
- **시계열 집계 대상** → 기존 스키마는 Run 1회 스냅샷 저장. p95 / error-rate는
  window 집계가 필수.

따라서 **`probe/` 라는 별도 bounded context**로 분리한다. 어댑터
(`adapters/rpc`, `adapters/blockscout`, …)는 재사용하되 도메인은 독립.

## MVP 스코프

**1차 (Phase 12A)** — 자체 indexer API만. 여기서 요구 재료가 확정되면 2차 확장.

**2차 (Phase 12B, deferred)** — 번들 어댑터 전체로 확장 (Blockscout /
Routescan / Etherscan / RPC 포함). adapter 하나만 추가하면 같은 probe
파이프라인이 동작하도록 설계.

## 모니터링 항목 (초안)

| 항목 | 관찰 주체 | 판정 기준 | 비고 |
|---|---|---|---|
| **응답 지연** | 자체 indexer API 엔드포인트 | p50/p95/p99 window 집계 → 임계값 초과 시 Incident | long-query 탐지 목적 |
| **에러율** | 자체 indexer API 엔드포인트 | error_count / total_count window 집계 → 임계값 초과 시 Incident | **에러 분류 체계 추가 설계 필요** (5xx vs 4xx vs timeout vs network) |
| **가용성** (선택) | 자체 indexer API 엔드포인트 | 연속 N회 실패 시 즉시 Incident | 기본 off, 옵션 |

## 도메인 모델 (초안)

`internal/probe/` (verification/diff와 동등 레벨, 프레임워크 import 금지).

```go
// 관찰 대상 정의
type Probe struct {
    ID          ProbeID           // 안정 식별자 (config 기반)
    Target      ProbeTarget       // 무엇을 관찰하나
    Schedule    ProbeSchedule     // 얼마나 자주 (cron or interval)
    Thresholds  []Threshold       // 임계값 세트
}

type ProbeTarget struct {
    Kind       string             // "http_endpoint" | "rpc_method" | "graphql_query"
    URL        string
    Method     string             // HTTP GET/POST | RPC method name
    Payload    []byte             // 필요시 body
}

// 매 실행 단위의 1회 관찰
type Observation struct {
    ProbeID    ProbeID
    At         time.Time
    ElapsedMS  int64              // 응답 지연
    StatusCode int                // HTTP / RPC 결과 코드
    ErrorClass ErrorClass         // none | network | timeout | http_4xx | http_5xx | protocol
    ErrorMsg   string             // 원문 (truncated)
    // Body 내용은 저장하지 않음 (프라이버시 + 용량)
}

// window 집계 + 임계값 비교 결과
type Incident struct {
    ID         IncidentID
    ProbeID    ProbeID
    OpenedAt   time.Time
    ClosedAt   *time.Time         // nil = 현재 진행중
    Breach     Breach             // 어떤 임계값이 깨졌나
    // 최근 N개 Observation 샘플을 함께 보관 (대시보드 툴팁용)
    Evidence   []Observation
}

type Breach struct {
    Metric     string             // "latency_p95_ms" | "error_rate_pct" | "consecutive_failures"
    Threshold  float64
    Observed   float64
    WindowSec  int
}

type ErrorClass int
const (
    ErrorNone ErrorClass = iota
    ErrorNetwork
    ErrorTimeout
    ErrorHTTP4xx
    ErrorHTTP5xx
    ErrorProtocol             // JSON-RPC error code, GraphQL error
)
```

## Use case (application 레이어)

`internal/application/probe/` (기존 application 패키지와 분리 권장):

- `RunProbe` — Probe 1회 실행 → Observation 저장
- `EvaluateWindow` — 최근 window Observation 집계 → 임계값 위반 시 Incident
  open/close
- `QueryProbes`, `QueryIncidents` — 대시보드 조회

## 어댑터

**Phase 12A (자체 indexer)**: 단순 HTTP probe면 충분. `adapters/httpprobe/`
신규 (기존 `adapters/internal/httpx/` 재사용 가능).

**Phase 12B (번들 어댑터 확장)**: 기존 어댑터를 감싸는 얇은 wrapper를
고려 — Source 포트 호출 시 소요 시간을 측정해 Observation으로 emit하는
**decorator**. adapter 자체를 수정하지 않아도 됨.

## Persistence

`probes`, `observations`, `incidents` 테이블 신규. verification 테이블과
분리해 도메인 경계 유지.

- observations는 **고volume** (매 초 단위) → 보관 기간 짧게 (기본 7일), 집계
  결과는 별도 `observation_rollups` 테이블에 장기 보관.
- TimescaleDB hypertable 도입 여부는 볼륨 측정 후 결정 (초기엔 그냥 Postgres
  + 파티셔닝으로 시작).

## API (Phase 8 연동)

Phase 8 HTTP API 설계 시 두 리소스 그룹을 분리:

```
/api/verification/runs            (기존)
/api/verification/diffs           (기존)

/api/probe/probes                 (신규)
/api/probe/observations           (신규)
/api/probe/incidents              (신규)
```

## 프론트 (Phase 9 연동)

좌측 탭을 **"정합성"** / **"서비스 헬스"** 로 분리. 서로 다른 독자 (데이터
엔지니어 vs SRE)가 각자의 뷰를 가짐.

## 의존 Phase

- Phase 4 (verification domain) — 없음 (독립)
- Phase 6 (persistence 패턴) — 참고 (같은 gorm + migrate + testcontainers 패턴 재사용)
- Phase 7 (queue) — `RunProbe`도 asynq 태스크로 enqueue (같은 scheduler 재사용)
- Phase 8 (HTTP API) — 신규 리소스 그룹 추가
- Phase 9 (프론트) — 신규 탭

## 미결정 이슈 (Open Items)

- [ ] **에러 분류 체계**: `ErrorClass` enum이 MVP에 충분한가? GraphQL/JSON-RPC
  protocol error의 세분화는?
- [ ] **임계값 설정 방식**: config로 정적 선언 vs API/UI로 런타임 변경. MVP는
  config 정적이 단순.
- [ ] **샘플링 빈도 vs 비용**: 초당 1회 × N엔드포인트 = 일 86400건/엔드포인트.
  자체 indexer는 괜찮지만 3rd-party로 확장 시 budget port 재사용해야 함.
- [ ] **Observation 보관 정책**: 원본 얼마나, rollup 얼마나. 기본값 초안만
  기록했음 — 볼륨 측정 후 Phase 12A 구현 시점에 확정.
- [ ] **Incident 알림 채널**: Slack / webhook / email. 본 context에는 포함하지
  않고 Phase 10 (observability) 와 연계해 외부 alertmanager에 위임 검토.
- [ ] **verification 결과와의 교차 참조**: indexer 에러율이 높을 때 같은 시각의
  verification Discrepancy와 연관이 있는지? → 대시보드 상관관계 뷰. Phase 9
  이후 고려.

## 참고

- [docs/plans/phase-04-verification-diff-domain.md](./phase-04-verification-diff-domain.md) — 기존 도메인과의 경계
- [docs/plans/phase-07-queue-scheduler.md](./phase-07-queue-scheduler.md) — asynq 재사용 지점
