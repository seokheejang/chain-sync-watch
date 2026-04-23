import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

// StatusBadge renders a run status or diff severity with a stable
// colour mapping the dashboard reuses everywhere. Unknown values
// fall back to the neutral palette rather than crashing — the
// backend can add new statuses without the frontend going red.
type Variant = "pending" | "running" | "completed" | "failed" | "cancelled" | "neutral";

const palette: Record<Variant, string> = {
  pending: "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200",
  running: "bg-sky-100 text-sky-900 dark:bg-sky-950 dark:text-sky-200",
  completed: "bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200",
  failed: "bg-rose-100 text-rose-900 dark:bg-rose-950 dark:text-rose-200",
  cancelled: "bg-zinc-100 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-200",
  neutral: "bg-muted text-muted-foreground",
};

export function StatusBadge({ value }: { value: string | undefined }) {
  const key = (value ?? "").toLowerCase();
  const variant: Variant =
    key === "pending" ||
    key === "running" ||
    key === "completed" ||
    key === "failed" ||
    key === "cancelled"
      ? key
      : "neutral";

  return (
    <Badge variant="outline" className={cn("rounded-full border-0", palette[variant])}>
      {value || "—"}
    </Badge>
  );
}

// SeverityBadge maps diff.Severity to the severity-specific palette.
// Kept separate from StatusBadge because an operator sees both in
// the same table (run status vs diff severity) — mixing the colour
// scales would confuse "failed run" with "critical diff".
const severityPalette: Record<string, string> = {
  critical: "bg-rose-100 text-rose-900 dark:bg-rose-950 dark:text-rose-200",
  warning: "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200",
  info: "bg-sky-100 text-sky-900 dark:bg-sky-950 dark:text-sky-200",
};

export function SeverityBadge({ value }: { value: string | undefined }) {
  const key = (value ?? "").toLowerCase();
  const tone = severityPalette[key] ?? "bg-muted text-muted-foreground";
  return (
    <Badge variant="outline" className={cn("rounded-full border-0 uppercase", tone)}>
      {value || "—"}
    </Badge>
  );
}

// TierBadge highlights a diff's metric tier. Tier A = RPC-canonical
// truth, Tier B = indexer-sampled, Tier C = mixed. Colours match
// the diff detail page's Tier context note convention.
const tierPalette: Record<string, string> = {
  A: "bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200",
  B: "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200",
  C: "bg-sky-100 text-sky-900 dark:bg-sky-950 dark:text-sky-200",
};

export function TierBadge({ value }: { value: string | undefined }) {
  const tone = tierPalette[value ?? ""] ?? "bg-muted text-muted-foreground";
  return (
    <Badge variant="outline" className={cn("rounded-full border-0", tone)}>
      Tier {value || "?"}
    </Badge>
  );
}
