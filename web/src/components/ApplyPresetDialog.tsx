import { useEffect, useState } from "react";
import { Info, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { FieldError, ProtoBadge } from "@/components/bits";
import { useApplyPreset, usePublicAddress, useSystem } from "@/hooks/queries";
import { ApiError } from "@/lib/api";
import { portSpan } from "@/lib/format";
import type { Preset } from "@/lib/types";

const ALL = "__all";

interface Row {
  selected: boolean;
  listen: string;
  target: string;
}

export function ApplyPresetDialog({ preset, onClose }: { preset: Preset | null; onClose: () => void }) {
  const [host, setHost] = useState("");
  const [prefix, setPrefix] = useState("");
  const [listenIp, setListenIp] = useState(ALL);
  const [rows, setRows] = useState<Row[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const apply = useApplyPreset();
  const system = useSystem();
  const publicAddr = usePublicAddress();

  useEffect(() => {
    if (!preset) return;
    setHost("");
    setPrefix(preset.name);
    setListenIp(ALL);
    setErrors({});
    setRows(
      preset.rules.map((r) => ({
        selected: !r.optional,
        listen: String(r.port_start),
        target: String(r.target_port_start || r.port_start),
      })),
    );
  }, [preset]);

  if (!preset) return null;

  const update = (i: number, patch: Partial<Row>) =>
    setRows((rs) =>
      rs.map((r, j) => {
        if (j !== i) return r;
        const next = { ...r, ...patch };
        // Ports that must match on both sides follow the listen port.
        if (preset.rules[i].same_port && patch.listen !== undefined) next.target = patch.listen;
        return next;
      }),
    );

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrors({});
    try {
      const created = await apply.mutateAsync({
        id: preset.id,
        target_host: host.trim(),
        listen_ip: listenIp === ALL ? "" : listenIp,
        name_prefix: prefix.trim(),
        items: rows.map((r, i) => ({
          index: i,
          selected: r.selected,
          listen_port_start: Number(r.listen) || 0,
          target_port_start: Number(r.target) || 0,
        })),
      });
      toast.success(`Created ${created.length} rule${created.length === 1 ? "" : "s"} from “${preset.name}”`);
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.fields) setErrors(err.fields);
      else toast.error(err instanceof Error ? err.message : "Failed");
    }
  };

  const itemErrors = Object.entries(errors).filter(([k]) => k.startsWith("items"));

  return (
    <Dialog open={!!preset} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Apply preset: {preset.name}</DialogTitle>
          <DialogDescription>{preset.description || "Creates one rule per selected port."}</DialogDescription>
        </DialogHeader>
        <form id="preset-form" onSubmit={submit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="p-host">Target server (IP or hostname)</Label>
            <Input id="p-host" value={host} onChange={(e) => setHost(e.target.value)} placeholder="ts.example.ru" className="font-mono" autoFocus />
            <FieldError msg={errors.target_host} />
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="p-prefix">Rule name prefix</Label>
              <Input id="p-prefix" value={prefix} onChange={(e) => setPrefix(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label>Listen interface</Label>
              <Select value={listenIp} onValueChange={setListenIp}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={ALL}>All interfaces</SelectItem>
                  {(system.data?.interface_ips ?? []).map((ip) => (
                    <SelectItem key={ip} value={ip}>{ip}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="overflow-hidden rounded-lg border">
            <div className="grid grid-cols-[auto_1fr_5.5rem_5.5rem] items-center gap-x-3 border-b bg-muted/50 px-3 py-2 text-xs font-medium text-muted-foreground">
              <span />
              <span>Port</span>
              <span>Listen</span>
              <span>Target</span>
            </div>
            {preset.rules.map((r, i) => {
              const row = rows[i];
              if (!row) return null;
              const width = r.port_end - r.port_start;
              return (
                <div key={i} className="grid grid-cols-[auto_1fr_5.5rem_5.5rem] items-center gap-x-3 border-b px-3 py-2 last:border-b-0">
                  <Checkbox checked={row.selected} onCheckedChange={(v) => update(i, { selected: v === true })} aria-label={`Include ${r.name}`} />
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">{r.name}</span>
                      <ProtoBadge proto={r.protocol} />
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {width > 0 ? `${width + 1} ports · ` : ""}default {portSpan(r.port_start, r.port_end)}
                      {r.optional && " · optional"}
                      {r.same_port && " · must match"}
                    </div>
                  </div>
                  <Input value={row.listen} onChange={(e) => update(i, { listen: e.target.value })} disabled={!row.selected} className="h-8 font-mono text-xs" inputMode="numeric" />
                  <Input value={row.target} onChange={(e) => update(i, { target: e.target.value })} disabled={!row.selected || r.same_port} className="h-8 font-mono text-xs" inputMode="numeric" />
                </div>
              );
            })}
          </div>
          {itemErrors.map(([k, v]) => (
            <FieldError key={k} msg={v} />
          ))}

          {preset.hint && (
            <Alert>
              <Info />
              <AlertTitle>Before you share {publicAddr}</AlertTitle>
              <AlertDescription>{preset.hint}</AlertDescription>
            </Alert>
          )}
        </form>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button type="submit" form="preset-form" disabled={apply.isPending || !host.trim()}>
            {apply.isPending && <Loader2 className="animate-spin" />}
            Create rules
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
