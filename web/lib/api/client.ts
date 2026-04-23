import createClient from "openapi-fetch";
import type { paths } from "@/lib/api/schema";

// Single csw-server HTTP API client keyed to the generated OpenAPI
// types. Every hook / route handler calls through `api` so a
// contract drift surfaces as a TypeScript error after `pnpm gen:api`,
// not at runtime against a stale shape.
//
// NEXT_PUBLIC_API_BASE_URL is read at module init. If unset we leave
// baseUrl undefined, which lets the browser fall back to relative
// URLs — useful when the frontend is served behind a reverse proxy
// that rewrites /diffs etc. to the backend.
const baseUrl = process.env.NEXT_PUBLIC_API_BASE_URL || undefined;

export const api = createClient<paths>({ baseUrl });

// Re-export the component schemas (DiffView / RunView / etc.) as a
// flat namespace so callers can write `Schemas.RunView` instead of
// digging through the nested OpenAPI types.
export type Schemas = import("@/lib/api/schema").components["schemas"];
