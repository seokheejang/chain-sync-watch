# Dockerfile — Next.js 16 frontend (standalone output).
#
# Three-stage build:
#
#   1. deps   — install pnpm + package lock; cached between builds.
#   2. build  — run `pnpm build` using the cached node_modules.
#   3. runner — minimal alpine with the standalone output + static
#               assets + public/. Runs as a non-root user.
#
# NEXT_PUBLIC_API_BASE_URL is baked in at build time (Next.js
# inlines NEXT_PUBLIC_* into the bundle). Production deployments
# behind a reverse proxy should either set this to a path like
# `/api` or omit it so the frontend uses relative URLs — either way
# the proxy rewrites to the csw-server upstream.

# syntax=docker/dockerfile:1.7
ARG NODE_VERSION=22-alpine

# --- deps ------------------------------------------------------------

FROM node:${NODE_VERSION} AS deps

WORKDIR /app

# pnpm 10 is pinned in package.json's packageManager? Not here —
# corepack picks up the lockfile and activates the right version.
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./

RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

# --- build -----------------------------------------------------------

FROM node:${NODE_VERSION} AS build

WORKDIR /app

ARG NEXT_PUBLIC_API_BASE_URL
ENV NEXT_PUBLIC_API_BASE_URL=${NEXT_PUBLIC_API_BASE_URL}
ENV NEXT_TELEMETRY_DISABLED=1

RUN corepack enable

COPY --from=deps /app/node_modules ./node_modules
COPY . .

RUN pnpm build

# --- runner ----------------------------------------------------------

FROM node:${NODE_VERSION} AS runner

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

# Non-root user — Next.js standalone cares only that the writable
# paths (none, in our case) are owned correctly.
RUN addgroup -g 1001 -S nextjs \
    && adduser -S -u 1001 -G nextjs nextjs

COPY --from=build --chown=nextjs:nextjs /app/.next/standalone ./
COPY --from=build --chown=nextjs:nextjs /app/.next/static ./.next/static
COPY --from=build --chown=nextjs:nextjs /app/public ./public

USER nextjs

EXPOSE 3000

ENV HOSTNAME=0.0.0.0
ENV PORT=3000

CMD ["node", "server.js"]
