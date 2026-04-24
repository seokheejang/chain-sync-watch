"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";

import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Form,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useChainSourceCounts, useChains, useCreateSchedule } from "@/lib/api/hooks";

const METRICS = [
  { key: "block.hash", category: "BlockImmutable" },
  { key: "block.parent_hash", category: "BlockImmutable" },
  { key: "block.state_root", category: "BlockImmutable" },
  { key: "block.receipts_root", category: "BlockImmutable" },
  { key: "block.transactions_root", category: "BlockImmutable" },
  { key: "block.timestamp", category: "BlockImmutable" },
  { key: "block.tx_count", category: "BlockImmutable" },
  { key: "block.gas_used", category: "BlockImmutable" },
  { key: "block.miner", category: "BlockImmutable" },
  { key: "address.balance_latest", category: "AddressLatest" },
  { key: "address.nonce_latest", category: "AddressLatest" },
  { key: "address.tx_count_latest", category: "AddressLatest" },
  { key: "address.balance_at_block", category: "AddressAtBlock" },
  { key: "address.nonce_at_block", category: "AddressAtBlock" },
  { key: "address.erc20_balance_latest", category: "AddressLatest" },
  { key: "address.erc20_holdings_latest", category: "AddressLatest" },
] as const;

// MVP schema mirrors /runs/new: latest_n sampling + manual-style
// metric picker, plus the cron expression this page adds on top. The
// fuller union-typed sampling, address plans, and token plans remain
// follow-ups in both forms.
const formSchema = z.object({
  chain_id: z.coerce.number().int().min(1, "Chain id is required"),
  metrics: z.array(z.string()).min(1, "Pick at least one metric"),
  latest_n: z.coerce.number().int().min(1).max(1000),
  // asynq's scheduler (robfig/cron v3) expects a 5-field spec:
  // minute hour day-of-month month day-of-week. Six-field forms
  // that include a leading seconds column are rejected at
  // registration time and the schedule silently never fires.
  cron_expr: z
    .string()
    .trim()
    .min(1, "Cron expression is required")
    .refine((v) => v.split(/\s+/).length === 5, "Must be 5 fields: minute hour dom month dow"),
  timezone: z.string().trim().optional(),
});

type FormValues = z.infer<typeof formSchema>;

export function NewScheduleForm() {
  const router = useRouter();
  const create = useCreateSchedule();
  const { data: chainsData } = useChains();
  const chains = chainsData?.items ?? [];
  const { counts: chainSourceCounts } = useChainSourceCounts(chains.map((c) => c.id));
  const configuredChains = chains.filter((c) => (chainSourceCounts[c.id] ?? 0) > 0);

  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      chain_id: 10,
      metrics: ["block.hash"],
      latest_n: 3,
      cron_expr: "*/5 * * * *",
      timezone: "UTC",
    },
  });

  const onSubmit = (values: FormValues) => {
    create.mutate(
      {
        chain_id: values.chain_id,
        metrics: values.metrics,
        sampling: {
          kind: "latest_n",
          latest_n: { n: values.latest_n },
        },
        schedule: {
          cron_expr: values.cron_expr,
          timezone: values.timezone || undefined,
        },
      },
      {
        onSuccess: (data) => {
          toast.success(`Schedule created — ${data?.job_id}`);
          router.push("/schedules");
        },
        onError: (err) =>
          toast.error(err instanceof Error ? err.message : "Create schedule failed"),
      },
    );
  };

  const selected = form.watch("metrics") || [];
  const toggleMetric = (key: string) => {
    const next = selected.includes(key) ? selected.filter((k) => k !== key) : [...selected, key];
    form.setValue("metrics", next, { shouldValidate: true });
  };

  return (
    <div className="max-w-3xl space-y-4">
      <div>
        <Link
          href="/schedules"
          className={buttonVariants({ variant: "ghost", size: "sm" }) + " mb-2"}
        >
          <ArrowLeft className="mr-2 h-4 w-4" /> Schedules
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">New schedule</h1>
        <p className="text-sm text-muted-foreground">
          Registers a recurring verification job. Each tick materialises a Run with the cron
          expression as its trigger.
        </p>
      </div>

      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Target chain</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              <FormField
                control={form.control}
                name="chain_id"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Chain</FormLabel>
                    <select
                      className="flex h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm disabled:cursor-not-allowed disabled:opacity-50"
                      value={field.value}
                      onChange={(e) => field.onChange(Number(e.target.value))}
                      disabled={configuredChains.length === 0}
                    >
                      {configuredChains.length === 0 ? (
                        <option value={field.value}>— no configured chains —</option>
                      ) : (
                        configuredChains.map((c) => (
                          <option key={c.id} value={c.id}>
                            {c.display_name} ({c.id})
                          </option>
                        ))
                      )}
                    </select>
                    <FormDescription>
                      Only chains with at least one source row are listed. Configure adapters under{" "}
                      <code className="font-mono">/sources</code> first.
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="latest_n"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Latest N blocks</FormLabel>
                    <Input type="number" min={1} max={1000} {...field} />
                    <FormDescription>How many trailing blocks from tip per tick.</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Cadence</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              <FormField
                control={form.control}
                name="cron_expr"
                render={({ field }) => (
                  <FormItem className="sm:col-span-2">
                    <FormLabel>Cron expression</FormLabel>
                    <Input placeholder="*/5 * * * *" {...field} />
                    <FormDescription>
                      5-field cron (minute, hour, dom, month, dow). Example {' "*/5 * * * *" '}
                      fires every 5 minutes. Seconds are not supported.
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="timezone"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Timezone (optional)</FormLabel>
                    <Input placeholder="UTC" {...field} />
                    <FormDescription>IANA name; empty = UTC.</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Metrics</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <FormField
                control={form.control}
                name="metrics"
                render={() => (
                  <FormItem>
                    {Object.entries(
                      Object.groupBy(METRICS, (m) => m.category) as Record<
                        string,
                        (typeof METRICS)[number][]
                      >,
                    ).map(([category, metrics]) => (
                      <div key={category} className="space-y-2">
                        <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                          {category}
                        </div>
                        <div className="grid gap-2 sm:grid-cols-2">
                          {metrics.map((m) => (
                            <label
                              key={m.key}
                              className="flex cursor-pointer items-center gap-2 rounded-md border p-2 text-sm hover:bg-muted/30"
                            >
                              <input
                                type="checkbox"
                                className="size-4 accent-primary"
                                checked={selected.includes(m.key)}
                                onChange={() => toggleMetric(m.key)}
                              />
                              <span className="font-mono text-xs">{m.key}</span>
                            </label>
                          ))}
                        </div>
                      </div>
                    ))}
                    <FormMessage />
                  </FormItem>
                )}
              />
              <p className="text-xs text-muted-foreground">
                AddressLatest / AddressAtBlock / ERC-20 metrics require address or token plans. The
                MVP form doesn't capture those yet — ticks will run but yield no diffs for those
                categories until the plan editors ship.
              </p>
            </CardContent>
          </Card>

          <div className="flex items-center gap-2">
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? "Creating…" : "Create schedule"}
            </Button>
            <Link href="/schedules" className={buttonVariants({ variant: "outline" })}>
              Cancel
            </Link>
          </div>
        </form>
      </Form>
    </div>
  );
}
