"use client";

import { EmptyState } from "@/components/shared/empty-state";
import { TierBadge } from "@/components/shared/status-badge";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useSources } from "@/lib/api/hooks";

// Stub /sources — lists the adapter capability matrix for a single
// chain. The full multi-chain selector + capability filtering lands
// in Phase 9.9. Optimism mainnet (chainId=10) is the MVP target so
// it doubles as the default.
const DEFAULT_CHAIN_ID = 10;

export default function SourcesPage() {
  const { data, isLoading, isError, error } = useSources(DEFAULT_CHAIN_ID);
  const items = data?.items ?? [];

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Sources</h1>
        <p className="text-sm text-muted-foreground">
          Adapter capability matrix for chain {DEFAULT_CHAIN_ID} (Optimism mainnet).
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Capability matrix</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-2">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : isError ? (
            <EmptyState
              title="Couldn't reach the API"
              description={
                error instanceof Error ? error.message : "Check NEXT_PUBLIC_API_BASE_URL."
              }
            />
          ) : items.length === 0 ? (
            <EmptyState
              title="No sources configured"
              description="The SourceGateway is currently a stub (Phase 10 wires real adapters from config)."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Source</TableHead>
                  <TableHead>Chain</TableHead>
                  <TableHead>Capabilities</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((src) => (
                  <TableRow key={src.id}>
                    <TableCell className="font-mono text-xs">{src.id}</TableCell>
                    <TableCell className="font-mono text-xs">{src.chain_id}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {(src.capabilities ?? []).map((cap) => (
                          <div key={cap.name} className="flex items-center gap-1">
                            <Badge variant="outline" className="font-mono text-xs">
                              {cap.name}
                            </Badge>
                            <TierBadge value={cap.tier} />
                          </div>
                        ))}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
