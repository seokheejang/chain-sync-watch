"use client";

import { useEffect } from "react";
import { useForm } from "react-hook-form";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { Schemas } from "@/lib/api/client";
import { useSourceTypes } from "@/lib/api/hooks";

// SourceFormValues is the shape both the create and edit flows
// round-trip through react-hook-form. Fields that only matter in
// one mode (`clear_secret` on edit, `chain_id` picker on create)
// are still present on both sides — the parent picks which ones to
// read out.
//
// type is a plain string (not a literal union) because private
// builds register additional adapter types at runtime — the
// dropdown's options come from the /sources/types endpoint.
export type SourceFormValues = {
  type: string;
  chain_id: number;
  endpoint: string;
  api_key: string;
  archive: boolean;
  enabled: boolean;
  clear_secret: boolean;
};

type SourceRow = Schemas["SourceConfigView"];

export function SourceFormDialog({
  open,
  onOpenChange,
  mode,
  source,
  defaultChainId,
  onSubmit,
  pending,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  mode: "create" | "edit";
  source?: SourceRow | null;
  // defaultChainId is the sidebar-selected chain on create. Edit mode
  // ignores it — the dialog prefills from the row.
  defaultChainId?: number;
  onSubmit: (values: SourceFormValues) => void;
  pending?: boolean;
}) {
  const form = useForm<SourceFormValues>({
    defaultValues: {
      type: "rpc",
      chain_id: defaultChainId ?? 10,
      endpoint: "",
      api_key: "",
      archive: false,
      enabled: true,
      clear_secret: false,
    },
  });

  // Types come from the backend — private builds surface custom
  // types without a frontend code change. Fall back to the public
  // bundled set while loading or if the endpoint is unreachable.
  const { data: typesData } = useSourceTypes();
  const adapterTypes = typesData?.types ?? ["blockscout", "routescan", "rpc"];

  // Prefill the form whenever the dialog opens. React-hook-form
  // doesn't re-read defaultValues on prop change, so a manual reset
  // covers the "click edit on a different row" flow.
  useEffect(() => {
    if (!open) return;
    if (mode === "edit" && source) {
      form.reset({
        type: source.type as SourceFormValues["type"],
        chain_id: source.chain_id,
        endpoint: source.endpoint,
        api_key: "",
        archive: Boolean((source.options as { archive?: boolean })?.archive),
        enabled: source.enabled,
        clear_secret: false,
      });
    } else {
      form.reset({
        type: "rpc",
        chain_id: defaultChainId ?? 10,
        endpoint: "",
        api_key: "",
        archive: false,
        enabled: true,
        clear_secret: false,
      });
    }
  }, [open, mode, source, defaultChainId, form]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{mode === "create" ? "New source" : `Edit ${source?.id ?? ""}`}</DialogTitle>
          <DialogDescription>
            {mode === "create"
              ? "Type + chain id determine the row's primary key (e.g. rpc-10). One row per (type, chain) — edit the existing row instead of creating a duplicate."
              : "Endpoint / enabled / options are editable. To rotate an api key, paste the new value; leave blank to keep the current secret or tick Clear to remove it."}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4" id="source-form">
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="type">Type</Label>
              <select
                id="type"
                disabled={mode === "edit"}
                {...form.register("type")}
                className="flex h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm disabled:cursor-not-allowed disabled:opacity-50"
              >
                {adapterTypes.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="chain_id">Chain id</Label>
              <Input
                id="chain_id"
                type="number"
                min={1}
                // Disabled on both modes: create uses the
                // sidebar-selected chain (defaultChainId), edit locks
                // the row's natural key.
                disabled
                {...form.register("chain_id", { valueAsNumber: true })}
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="endpoint">Endpoint</Label>
            <Input
              id="endpoint"
              placeholder="https://…"
              {...form.register("endpoint", { required: true })}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="api_key">
              API key{" "}
              <span className="font-normal text-muted-foreground">
                {mode === "edit"
                  ? "(leave blank to keep existing)"
                  : "(optional; only adapters that need it)"}
              </span>
            </Label>
            <Input
              id="api_key"
              type="password"
              autoComplete="off"
              placeholder={source?.has_secret ? "•••••• (set)" : ""}
              {...form.register("api_key")}
            />
          </div>

          <div className="flex flex-wrap gap-4 text-sm">
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                className="size-4 accent-primary"
                {...form.register("archive")}
              />
              Archive node (rpc only — enables historical state)
            </label>
            {mode === "edit" ? (
              <>
                <label className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    className="size-4 accent-primary"
                    {...form.register("enabled")}
                  />
                  Enabled
                </label>
                <label className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    className="size-4 accent-primary"
                    {...form.register("clear_secret")}
                  />
                  Clear stored secret
                </label>
              </>
            ) : null}
          </div>
        </form>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" form="source-form" disabled={pending}>
            {pending ? "Saving…" : mode === "create" ? "Create" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
