// Fallback chain labels used when the /chains API is unreachable or
// has not loaded yet. Keep the list short — the authoritative
// catalog lives in internal/config/defaults.yaml and is served via
// GET /chains. Unknown ids render as "Chain <id>".
const FALLBACK_LABELS: Record<number, string> = {
  1: "Ethereum",
  10: "Optimism",
  8453: "Base",
  42161: "Arbitrum",
  11155111: "Sepolia",
};

export function chainName(id: number): string {
  return FALLBACK_LABELS[id] ?? `Chain ${id}`;
}

// chainLabel renders "<name> (id)" for tables and detail headers
// where both pieces of information are useful.
export function chainLabel(id: number): string {
  const name = FALLBACK_LABELS[id];
  if (!name) return `Chain ${id}`;
  return `${name} (${id})`;
}
