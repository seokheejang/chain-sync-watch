# Phase 6 — Persistence (Postgres + gorm)

## 목표

`RunRepository` / `DiffRepository` 포트의 **Postgres + gorm 구현체**와 마이그레이션. **testcontainers-go 기반 통합 테스트**로 실동작 검증.

## 산출물 (DoD)

- [ ] `migrations/` — 마이그레이션 파일 (golang-migrate 형식)
- [ ] `internal/infrastructure/persistence/models.go` — gorm 모델 (도메인과 분리)
- [ ] `internal/infrastructure/persistence/run_repository.go`
- [ ] `internal/infrastructure/persistence/diff_repository.go`
- [ ] `internal/infrastructure/persistence/mapper.go` — 도메인 ↔ gorm 모델 변환
- [ ] 통합 테스트 (testcontainers-go, `-tags=integration` 또는 TestMain 기반)
- [ ] 마이그레이션 CLI — `csw migrate up/down/status` 서브커맨드 (`cmd/csw/` 통합) + `make migrate` 래퍼

## 설계 원칙

### 도메인 ↔ 퍼시스턴스 모델 분리

```go
// internal/verification/run.go (도메인)
type Run struct { /* private fields */ }

// internal/infrastructure/persistence/models.go (gorm)
type runModel struct {
    ID         string    `gorm:"primaryKey"`
    ChainID    uint64
    Status     string
    // gorm 태그 / 인덱스 / 제약은 여기만
}
```

**왜 분리**:
- gorm 태그가 도메인을 오염시키지 않음
- 도메인 모델 바꿀 때 DB 스키마 독립적 변경 가능
- DDD 철학 관철

**Mapper**:
```go
func toRunModel(r *verification.Run) runModel
func toDomainRun(m runModel) (*verification.Run, error)
```

### 스키마 초안

```sql
-- migrations/001_init.up.sql
CREATE TABLE runs (
    id            TEXT PRIMARY KEY,
    chain_id      BIGINT NOT NULL,
    status        TEXT NOT NULL,
    trigger_type  TEXT NOT NULL,   -- manual | scheduled | realtime
    trigger_data  JSONB NOT NULL,  -- user, cron expr, block, etc.
    strategy_kind TEXT NOT NULL,   -- fixed_list | latest_n | random | sparse_steps
    strategy_data JSONB NOT NULL,
    metrics       TEXT[] NOT NULL,
    error_msg     TEXT,
    created_at    TIMESTAMPTZ NOT NULL,
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ
);
CREATE INDEX idx_runs_chain_status ON runs(chain_id, status);
CREATE INDEX idx_runs_created_at ON runs(created_at DESC);

CREATE TABLE discrepancies (
    id             BIGSERIAL PRIMARY KEY,
    run_id         TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    metric         TEXT NOT NULL,
    block_number   BIGINT NOT NULL,
    subject_type   TEXT NOT NULL,     -- block | address | contract
    subject_addr   BYTEA,              -- 20 bytes (nullable)
    values         JSONB NOT NULL,     -- { source_id: { raw, typed, fetched_at, reflected_block } }
    severity       TEXT NOT NULL,
    trusted_sources TEXT[] NOT NULL,
    reasoning      TEXT,
    resolved       BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_at    TIMESTAMPTZ,
    detected_at    TIMESTAMPTZ NOT NULL,
    -- 2026-04-20 추가 — Tier/Anchor 메타 (phase-02-source-ports.md Phase 2C)
    tier           SMALLINT,           -- 1=A, 2=B, 3=C (nullable: 기존 row 호환)
    anchor_block   BIGINT,             -- Run이 고정한 anchor block (보통 finalized)
    sampling_seed  BIGINT              -- Tier B 샘플링 재현용 (Random strategy seed)
);
CREATE INDEX idx_disc_run ON discrepancies(run_id);
CREATE INDEX idx_disc_metric_block ON discrepancies(metric, block_number);
CREATE INDEX idx_disc_severity ON discrepancies(severity) WHERE NOT resolved;
CREATE INDEX idx_disc_tier ON discrepancies(tier) WHERE tier IS NOT NULL;
```

`values` JSONB 스키마 갱신: `{source_id: {raw, typed, fetched_at, reflected_block}}` — Tier B anchor window 사후 재판정 가능하도록 `reflected_block` 포함.

**결정 포인트**:
- `run` 테이블에 policy/strategy 원본을 `JSONB`로 보관 (스키마 변경 유연)
- `discrepancies.values`도 JSONB — 소스별 raw value를 그대로 (감사용)
- Soft delete 미사용, run 삭제 시 관련 diff 함께 cascade
- `address`는 `BYTEA` 20바이트로 저장 (문자열보다 공간·인덱스 효율)

## 세부 단계

### 6.1 마이그레이션 도구
- [ ] `golang-migrate/migrate` 선택 (커뮤니티 표준, CLI + Go 임베드 둘 다 가능)
- [ ] `migrations/001_init.{up,down}.sql` 작성
- [ ] `cmd/csw/` CLI에 `migrate up` / `migrate down` / `migrate status` 서브커맨드 추가 (cobra 또는 flag 기반). `csw` 단일 CLI 바이너리에 통합 — 별도 `cmd/migrate/` 바이너리를 만들지 않음 (Phase 0 네이밍 규약).
- [ ] `make migrate` → `go run ./cmd/csw migrate up` 래퍼

### 6.2 gorm 모델 + 매퍼
- [ ] 테스트: round-trip 매핑 (domain → model → domain 후 동등성)
- [ ] 구현

### 6.3 `RunRepository` 구현
- [ ] testcontainers-go로 Postgres 띄우는 TestMain
- [ ] 테스트: Save(신규·갱신), FindByID(존재·부재), List(필터·페이지), 상태 전이 반영, 동시 업데이트 시 낙관적 락 여부 결정
- [ ] 구현

### 6.4 `DiffRepository` 구현
- [ ] 테스트: Save, FindByRun, FindByID, resolved 업데이트
- [ ] 구현

### 6.5 트랜잭션 경계
- [ ] Application이 `UnitOfWork`를 쓸지, repository 안에서 원자성 책임질지 결정
- [ ] 초기 권장: repository 메서드 하나가 원자적 단위. UoW는 필요 시 도입.

### 6.6 시드 데이터 / 개발용 픽스처
- [ ] 로컬 개발용 `make seed` (옵션)

## 의존 Phase

- Phase 4 (도메인)
- Phase 5 (포트 — 구현 대상)

## 주의

- **gorm pitfalls**:
  - `AutoMigrate` 프로덕션에서 쓰지 말 것 (엄격한 마이그레이션만 사용)
  - soft delete 의도 없는데 `gorm.Model` 통째 상속 금지 (deleted_at 들어옴)
  - `[]byte` vs `string`, 숫자 타입 (BIGINT vs INTEGER) 주의
- **Address 저장**: `[20]byte` → `BYTEA`. 쿼리 시 hex 변환은 repo 레이어 책임.
- **`*big.Int` balance 저장**: `NUMERIC(78,0)` 컬럼에 string으로. 또는 JSONB values 안에 문자열.
- **인덱스 재검토**: 프론트의 실제 쿼리 패턴 확정(Phase 9) 후 인덱스 추가·조정.

## 테스트 전략

- **단위**: 매퍼 (빠름, 많이)
- **통합**: repository 구현 (testcontainers-go, 상대적으로 느림) — 빌드 태그 `integration` 또는 `short` 검사로 분리
- **E2E**: Phase 10

## 참고

- [gorm 공식 문서](https://gorm.io/docs/)
- [golang-migrate](https://github.com/golang-migrate/migrate)
- [testcontainers-go](https://golang.testcontainers.org/)
- [Postgres JSONB 쿼리](https://www.postgresql.org/docs/current/datatype-json.html)
