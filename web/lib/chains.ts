// chainName renders a human label for a numeric chain id. Mirrors
// the display names in internal/config/defaults.yaml `chains:` so
// the UI stays in sync without a round-trip to fetch the catalogue.
// Unknown ids fall back to "Chain <id>" rather than throwing —
// operators adding new chains should see them listed right away
// even before this map is updated.
const CHAIN_LABELS: Record<number, string> = {
  10: "Optimism",
  1: "Ethereum",
};

export function chainName(id: number): string {
  return CHAIN_LABELS[id] ?? `Chain ${id}`;
}

// chainLabel renders "<name> (id)" for tables and detail headers
// where both pieces of information are useful.
export function chainLabel(id: number): string {
  const name = CHAIN_LABELS[id];
  if (!name) return `Chain ${id}`;
  return `${name} (${id})`;
}
