import { useState } from "react";
import { Link, useParams } from "react-router";
import { AlertTriangle, ArrowLeft, Ban, Info, Pencil, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CopyButton, EmptyState, LevelBadge, ProtoBadge, StatusBadge } from "@/components/bits";
import { LiveChart, TrafficChart } from "@/components/charts";
import { ConnectionsTable } from "@/components/ConnectionsTable";
import { useBans, useConnections, useDeleteBan, useEvents, usePublicAddress, useRule, useTraffic } from "@/hooks/queries";
import { errMsg } from "@/lib/api";
import { bracketHost, fmtBytes, fmtDateTime, fmtRate, listenStr, portSpan, targetStr } from "@/lib/format";
import { useLive } from "@/lib/live";
import { useRuleActions } from "./Rules";

const RANGES = ["1h", "6h", "24h", "7d", "30d", "1y"] as const;

export function RuleDetailPage() {
  const id = Number(useParams().id);
  const rule = useRule(id);
  const actions = useRuleActions();
  const samples = useLive((s) => s.samples);
  const live = useLive((s) => s.byRule[id]);
  const [range, setRange] = useState<string>("24h");
  const traffic = useTraffic(range, id);
  const conns = useConnections(id);
  const events = useEvents({ rule: id, limit: 15 });
  const bans = useBans();
  const delBan = useDeleteBan();
  const publicAddr = usePublicAddress();

  if (rule.isLoading) return <Skeleton className="h-96 w-full" />;
  if (!rule.data) {
    return (
      <EmptyState title="Rule not found">
        <Link to="/rules" className="underline">Back to rules</Link>
      </EmptyState>
    );
  }
  const r = rule.data;
  const ruleBans = (bans.data ?? []).filter((b) => b.rule_id === r.id);
  const connectAddr = `${bracketHost(r.listen_ip || publicAddr)}:${portSpan(r.listen_port_start, r.listen_port_end)}`;
  const isTS3 = r.preset_group.startsWith("ts3@");
  const ts3Message = `Hi! Players connect to our TeamSpeak through a relay with IP ${publicAddr}. Please add ${publicAddr} to query_ip_allowlist.txt so anti-flood doesn't ban everyone behind it. Thanks!`;

  return (
    <>
      <Button variant="ghost" size="sm" asChild className="-ml-2 mb-2">
        <Link to="/rules"><ArrowLeft /> Rules</Link>
      </Button>
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="truncate text-2xl font-semibold tracking-tight">{r.name}</h1>
            <StatusBadge state={r.status.state} />
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-2 font-mono text-sm text-muted-foreground">
            <ProtoBadge proto={r.protocol} />
            {listenStr(r)} → {targetStr(r)}
          </div>
          {r.notes && <p className="mt-2 text-sm text-muted-foreground">{r.notes}</p>}
        </div>
        <div className="flex items-center gap-2">
          <label className="mr-2 flex items-center gap-2 text-sm">
            <Switch checked={r.enabled} onCheckedChange={(v) => actions.setEnabled(r, v)} />
            Enabled
          </label>
          <Button variant="outline" size="sm" onClick={() => actions.edit(r)}><Pencil /> Edit</Button>
          <Button variant="outline" size="sm" onClick={() => actions.remove(r)} className="text-destructive"><Trash2 /> Delete</Button>
        </div>
      </div>

      {r.status.state === "error" && (
        <Alert variant="destructive" className="mb-6">
          <AlertTriangle />
          <AlertTitle>Not forwarding ({r.status.listeners}/{r.status.expected} listeners up)</AlertTitle>
          <AlertDescription>
            {r.status.error}. proxier retries every 30 seconds. Make sure no other program uses the port.
          </AlertDescription>
        </Alert>
      )}

      {isTS3 && (
        <Alert className="mb-6">
          <Info />
          <AlertTitle>Ask the TeamSpeak admin to allowlist {publicAddr}</AlertTitle>
          <AlertDescription>
            <p>
              Everyone using this relay reaches the TS3 server from this server's IP. If it isn't in the server's
              <code className="mx-1 rounded bg-muted px-1">query_ip_allowlist.txt</code>, anti-flood may ban the relay and
              disconnect all your friends at once.
            </p>
            <div className="mt-2">
              <CopyButton value={ts3Message} label="Copy message for the admin" className="-ml-2" />
            </div>
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Card className="gap-2 py-4">
          <CardHeader className="px-4"><CardDescription>Players connect to</CardDescription></CardHeader>
          <CardContent className="flex items-center gap-1 px-4">
            <span className="truncate font-mono text-lg font-semibold">{connectAddr}</span>
            <CopyButton value={connectAddr} />
          </CardContent>
        </Card>
        <Card className="gap-2 py-4">
          <CardHeader className="px-4"><CardDescription>Forwarded to</CardDescription></CardHeader>
          <CardContent className="px-4"><span className="break-all font-mono text-lg font-semibold">{targetStr(r)}</span></CardContent>
        </Card>
        <Card className="gap-2 py-4">
          <CardHeader className="px-4"><CardDescription>Right now</CardDescription></CardHeader>
          <CardContent className="px-4 text-sm tabular">
            <div className="text-chart-1">↓ {fmtRate(live?.out_bps ?? 0)}</div>
            <div className="text-chart-2">↑ {fmtRate(live?.in_bps ?? 0)}</div>
            <div className="text-muted-foreground">{live?.active ?? 0} active</div>
          </CardContent>
        </Card>
        <Card className="gap-2 py-4">
          <CardHeader className="px-4"><CardDescription>All time</CardDescription></CardHeader>
          <CardContent className="px-4 text-sm tabular">
            <div>↓ {fmtBytes(r.total_out)}</div>
            <div>↑ {fmtBytes(r.total_in)}</div>
            <div className="text-muted-foreground">since {new Date(r.created_at * 1000).toLocaleDateString()}</div>
          </CardContent>
        </Card>
      </div>

      <div className="mt-6 grid gap-6 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Live</CardTitle>
            <CardDescription>Last 5 minutes</CardDescription>
          </CardHeader>
          <CardContent><LiveChart samples={samples} ruleId={r.id} /></CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-2">
            <div>
              <CardTitle>History</CardTitle>
              <CardDescription>
                {traffic.data
                  ? `${fmtBytes(traffic.data.total_out)} down · ${fmtBytes(traffic.data.total_in)} up · ${traffic.data.total_conns} connections`
                  : "Loading…"}
              </CardDescription>
            </div>
            <Tabs value={range} onValueChange={setRange}>
              <TabsList>
                {RANGES.map((x) => <TabsTrigger key={x} value={x} className="px-2 text-xs">{x}</TabsTrigger>)}
              </TabsList>
            </Tabs>
          </CardHeader>
          <CardContent>
            {traffic.data && <TrafficChart points={traffic.data.points} bucketS={traffic.data.bucket_s} />}
          </CardContent>
        </Card>
      </div>

      <section className="mt-8">
        <h2 className="mb-3 text-lg font-semibold">Active connections <span className="text-muted-foreground">({conns.data?.length ?? 0})</span></h2>
        <ConnectionsTable conns={conns.data ?? []} updatedAt={conns.dataUpdatedAt} showRule={false} />
      </section>

      <div className="mt-8 grid gap-6 xl:grid-cols-2">
        <section>
          <h2 className="mb-3 text-lg font-semibold">Recent events</h2>
          <Card className="py-0">
            <CardContent className="divide-y p-0">
              {events.data?.length === 0 && <p className="p-4 text-sm text-muted-foreground">No events.</p>}
              {events.data?.map((e) => (
                <div key={e.id} className="flex gap-3 px-4 py-2.5 text-sm">
                  <LevelBadge level={e.level} />
                  <span className="min-w-0 flex-1 break-words">{e.message}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">{fmtDateTime(e.ts)}</span>
                </div>
              ))}
              <div className="px-4 py-2">
                <Link to={`/events?rule=${r.id}`} className="text-xs text-muted-foreground hover:underline">All events for this rule →</Link>
              </div>
            </CardContent>
          </Card>
        </section>
        <section>
          <h2 className="mb-3 text-lg font-semibold">Bans on this rule</h2>
          <Card className="py-0">
            <CardContent className="divide-y p-0">
              {ruleBans.length === 0 && (
                <p className="p-4 text-sm text-muted-foreground">
                  No rule-specific bans. Ban a client from its connection's menu, or on the <Link to="/bans" className="underline">Bans</Link> page.
                </p>
              )}
              {ruleBans.map((b) => (
                <div key={b.id} className="flex items-center gap-3 px-4 py-2.5 text-sm">
                  <Ban className="size-4 text-destructive" />
                  <span className="font-mono">{b.cidr}</span>
                  <span className="min-w-0 flex-1 truncate text-muted-foreground">{b.reason}</span>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() =>
                      delBan.mutate(b.id, {
                        onSuccess: () => toast.success(`Unbanned ${b.cidr}`),
                        onError: (e) => toast.error(errMsg(e)),
                      })
                    }
                  >
                    Remove
                  </Button>
                </div>
              ))}
            </CardContent>
          </Card>
        </section>
      </div>
      {actions.dialogs}
    </>
  );
}
