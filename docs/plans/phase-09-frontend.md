# Phase 9 — Frontend (Next.js 15 + shadcn/ui + TanStack Query)

## 목표

검증 도구의 최소 UI: **job 대시보드 / diff 뷰 / 블록 드릴다운**. OpenAPI 스펙에서 **타입 + API 클라이언트 자동 생성**. "AI 바이브 코딩"에서 핫한 스택으로 DX 극대화.

## 스택

| 항목 | 선택 |
|---|---|
| Framework | **Next.js 15** (App Router, SSR 선호) |
| UI | **shadcn/ui** + Radix UI primitives |
| 스타일 | **Tailwind CSS v4** |
| 서버 상태 | **TanStack Query v5** |
| 타입 생성 | **openapi-typescript** (정적 타입) + **Hey API** 또는 **orval** (클라이언트 + react-query hooks 생성) |
| 폼 | **react-hook-form** + **zod** |
| 테이블 | **TanStack Table v8** |
| 차트 (선택) | **Recharts** or **visx** |
| 아이콘 | **lucide-react** |
| 다크모드 | **next-themes** |
| Node | **22 LTS** |
| 패키지 매니저 | **pnpm 10** |
| 린트/포맷 | **Biome** (ESLint+Prettier 대체, 요즘 추세) |
| UI 언어 | **영문 기본** (i18n 확장 고려는 post-MVP) |

## 산출물 (DoD)

- [ ] `web/` 디렉토리 — Next.js 15 App Router 프로젝트
- [ ] shadcn/ui 초기 설치 + 기본 컴포넌트 세트 (button, card, table, dialog, toast 등)
- [ ] API 클라이언트 자동 생성 파이프라인 (`pnpm run gen:api`)
- [ ] 페이지 라우트
  - `/` — 대시보드 (최근 run, 최근 diff, 소스 상태, Snapshot 비교 카드)
  - `/runs` — run 목록 + 필터 (metric category 포함)
  - `/runs/[id]` — run 상세 + 그 run의 diff 목록 (Tier A 전수 결과 vs Tier B 샘플링 결과 분리 표시)
  - `/diffs` — 전체 diff 목록 (카테고리별 탭: BlockImmutable / AddressLatest / AddressAtBlock / Snapshot) + **Tier 필터 (A/B/C)**
  - `/diffs/[id]` — diff 상세 (소스별 값 비교 테이블 + **ReflectedBlock 표시**, **anchor window 시각화**, **Tier 배지**, **SamplingSeed**로 재현 안내)
  - `/schedules` — 스케줄 관리
  - `/sources` — 연결된 소스·Capability 매트릭스 (필드 단위 + **Tier 컬럼**)
  - `/runs/new` — 새 run 생성 폼 (Metric 선택 시 카테고리별로 그룹핑 + Tier B Metric 선택 시 샘플링 stratum·예산 경고)
- [ ] 공용 레이아웃 (헤더, 사이드바, 다크모드 토글)
- [ ] 에러 바운더리 + 로딩 스켈레톤
- [ ] docker-compose에 web 서비스 추가 (선택)

## 디렉토리 구조

```
web/
├── app/
│   ├── layout.tsx
│   ├── page.tsx                    대시보드
│   ├── runs/
│   │   ├── page.tsx                목록
│   │   ├── new/page.tsx            생성 폼
│   │   └── [id]/page.tsx           상세
│   ├── diffs/
│   │   ├── page.tsx
│   │   └── [id]/page.tsx
│   ├── schedules/page.tsx
│   ├── sources/page.tsx
│   └── api/                        (필요 시 BFF)
├── components/
│   ├── ui/                         shadcn 컴포넌트
│   ├── runs/
│   ├── diffs/
│   └── shared/                     공통(Pagination, StatusBadge, EmptyState)
├── lib/
│   ├── api/                        자동 생성된 타입·클라이언트 (gitignore 또는 commit)
│   ├── query-client.tsx
│   └── utils.ts
├── hooks/
├── styles/
├── openapi.json                    backend에서 가져온 spec (commit)
├── biome.json
├── tailwind.config.ts
├── tsconfig.json
├── next.config.mjs
└── package.json
```

## 자동 생성 파이프라인

```json
// web/package.json (일부)
"scripts": {
  "dev": "next dev --turbopack",
  "build": "next build",
  "start": "next start",
  "lint": "biome check .",
  "gen:api": "openapi-typescript ./openapi.json -o ./lib/api/schema.ts && openapi-fetch-gen ..."
}
```

**워크플로우**:
1. 백엔드에서 `make openapi` → `openapi.json` 최신화
2. `cp ../backend/openapi.json web/openapi.json` (또는 빌드 시 fetch)
3. `pnpm gen:api` → 타입·클라이언트 재생성
4. TypeScript 에러로 계약 위반 즉시 감지

## 주요 페이지 설계

### `/` 대시보드
- 카드 3종: "최근 Run", "미해결 Critical diff", "소스 헬스"
- 시각적 요약 (차트 1~2개)

### `/runs`
- TanStack Table — status, chain, trigger, created_at, finished_at, diff count
- 필터: status, chain, 날짜 range
- 페이지네이션

### `/runs/[id]`
- 상단: run 메타 + 취소 버튼
- 하단 tab:
  - "Diffs" (TanStack Table)
  - "Details" (sampling, metrics, raw JSON 보기)

### `/diffs/[id]`
- 소스별 값을 **나란히 표시하는 비교 테이블**
  - 컬럼에 `reflected_block` 추가 (Tier B/C 응답의 실제 반영 블록 표시)
  - anchor block과 reflected_block 차이를 "Δ +12 blocks" 형태로 시각화
- severity 배지 + **Tier 배지** (A=초록 · B=노랑 · C=파랑)
- anchor window(tol_back/tol_fwd) 시각 표시 — 이 범위 밖으로 벗어난 샘플은 "discarded"로 별도 표기
- replay 버튼 — Tier B 재현 시 `SamplingSeed` 그대로 전달됨 안내
- (선택) 해당 블록을 체인 익스플로러로 가는 링크
- Tier A diff: "RPC 전수 비교 결과" 컨텍스트 노트
- Tier B diff: "3rd-party 샘플링 비교 결과 · seed=N · anchor=block #M" 컨텍스트 노트

### `/runs/new`
- react-hook-form + zod
- 폼 필드: chain / metrics (카테고리별 그룹 multi-select) / sampling strategy (switch) / trigger (switch)
- Metric 선택 UX: 카테고리 아코디언 (BlockImmutable / AddressLatest / AddressAtBlock / Snapshot) — 카테고리별 설명·지원 소스 미니 표 함께
- AddressAtBlock 카테고리 선택 시 "archive RPC 필요" 경고 표시
- Snapshot 카테고리 선택 시 "관찰용, 자동 판정 없음" 주의 표시
- 각 strategy 선택 시 동적 필드 (fixed list / N / range+count / range+step)

## 세부 단계

### 9.1 Next.js 15 부트스트랩
- [ ] Node 22 / pnpm 10 확인
- [ ] `pnpm create next-app@latest web --ts --app --tailwind --no-src-dir --use-pnpm`
- [ ] shadcn/ui init (`pnpm dlx shadcn@latest init`)
- [ ] 기본 컴포넌트 설치 (`button card input dialog dropdown-menu table toast tabs badge skeleton`)
- [ ] Biome 설치·설정
- [ ] next-themes 다크모드

### 9.2 API 타입 생성 세팅
- [ ] openapi-typescript 설치
- [ ] `gen:api` 스크립트 작성
- [ ] `lib/api/client.ts` — fetch wrapper + base URL config
- [ ] 생성 파일 커밋 정책 결정 (커밋 권장: review 가능, reproducibility)

### 9.3 TanStack Query 세팅
- [ ] QueryClientProvider
- [ ] devtools 개발 환경에만

### 9.4 레이아웃 / 공용 컴포넌트
- [ ] 헤더 + 사이드바
- [ ] StatusBadge (status/severity 색 통일)
- [ ] Pagination, EmptyState, ErrorBoundary

### 9.5 `/runs` 페이지
- [ ] 목록 query hook
- [ ] 필터·페이징
- [ ] 행 클릭 → 상세

### 9.6 `/runs/[id]` 페이지
- [ ] run 상세, diffs tab
- [ ] cancel mutation

### 9.7 `/runs/new` 페이지
- [ ] zod 스키마 (백엔드 OpenAPI 스펙과 정렬)
- [ ] 각 strategy별 동적 필드
- [ ] 생성 성공 시 `/runs/[id]`로 이동

### 9.8 `/diffs` + `/diffs/[id]`
- [ ] 목록 + 상세 + replay mutation

### 9.9 `/schedules` / `/sources`
- [ ] CRUD 및 Capability 매트릭스 뷰

### 9.10 통합 & 배포
- [ ] next build + runtime env (NEXT_PUBLIC_API_BASE_URL)
- [ ] docker-compose에 web 서비스 (선택)

## 의존 Phase

- Phase 8 (HTTP API + OpenAPI spec 서빙)

## 주의

- **SSR ↔ TanStack Query**: App Router + TanStack은 prefetch + dehydrate 패턴. 공식 가이드 참조.
- **인증**: 백엔드 MVP가 인증 미포함이면 프론트도 생략. 추후 auth.js (NextAuth v5)로.
- **환경 변수**: `NEXT_PUBLIC_API_BASE_URL`만 클라이언트 노출. 비밀은 서버 컴포넌트에서만.
- **실시간**: 초기엔 polling (TanStack `refetchInterval`)으로 충분. Phase 10에서 SSE/WebSocket 고려.
- **접근성**: shadcn/ui 기반이므로 Radix 접근성 자동 상속. 그래도 form label 명시.
- **번들 크기**: 아이콘·날짜 라이브러리는 tree-shaking 확인 (dayjs/date-fns 택1)

## 참고

- [Next.js 15 App Router](https://nextjs.org/docs)
- [shadcn/ui](https://ui.shadcn.com/)
- [TanStack Query v5](https://tanstack.com/query/v5)
- [openapi-typescript](https://openapi-ts.dev/)
- [Hey API](https://heyapi.dev/)
- [Biome](https://biomejs.dev/)
