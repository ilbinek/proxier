import { useEffect, useRef, useState } from "react";
import { Download, Loader2, Upload } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { FieldError, PageHeader } from "@/components/bits";
import { useImport, useSaveSettings, useSettings, useSystem } from "@/hooks/queries";
import { useNow } from "@/hooks/useNow";
import { api, ApiError, errMsg } from "@/lib/api";
import { fmtDuration } from "@/lib/format";
import type { Settings } from "@/lib/types";

function GeneralSettings() {
  const settings = useSettings();
  const system = useSystem();
  const save = useSaveSettings();
  const [form, setForm] = useState<Settings | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    if (settings.data) setForm(settings.data);
  }, [settings.data]);
  if (!form) return null;

  const num = (k: keyof Settings) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: Number(e.target.value) || 0 });

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrors({});
    try {
      await save.mutateAsync(form);
      toast.success("Settings saved");
    } catch (err) {
      if (err instanceof ApiError && err.fields) setErrors(err.fields);
      else toast.error(errMsg(err));
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>General</CardTitle>
        <CardDescription>Shown to you in “connect to” hints; doesn't affect forwarding.</CardDescription>
      </CardHeader>
      <CardContent>
        <form id="settings" onSubmit={submit} className="space-y-5">
          <div className="space-y-2">
            <Label htmlFor="pub">Public address of this server</Label>
            <Input
              id="pub"
              value={form.public_address}
              onChange={(e) => setForm({ ...form, public_address: e.target.value })}
              placeholder={system.data?.guessed_address || "vps.example.com"}
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">
              The IP or domain your friends connect to. Leave empty to use the detected address
              {system.data?.guessed_address ? ` (${system.data.guessed_address})` : ""}.
            </p>
            <FieldError msg={errors.public_address} />
          </div>
          <div>
            <Label>Data retention (days)</Label>
            <div className="mt-2 grid gap-3 sm:grid-cols-3">
              <div className="space-y-1">
                <Input type="number" min={1} value={form.retention_minute_days} onChange={num("retention_minute_days")} />
                <p className="text-xs text-muted-foreground">Per-minute traffic</p>
                <FieldError msg={errors.retention_minute_days} />
              </div>
              <div className="space-y-1">
                <Input type="number" min={1} value={form.retention_hour_days} onChange={num("retention_hour_days")} />
                <p className="text-xs text-muted-foreground">Hourly traffic</p>
                <FieldError msg={errors.retention_hour_days} />
              </div>
              <div className="space-y-1">
                <Input type="number" min={1} value={form.retention_events_days} onChange={num("retention_events_days")} />
                <p className="text-xs text-muted-foreground">Events log</p>
                <FieldError msg={errors.retention_events_days} />
              </div>
            </div>
          </div>
        </form>
      </CardContent>
      <CardFooter>
        <Button type="submit" form="settings" disabled={save.isPending}>
          {save.isPending && <Loader2 className="animate-spin" />} Save
        </Button>
      </CardFooter>
    </Card>
  );
}

function PasswordCard() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (next !== confirm) {
      setErrors({ confirm: "Passwords don't match" });
      return;
    }
    setBusy(true);
    setErrors({});
    try {
      await api("/auth/password", { body: { current, new: next } });
      toast.success("Password changed; other sessions were logged out");
      setCurrent("");
      setNext("");
      setConfirm("");
    } catch (err) {
      if (err instanceof ApiError && err.fields) setErrors(err.fields);
      else toast.error(errMsg(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Admin password</CardTitle>
        <CardDescription>Changing it logs out every other session.</CardDescription>
      </CardHeader>
      <CardContent>
        <form id="pw" onSubmit={submit} className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="cur">Current password</Label>
            <Input id="cur" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
            <FieldError msg={errors.current} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="new">New password</Label>
            <Input id="new" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
            <FieldError msg={errors.new} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="conf">Confirm new password</Label>
            <Input id="conf" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
            <FieldError msg={errors.confirm} />
          </div>
        </form>
      </CardContent>
      <CardFooter>
        <Button type="submit" form="pw" disabled={busy || !current || !next}>
          {busy && <Loader2 className="animate-spin" />} Change password
        </Button>
      </CardFooter>
    </Card>
  );
}

function BackupCard() {
  const fileRef = useRef<HTMLInputElement>(null);
  const [doc, setDoc] = useState<{ name: string; data: unknown } | null>(null);
  const [mode, setMode] = useState<"merge" | "replace">("merge");
  const imp = useImport();

  const pick = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    e.target.value = "";
    if (!f) return;
    try {
      setDoc({ name: f.name, data: JSON.parse(await f.text()) });
    } catch {
      toast.error("That file isn't valid JSON");
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Backup &amp; restore</CardTitle>
        <CardDescription>Export rules, bans, custom presets and settings as JSON, or import them on another server.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center gap-2">
        <Button variant="outline" asChild>
          <a href="/api/export" download>
            <Download /> Export
          </a>
        </Button>
        <Select value={mode} onValueChange={(v) => setMode(v as "merge" | "replace")}>
          <SelectTrigger className="w-44"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="merge">Import: merge</SelectItem>
            <SelectItem value="replace">Import: replace all</SelectItem>
          </SelectContent>
        </Select>
        <Button variant="outline" onClick={() => fileRef.current?.click()} disabled={imp.isPending}>
          {imp.isPending ? <Loader2 className="animate-spin" /> : <Upload />} Import…
        </Button>
        <input ref={fileRef} type="file" accept="application/json,.json" className="hidden" onChange={pick} />
      </CardContent>
      <ConfirmDialog
        open={!!doc}
        onOpenChange={(o) => !o && setDoc(null)}
        title={mode === "replace" ? "Replace everything?" : "Merge import?"}
        description={
          mode === "replace"
            ? `All current rules, bans and custom presets will be deleted and replaced with the contents of ${doc?.name}.`
            : `Rules, bans and presets from ${doc?.name} will be added. Rules that conflict with existing ports are imported disabled.`
        }
        confirmLabel="Import"
        destructive={mode === "replace"}
        onConfirm={async () => {
          if (!doc) return;
          try {
            const r = await imp.mutateAsync({ doc: doc.data, mode });
            toast.success(`Imported ${r.rules} rules, ${r.bans} bans, ${r.presets} presets`, {
              description: r.rules_disabled ? `${r.rules_disabled} rule(s) were disabled due to port conflicts.` : undefined,
            });
          } catch (e) {
            toast.error("Import failed", { description: errMsg(e) });
          }
        }}
      />
    </Card>
  );
}

function SystemCard() {
  const sys = useSystem();
  const now = useNow(10_000);
  const d = sys.data;
  if (!d) return null;
  const rows: [string, React.ReactNode][] = [
    ["Version", d.version],
    ["Uptime", fmtDuration(now - d.started_at)],
    ["Platform", `${d.os} · ${d.go_version}`],
    ["Web UI address", <code key="l">{d.listen}</code>],
    ["TLS", d.tls_mode + (d.acme_domain ? ` (${d.acme_domain})` : "")],
    ["Behind reverse proxy", d.trust_proxy ? "yes (trusting X-Forwarded-*)" : "no"],
    ["Data directory", <code key="d">{d.data_dir}</code>],
    ["Interface IPs", (d.interface_ips ?? []).join(", ") || "—"],
  ];
  return (
    <Card>
      <CardHeader>
        <CardTitle>Server</CardTitle>
        <CardDescription>
          Set with command-line flags or <code>PROXIER_*</code> environment variables (e.g. <code>/etc/proxier/proxier.env</code>), so a
          mistake here can't lock you out.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{k}</dt>
              <dd className="min-w-0 break-words">{v}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

export function SettingsPage() {
  return (
    <>
      <PageHeader title="Settings" />
      <div className="grid gap-6 xl:grid-cols-2">
        <div className="space-y-6">
          <GeneralSettings />
          <BackupCard />
        </div>
        <div className="space-y-6">
          <PasswordCard />
          <SystemCard />
        </div>
      </div>
    </>
  );
}
