import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

// Dashboard stub — real cards (recent runs, unresolved critical diffs,
// source health) land in the Phase 9.5+ sessions. For now this page
// exists to prove the layout, providers, and navigation work.
export default function DashboardPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
        <p className="text-sm text-muted-foreground">
          Cross-source verification at a glance. Cards hook into live data in the next pass.
        </p>
      </div>
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader>
            <CardTitle>Recent runs</CardTitle>
            <CardDescription>Latest verification jobs</CardDescription>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Wire in list-runs + timeline in Phase 9.5.
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Unresolved critical</CardTitle>
            <CardDescription>Discrepancies awaiting review</CardDescription>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Wire in list-diffs (severity=critical) in Phase 9.8.
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Source health</CardTitle>
            <CardDescription>Adapter capability matrix</CardDescription>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Wire in list-sources with Tier column in Phase 9.9.
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
