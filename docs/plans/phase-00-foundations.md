# Phase 0 — Foundations

## 목표

코드 없이 **프로젝트 골격·툴체인·CI/로컬 환경**만 준비. 이후 모든 Phase가 올라탈 토대.

## 확정된 메타 정보

| 항목 | 값 |
|---|---|
| Module path | `github.com/seokheejang/chain-sync-watch` |
| Go 최소 버전 | 1.24 (toolchain 1.25 허용) |
| Postgres | 17 |
| Redis | 7.4 |
| Node (frontend) | 22 LTS |
| pnpm | 10.x |
| License | MIT |
| CI | GitHub Actions |
| 커밋 컨벤션 | [Conventional Commits](https://www.conventionalcommits.org/) |
| 기본 브랜치 | `main` + 기능 브랜치 + PR |
| UI 언어 | 영문 기본 |
| 바이너리 이름 | `csw-server`, `csw-worker`, `csw` (CLI) |
| API 인증 (MVP) | 없음 (대시보드 전용) |
| 테스트 러너 | 기본 `go test` |

## 산출물 (Definition of Done)

- [ ] Go 모듈 초기화 (`go.mod` — module `github.com/seokheejang/chain-sync-watch`, go 1.24)
- [ ] 디렉토리 구조 정립 (아래 "구조" 참고)
- [ ] Makefile (`make test`, `make lint`, `make run-server`, `make run-worker`, `make up`, `make down`, `make migrate`, `make openapi`)
- [ ] 린터 / 포매터 설정 (`golangci-lint` + `gofumpt`)
- [ ] `.editorconfig`, `.gitignore` (private/ 포함), `.env.example`
- [ ] `internal/config/defaults.yaml` (embed되는 단일 source of truth, 커밋)
- [ ] `configs/config.example.yaml` (로컬 override 템플릿, 커밋)
- [ ] `docker-compose.yml` (Postgres 17 + Redis 7.4)
- [ ] CI 파이프라인 (GitHub Actions): fmt check / vet / lint / unit test / integration test / build
- [ ] config 로더 패키지 (`internal/config`, `knadh/koanf` — yaml + env 하이브리드)
- [ ] 구조화 로거 골격 (`log/slog`, JSON handler)
- [ ] `CLAUDE.md` 초안 (코드베이스 설명, 테스트 실행법, DDD 경계 규칙)
- [ ] `README.md` 갱신 (프로젝트 개요 + 로컬 개발 시작 방법)

## 구조

```
chain-sync-watch/
├── cmd/
│   ├── csw-server/          HTTP 서버 바이너리 (Go 관례: 디렉토리 이름 = 바이너리 이름)
│   ├── csw-worker/          asynq worker 바이너리
│   └── csw/                 관리 CLI (migrate, openapi-dump 등 서브커맨드)
├── internal/
│   ├── chain/               [Phase 1] 체인 값객체 (순수)
│   ├── source/              [Phase 2] Source 포트 + Capability + Query/Result (추상만, 구체 어댑터 0)
│   │   └── fake/            테스트용 inmem fake
│   ├── verification/        [Phase 4] 순수 도메인
│   ├── diff/                [Phase 4] 순수 도메인
│   ├── application/         [Phase 5] use case + 포트 정의
│   ├── infrastructure/      외부 경계 구현
│   │   ├── persistence/     [Phase 6] gorm + Postgres
│   │   ├── queue/           [Phase 7] asynq + Redis
│   │   └── httpapi/         [Phase 8] chi + huma
│   ├── config/
│   └── observability/
├── adapters/                [Phase 3] 선택적 번들 Source 어댑터 (각자 독립 패키지)
│   ├── rpc/                 JSON-RPC (ethclient 기반)
│   ├── blockscout/          Blockscout v2 REST + Etherscan-호환 proxy
│   └── etherscan/           Etherscan V2 Multichain
├── examples/
│   └── custom-graphql-adapter/
│                            사용자 정의 GraphQL 어댑터 구현 패턴 시연 (익명 스키마)
├── web/                     [Phase 9] Next.js 프론트엔드
├── migrations/              [Phase 6]
├── configs/
│   ├── config.example.yaml  사용자 로컬 오버라이드 템플릿 (커밋)
│   └── config.local.yaml    실사용자 오버라이드 (.gitignore)
├── docs/
│   ├── plans/
│   ├── research/            소스 스키마·형상 조사 기록
│   └── architecture.md      ADR
├── docker-compose.yml
├── Makefile
├── .env.example             비밀 placeholder (커밋)
├── .env                     실제 비밀 (.gitignore)
├── CLAUDE.md
└── README.md
```

**핵심 원칙 (`database/sql` 드라이버 패턴과 동일)**:

- `internal/source/`는 **추상 포트만** — 어떤 구체 어댑터도 import 하지 않음
- 각 `adapters/*/` 는 **독립 Go 모듈 하위 패키지** — 사용자가 원하는 것만 import
- wiring/DI는 **`cmd/csw-server/main.go` (또는 `internal/config/wire.go`) 에서만** (infrastructure boundary)
- 내부 indexer 같은 사용자 고유 소스는 `examples/custom-graphql-adapter/`를 참고해서 **사용자가 자기 repo에 구현** → `Source` 인터페이스 만족하면 plug-in 가능

## 세부 단계

### 0.1 Go 모듈 / 디렉토리
- [ ] `go mod init github.com/seokheejang/chain-sync-watch`
- [ ] `go.mod`에 `go 1.24` 선언
- [ ] 상위 디렉토리 생성 (`cmd/`, `internal/`, `adapters/`, `examples/`, `configs/`, `docs/`, `migrations/`)
- [ ] `.gitignore` 작성:
  - `/tmp`, `/bin`, `/dist`
  - `.env`, `.env.local`
  - `configs/config.local.yaml`
  - `coverage.out`, `coverage.html`
  - `node_modules/`, `.next/`, `out/`
  - `private/` (사용자 로컬 비공개 코드 관리용)
  - IDE: `.idea/`, `.vscode/` (단 `.vscode/settings.json` 공유용이면 예외)

**`private/` 디렉토리 규약**: 사용자 로컬에서 개발 중인 커스텀 어댑터·스크립트·fixture 등. 전체 gitignored. 예: `private/adapters/myindexer/`에서 자기 커스텀 어댑터 개발 → 나중에 별도 private repo로 분리.

### 0.2 툴체인
- [ ] `.tool-versions` 또는 `.mise.toml`:
  - Go 1.24
  - Node 22
  - pnpm 10
- [ ] `.golangci.yml` 작성 (govet, staticcheck, errcheck, gocritic, gosec, revive 최소 + 도메인 import 제약 룰)
- [ ] `Makefile` — 최소 타겟:
  - `make deps` — `go mod download`
  - `make fmt` — `gofumpt -l -w .`
  - `make lint` — `golangci-lint run`
  - `make test` — `go test ./... -race -coverprofile=coverage.out`
  - `make test-integration` — `go test ./... -tags=integration`
  - `make test-e2e` — `go test ./... -tags=e2e`
  - `make up` / `make down` — docker compose 제어
  - `make run-server` / `make run-worker` — 로컬 바이너리 실행
  - `make migrate` — 마이그레이션 적용
  - `make openapi` — OpenAPI 스펙 덤프

### 0.3 로컬 인프라
- [ ] `docker-compose.yml`:
  - `postgres:17` (DB 1개, 비밀번호/DB 이름 env로)
  - `redis:7.4`
  - 볼륨 영속화, healthcheck 포함
- [ ] `.env.example` — 비밀·환경별 오버라이드 placeholder만 (절대 실값 넣지 말 것)

### 0.4 Config 로더 (하이브리드: YAML + env)

**라이브러리**: [`knadh/koanf`](https://github.com/knadh/koanf) — multi-provider 조합 가능, viper보다 경량·깔끔

**로딩 순서**:
1. 내장 default (`internal/config/defaults.yaml`, `go:embed`로 바이너리에 포함 — 단일 source of truth)
2. 선택적 로컬 오버라이드 (`configs/config.local.yaml` — 있으면 병합, `.gitignore`)
3. 환경변수 (`CSW_*` prefix, 이중 언더스코어 `__`로 nesting) — 최종 우선권
4. `.env` 파일은 shell이 자동 export (직접 `godotenv`로 보강 가능)

**비밀/공개 분리 원칙**:
- **공개 가능한 기본값** (체인 목록, endpoint URL, rate limit, timeout 등) → yaml
- **비밀** (DB 비밀번호, API 키, 내부 endpoint) → env only
- **사용자별 커스터마이즈** → `config.local.yaml` (gitignore) 또는 env 오버라이드

#### 작업 체크리스트
- [ ] `internal/config/defaults.yaml` 작성 (embed되는 단일 source of truth, 커밋)
- [ ] `configs/config.example.yaml` 작성 (커밋, 사용자가 `config.local.yaml`로 복사해서 쓰는 템플릿)
- [ ] `configs/config.local.yaml`은 `.gitignore`
- [ ] `internal/config/config.go`:
  - `Config` struct (server, worker, log, chains, adapters, verification, raw_response)
  - `Load(*Options) (*Config, error)` — koanf로 위 로딩 순서 구현
  - `//go:embed defaults.yaml`로 바이너리 embed
  - env 오버라이드: `CSW_` prefix, nested는 `__` delimiter (예: `CSW_ADAPTERS__RPC__ENDPOINTS__10=...`)
- [ ] 필수 필드 누락·타입 오류 시 fail-fast (koanf `k.Unmarshal` + struct 검증)
- [ ] 테스트:
  - default만 로드 → 기본값 검증
  - local 파일 병합 → override 적용 확인
  - env override → 우선순위 확인
  - 필수 필드 누락 → 명확한 에러

#### `internal/config/defaults.yaml` (embed되는 단일 source of truth)

바이너리에 `//go:embed`로 포함되어 런타임에 디스크 파일 의존 없이 동작. 사용자 override는 `configs/config.local.yaml` 또는 env(`CSW_*`)로 주입.

실제 내용은 [internal/config/defaults.yaml](../../internal/config/defaults.yaml) 참조 (commit 후 링크 유효).

#### `.env.example` (비밀 placeholder)

```bash
# Database
DATABASE_URL=postgres://csw:csw@localhost:5432/csw?sslmode=disable

# Redis
REDIS_URL=redis://localhost:6379/0

# Etherscan (선택 — 키 있으면 enabled=true로)
CSW_ADAPTERS__ETHERSCAN__API_KEY=
# CSW_ADAPTERS__ETHERSCAN__ENABLED=true

# yaml 값 오버라이드 예시 (이중 underscore `__` = nesting, 단일 `_`는 키 내부 유지)
# CSW_SERVER__ADDR=:9090
# CSW_ADAPTERS__RPC__ENDPOINTS__10=https://your-private-rpc.example.com

# 사용자 custom 어댑터 endpoint (예시; 실제 URL은 공개 금지)
# CUSTOM_ADAPTER_GRAPHQL_ENDPOINT=
```

#### `configs/config.example.yaml` (로컬 템플릿)

```yaml
# Copy to config.local.yaml and edit. config.local.yaml is gitignored.
# Overrides config.default.yaml; env still takes final precedence.

# adapters:
#   rpc:
#     endpoints:
#       10: "https://your-alternate-rpc.example.com"
#     archive: true

# chains:
#   - id: 10
#     slug: optimism
#     display_name: "Optimism"
#   - id: 1
#     slug: ethereum
#     display_name: "Ethereum"
```

### 0.5 로거
- [ ] `internal/observability/logger.go` — `slog.New(slog.NewJSONHandler(...))`
- [ ] context에서 request id 뽑아 로그에 attach하는 helper

### 0.6 CI
- [ ] `.github/workflows/ci.yml`:
  - Job 1: `fmt-check` (gofumpt), `lint` (golangci-lint)
  - Job 2: `test-unit` (Go 1.24, race, coverage)
  - Job 3: `test-integration` (testcontainers-go, 필요 서비스 자동 기동)
  - Job 4: `build` (cross-compile check)
  - 공통: 캐시 (`actions/setup-go` + `actions/cache`)
  - 커버리지 업로드 (codecov 선택)
- [ ] `.github/workflows/frontend.yml`:
  - Node 22 + pnpm 10
  - `biome check`, `type-check`, `build`
- [ ] Conventional Commits 검증 (선택): `commitlint` in PR check
- [ ] PR 템플릿 (`.github/pull_request_template.md`) — 간단한 체크리스트만

### 0.7 문서
- [ ] `CLAUDE.md` — Claude가 이 repo에서 작업할 때 알아야 할 내용 (디렉토리 역할, 테스트 명령, DDD 경계 규칙)
- [ ] `README.md` — 프로젝트 개요 + Quick Start

## 의존 Phase

없음. **가장 먼저 완료해야 함.**

## TDD 체크

Phase 0에선 **코드가 거의 없으므로 테스트도 최소**. 다만:
- `internal/config` 패키지에 load 테스트는 이 Phase에서 짧게라도 작성 (TDD 근육 기르기)

## 참고

- [gofumpt](https://github.com/mvdan/gofumpt)
- [golangci-lint](https://golangci-lint.run/)
- [knadh/koanf](https://github.com/knadh/koanf) — config 로더
- [joho/godotenv](https://github.com/joho/godotenv) — .env 로딩 (선택)
- [log/slog 공식 문서](https://pkg.go.dev/log/slog)
