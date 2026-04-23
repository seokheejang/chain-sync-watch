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
import { useCreateRun } from "@/lib/api/hooks";

// Metric catalog — mirrors internal/verification/metric.go. When the
// backend exposes a /metrics endpoint we switch this to a fetched
// list; hard-coding for now keeps the MVP form self-contained.
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

// MVP schema — latest_n sampling + manual trigger only. The fuller
// discriminated unions (fixed_list / random / sparse_steps, scheduled
// / realtime triggers, address_plans, token_plans) land in follow-ups.
const formSchema = z.object({
  chain_id: z.coerce.number().int().min(1, "Chain id is required"),
  metrics: z.array(z.string()).min(1, "Pick at least one metric"),
  latest_n: z.coerce.number().int().min(1).max(1000),
  user: z.string().min(1, "Who is running this?"),
});

type FormValues = z.infer<typeof formSchema>;

export function NewRunForm() {
  const router = useRouter();
  const create = useCreateRun();

  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      chain_id: 10,
      metrics: ["block.hash"],
      latest_n: 3,
      user: "web-ui",
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
        trigger: {
          kind: "manual",
          user: values.user,
        },
      },
      {
        onSuccess: (data) => {
          toast.success(`Run created — ${data?.run_id}`);
          if (data?.run_id) router.push(`/runs/${data.run_id}`);
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : "Create run failed"),
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
        <Link href="/runs" className={buttonVariants({ variant: "ghost", size: "sm" }) + " mb-2"}>
          <ArrowLeft className="mr-2 h-4 w-4" /> Runs
        </Link>
        <h1 className="text-2xl font-semibold tracking-tight">New run</h1>
        <p className="text-sm text-muted-foreground">
          Dispatches a manual verification run. Scheduled and realtime triggers land in a follow-up.
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
                    <FormLabel>Chain id</FormLabel>
                    <Input type="number" min={1} {...field} />
                    <FormDescription>10 = Optimism mainnet (MVP target).</FormDescription>
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
                    <FormDescription>How many trailing blocks from tip to verify.</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="user"
                render={({ field }) => (
                  <FormItem className="sm:col-span-2">
                    <FormLabel>Triggered by</FormLabel>
                    <Input {...field} />
                    <FormDescription>
                      Operator identifier persisted with the run's manual trigger.
                    </FormDescription>
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
                AddressLatest / AddressAtBlock / ERC-20 metrics need an address plan (or token
                plan), which the MVP form doesn't capture yet. Selecting them will run but yield no
                diffs until address/token plans are wired.
              </p>
            </CardContent>
          </Card>

          <div className="flex items-center gap-2">
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? "Creating…" : "Create run"}
            </Button>
            <Link href="/runs" className={buttonVariants({ variant: "outline" })}>
              Cancel
            </Link>
          </div>
        </form>
      </Form>
    </div>
  );
}
