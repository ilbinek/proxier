import { useState } from "react";
import { Info, Play, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ApplyPresetDialog } from "@/components/ApplyPresetDialog";
import { PageHeader, ProtoBadge } from "@/components/bits";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { useDeletePreset, usePresets } from "@/hooks/queries";
import { errMsg } from "@/lib/api";
import { portSpan } from "@/lib/format";
import type { Preset } from "@/lib/types";

function PresetCard({ p, onApply, onDelete }: { p: Preset; onApply: () => void; onDelete?: () => void }) {
  return (
    <Card className="flex flex-col">
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <CardTitle className="leading-snug">{p.name}</CardTitle>
          {!p.builtin && <Badge variant="secondary">Custom</Badge>}
          {p.hint && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Info className="size-4 shrink-0 text-muted-foreground" />
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">{p.hint}</TooltipContent>
            </Tooltip>
          )}
        </div>
        {p.description && <CardDescription>{p.description}</CardDescription>}
      </CardHeader>
      <CardContent className="flex-1 space-y-1.5">
        {p.rules.map((r, i) => (
          <div key={i} className={`flex items-center gap-2 text-sm ${r.optional ? "text-muted-foreground" : ""}`}>
            <ProtoBadge proto={r.protocol} />
            <span className="font-mono text-xs">{portSpan(r.port_start, r.port_end)}</span>
            <span className="truncate">{r.name}</span>
            {r.optional && <span className="text-xs">(optional)</span>}
          </div>
        ))}
      </CardContent>
      <CardFooter className="gap-2">
        <Button size="sm" onClick={onApply}>
          <Play /> Use preset
        </Button>
        {onDelete && (
          <Button size="sm" variant="ghost" onClick={onDelete} className="ml-auto text-destructive" aria-label="Delete preset">
            <Trash2 />
          </Button>
        )}
      </CardFooter>
    </Card>
  );
}

export function PresetsPage() {
  const presets = usePresets();
  const del = useDeletePreset();
  const [applying, setApplying] = useState<Preset | null>(null);
  const [deleting, setDeleting] = useState<Preset | null>(null);

  const builtin = (presets.data ?? []).filter((p) => p.builtin);
  const custom = (presets.data ?? []).filter((p) => !p.builtin);

  return (
    <>
      <PageHeader
        title="Presets"
        description="Create all the rules a game or app needs in one go. Select rules on the Rules page to save your own."
      />
      {presets.isLoading && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => <Skeleton key={i} className="h-48" />)}
        </div>
      )}
      {custom.length > 0 && (
        <>
          <h2 className="mb-3 text-lg font-semibold">Your presets</h2>
          <div className="mb-8 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {custom.map((p) => <PresetCard key={p.id} p={p} onApply={() => setApplying(p)} onDelete={() => setDeleting(p)} />)}
          </div>
          <h2 className="mb-3 text-lg font-semibold">Built-in</h2>
        </>
      )}
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {builtin.map((p) => <PresetCard key={p.id} p={p} onApply={() => setApplying(p)} />)}
      </div>
      <ApplyPresetDialog preset={applying} onClose={() => setApplying(null)} />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete preset “${deleting?.name}”?`}
        description="Rules created from it are not affected."
        confirmLabel="Delete"
        destructive
        onConfirm={async () => {
          if (!deleting) return;
          try {
            await del.mutateAsync(deleting.id);
            toast.success("Preset deleted");
          } catch (e) {
            toast.error(errMsg(e));
          }
        }}
      />
    </>
  );
}
