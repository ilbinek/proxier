import { useEffect, useMemo, useState } from "react";
import { ArrowRight, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { FieldError } from "@/components/bits";
import { useSaveRule, useSystem } from "@/hooks/queries";
import { api, ApiError } from "@/lib/api";
import { bracketHost, parsePortSpec, portSpan, protoLabel } from "@/lib/format";
import type { Protocol, Rule, RuleInput } from "@/lib/types";

const ALL = "__all";

interface Form {
  name: string;
  enabled: boolean;
  protocol: Protocol;
  listenIp: string;
  listen: string;
  targetHost: string;
  targetPort: string;
  udpTimeout: string;
  notes: string;
}

function toForm(r?: Partial<Rule>): Form {
  return {
    name: r?.name ?? "",
    enabled: r?.enabled ?? true,
    protocol: r?.protocol ?? "tcp",
    listenIp: r?.listen_ip || ALL,
    listen: r?.listen_port_start ? portSpan(r.listen_port_start, r.listen_port_end ?? r.listen_port_start) : "",
    targetHost: r?.target_host ?? "",
    targetPort: r?.target_port_start && r.target_port_start !== r.listen_port_start ? String(r.target_port_start) : "",
    udpTimeout: String(r?.udp_idle_timeout_s || 120),
    notes: r?.notes ?? "",
  };
}

type Errors = Record<string, string>;

function build(f: Form, id?: number): { input?: RuleInput; errors: Errors } {
  const errors: Errors = {};
  const span = parsePortSpec(f.listen);
  if (!f.listen.trim()) errors.listen = "Required";
  else if (!span) errors.listen = "Use a port (9987) or range (27000-27050)";
  else if (span[1] < span[0]) errors.listen = "End port must be ≥ start port";
  const tp = f.targetPort.trim() ? Number(f.targetPort) : span?.[0] ?? 0;
  if (f.targetPort.trim() && (!Number.isInteger(tp) || tp < 1 || tp > 65535)) errors.target_port = "Port 1-65535";
  const timeout = Number(f.udpTimeout);
  if (f.protocol !== "tcp" && (!Number.isInteger(timeout) || timeout < 5)) errors.udp_idle_timeout_s = "At least 5 seconds";
  if (!span || Object.keys(errors).length) return { errors };
  return {
    errors,
    input: {
      id,
      name: f.name.trim(),
      enabled: f.enabled,
      protocol: f.protocol,
      listen_ip: f.listenIp === ALL ? "" : f.listenIp,
      listen_port_start: span[0],
      listen_port_end: span[1],
      target_host: f.targetHost.trim(),
      target_port_start: tp,
      udp_idle_timeout_s: f.protocol === "tcp" ? 120 : timeout,
      notes: f.notes,
    },
  };
}

function mapServerFields(fields?: Record<string, string>): Errors {
  const out: Errors = {};
  for (const [k, v] of Object.entries(fields ?? {})) {
    const key = k === "listen_port_start" || k === "listen_port_end" ? "listen" : k === "target_port_start" ? "target_port" : k;
    out[key] ??= v;
  }
  return out;
}

export function RuleEditor({
  open,
  onOpenChange,
  rule,
  template,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  /** Rule being edited (has id). */
  rule?: Rule;
  /** Prefill for a new rule (e.g. duplicate). */
  template?: Partial<Rule>;
}) {
  const [form, setForm] = useState<Form>(() => toForm(rule ?? template));
  const [serverErrors, setServerErrors] = useState<Errors>({});
  const [checking, setChecking] = useState(false);
  const [touched, setTouched] = useState(false);
  const save = useSaveRule();
  const system = useSystem();

  useEffect(() => {
    if (open) {
      setForm(toForm(rule ?? template));
      setServerErrors({});
      setTouched(false);
    }
  }, [open, rule, template]);

  const { input, errors: localErrors } = useMemo(() => build(form, rule?.id), [form, rule?.id]);

  // Debounced server-side validation + conflict check.
  useEffect(() => {
    if (!open || !touched || !input || !input.name || !input.target_host) return;
    const ctl = new AbortController();
    const t = setTimeout(async () => {
      setChecking(true);
      try {
        await api("/rules/check", { body: input, signal: ctl.signal });
        setServerErrors({});
      } catch (e) {
        if (e instanceof ApiError) setServerErrors(mapServerFields(e.fields));
      } finally {
        if (!ctl.signal.aborted) setChecking(false);
      }
    }, 450);
    return () => {
      clearTimeout(t);
      ctl.abort();
    };
  }, [input, open, touched]);

  const set = <K extends keyof Form>(k: K, v: Form[K]) => {
    setTouched(true);
    setForm((f) => ({ ...f, [k]: v }));
  };
  const errors = { ...serverErrors, ...(touched ? localErrors : {}) };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!input) return;
    try {
      await save.mutateAsync(input);
      toast.success(rule ? "Rule updated" : "Rule created");
      onOpenChange(false);
    } catch (err) {
      if (err instanceof ApiError && err.fields) setServerErrors(mapServerFields(err.fields));
      else toast.error(err instanceof Error ? err.message : "Save failed");
    }
  };

  const ips = system.data?.interface_ips ?? [];
  const span = parsePortSpec(form.listen);
  const width = span ? span[1] - span[0] : 0;
  const tp = form.targetPort.trim() ? Number(form.targetPort) : span?.[0];

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col gap-0 sm:max-w-lg">
        <SheetHeader className="border-b">
          <SheetTitle>{rule ? `Edit “${rule.name}”` : "New forwarding rule"}</SheetTitle>
          <SheetDescription>Traffic arriving on this server's port is relayed to the target.</SheetDescription>
        </SheetHeader>
        <form id="rule-form" onSubmit={submit} className="flex-1 space-y-5 overflow-y-auto p-4">
          <div className="space-y-2">
            <Label htmlFor="name">Name</Label>
            <Input id="name" value={form.name} onChange={(e) => set("name", e.target.value)} placeholder="TeamSpeak voice" autoFocus />
            <FieldError msg={errors.name} />
          </div>

          <div className="space-y-2">
            <Label>Protocol</Label>
            <ToggleGroup
              type="single"
              variant="outline"
              value={form.protocol}
              onValueChange={(v) => v && set("protocol", v as Protocol)}
              className="w-full"
            >
              <ToggleGroupItem value="tcp" className="flex-1">TCP</ToggleGroupItem>
              <ToggleGroupItem value="udp" className="flex-1">UDP</ToggleGroupItem>
              <ToggleGroupItem value="both" className="flex-1">TCP + UDP</ToggleGroupItem>
            </ToggleGroup>
            <FieldError msg={errors.protocol} />
          </div>

          <fieldset className="space-y-3 rounded-lg border p-3">
            <legend className="px-1 text-xs font-medium text-muted-foreground">Listen on this server</legend>
            <div className="grid grid-cols-[1fr_9rem] gap-3">
              <div className="space-y-2">
                <Label>Interface</Label>
                <Select value={form.listenIp} onValueChange={(v) => set("listenIp", v)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={ALL}>All interfaces (IPv4 + IPv6)</SelectItem>
                    {ips.map((ip) => (
                      <SelectItem key={ip} value={ip}>{ip}</SelectItem>
                    ))}
                    {form.listenIp !== ALL && !ips.includes(form.listenIp) && (
                      <SelectItem value={form.listenIp}>{form.listenIp}</SelectItem>
                    )}
                  </SelectContent>
                </Select>
                <FieldError msg={errors.listen_ip} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="listen">Port or range</Label>
                <Input id="listen" value={form.listen} onChange={(e) => set("listen", e.target.value)} placeholder="9987" inputMode="numeric" className="font-mono" />
              </div>
            </div>
            <FieldError msg={errors.listen} />
          </fieldset>

          <fieldset className="space-y-3 rounded-lg border p-3">
            <legend className="px-1 text-xs font-medium text-muted-foreground">Forward to</legend>
            <div className="grid grid-cols-[1fr_9rem] gap-3">
              <div className="space-y-2">
                <Label htmlFor="target">Target host</Label>
                <Input id="target" value={form.targetHost} onChange={(e) => set("targetHost", e.target.value)} placeholder="ts.example.ru or 203.0.113.10" className="font-mono" />
              </div>
              <div className="space-y-2">
                <Label htmlFor="tport">{width > 0 ? "Start port" : "Port"}</Label>
                <Input id="tport" value={form.targetPort} onChange={(e) => set("targetPort", e.target.value)} placeholder={span ? String(span[0]) : "same"} inputMode="numeric" className="font-mono" />
              </div>
            </div>
            <FieldError msg={errors.target_host} />
            <FieldError msg={errors.target_port} />
          </fieldset>

          {span && form.targetHost.trim() && (
            <div className="flex flex-wrap items-center gap-2 rounded-lg bg-muted/60 px-3 py-2 font-mono text-xs">
              <span className="text-muted-foreground">{protoLabel(form.protocol)}</span>
              <span>{form.listenIp === ALL ? "*" : bracketHost(form.listenIp)}:{portSpan(span[0], span[1])}</span>
              <ArrowRight className="size-3.5 text-muted-foreground" />
              <span className="break-all">{bracketHost(form.targetHost.trim())}:{tp ? portSpan(tp, tp + width) : "?"}</span>
              {width > 0 && <span className="text-muted-foreground">({width + 1} ports)</span>}
            </div>
          )}

          {form.protocol !== "tcp" && (
            <div className="space-y-2">
              <Label htmlFor="timeout">UDP idle timeout (seconds)</Label>
              <Input id="timeout" value={form.udpTimeout} onChange={(e) => set("udpTimeout", e.target.value)} inputMode="numeric" className="w-32 font-mono" />
              <p className="text-xs text-muted-foreground">A UDP client's session is closed after this long without packets.</p>
              <FieldError msg={errors.udp_idle_timeout_s} />
            </div>
          )}

          <div className="space-y-2">
            <Label htmlFor="notes">Notes</Label>
            <Textarea id="notes" value={form.notes} onChange={(e) => set("notes", e.target.value)} rows={2} placeholder="Optional" />
          </div>

          <label className="flex items-center justify-between gap-3 rounded-lg border p-3">
            <span>
              <span className="block text-sm font-medium">Enabled</span>
              <span className="block text-xs text-muted-foreground">Disabled rules keep their settings but free the port.</span>
            </span>
            <Switch checked={form.enabled} onCheckedChange={(v) => set("enabled", v)} />
          </label>
        </form>
        <SheetFooter className="flex-row items-center justify-end gap-2 border-t">
          {checking && (
            <span className="mr-auto flex items-center gap-1.5 text-xs text-muted-foreground">
              <Loader2 className="size-3 animate-spin" /> Checking ports…
            </span>
          )}
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button type="submit" form="rule-form" disabled={save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" />}
            {rule ? "Save changes" : "Create rule"}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
