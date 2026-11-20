import { Link, useNavigate } from "react-router";
import { Activity, ArrowDown, ArrowLeftRight, ArrowRight, ArrowUp, Cable, Database, LayoutTemplate, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { LevelBadge, PageHeader, ProtoBadge, StatusDot } from "@/components/bits";
import { LiveChart } from "@/components/charts";
import { useEvents, useRules, useTraffic } from "@/hooks/queries";
import { fmtBytes, fmtRate, listenStr, targetStr, timeAgo } from "@/lib/format";
import { useLive } from "@/lib/live";

function Stat({ icon, label, value, sub }: { icon: React.ReactNode; label: string; value: React.ReactNode; sub?: React.ReactNode }) {
  return (
    <Card className="gap-2 py-4">
      <CardHeader className="flex flex-row items-center justify-between px-4">
        <CardDescription className="text-xs font-medium uppercase tracking-wide">{label}</CardDescription>
        <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>
      </CardHeader>
      <CardContent className="px-4">
        <div className="text-2xl font-semibold tabular tracking-tight">{value}</div>
        {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
      </CardContent>
    </Card>
  );
}

export function DashboardPage() {
  const rules = useRules();
  const samples = useLive((s) => s.samples);
  const byRule = useLive((s) => s.byRule);
  const latest = samples.at(-1);
  const traffic = useTraffic("24h");
  const events = useEvents({ limit: 8 });
  const navigate = useNavigate();

  const list = rules.data ?? [];
  const running = list.filter((r) => r.status.state === "running").length;
  const errors = list.filter((r) => r.status.state === "error").length;
  const top = [...list]
    .sort((a, b) => {
      const ra = byRule[a.id], rb = byRule[b.id];
      const va = (ra?.in_bps ?? 0) + (ra?.out_bps ?? 0) + (ra?.active ?? 0);
      const vb = (rb?.in_bps ?? 0) + (rb?.out_bps ?? 0) + (rb?.active ?? 0);
      return vb - va || Number(b.enabled) - Number(a.enabled) || a.listen_port_start - b.listen_port_start;
    })
    .slice(0, 8);

  return (
    <>
      <PageHeader
        title="Dashboard"
        description="Live overview of everything this server is forwarding."
        actions={
          <Button onClick={() => navigate("/rules?new=1")}>
            <Plus /> New rule
          </Button>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Stat
          icon={<Activity />}
          label="Throughput"
          value={
            <span className="flex items-baseline gap-3 text-xl">
              <span className="flex items-center gap-1 text-chart-1"><ArrowDown className="size-4" />{fmtRate(latest?.out_bps ?? 0)}</span>
            </span>
          }
          sub={<span className="flex items-center gap-1 text-chart-2"><ArrowUp className="size-3" />{fmtRate(latest?.in_bps ?? 0)} upload</span>}
        />
        <Stat icon={<Cable />} label="Active connections" value={latest?.active ?? 0} sub="TCP connections + UDP sessions" />
        <Stat
          icon={<ArrowLeftRight />}
          label="Rules"
          value={rules.isLoading ? <Skeleton className="h-8 w-16" /> : `${running} / ${list.length}`}
          sub={errors > 0 ? <span className="text-destructive">{errors} with errors</span> : "running / total"}
        />
        <Stat
          icon={<Database />}
          label="Traffic, last 24h"
          value={fmtBytes((traffic.data?.total_in ?? 0) + (traffic.data?.total_out ?? 0))}
          sub={`${traffic.data?.total_conns ?? 0} connections opened`}
        />
      </div>

      {!rules.isLoading && list.length === 0 && (
        <Card className="mt-6 border-dashed">
          <CardHeader>
            <CardTitle>Forward your first port</CardTitle>
            <CardDescription>
              Pick a preset like TeamSpeak 3 or Minecraft, or create a custom rule: “port X on this server → IP:port elsewhere”.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            <Button onClick={() => navigate("/presets")}>
              <LayoutTemplate /> Browse presets
            </Button>
            <Button variant="outline" onClick={() => navigate("/rules?new=1")}>
              <Plus /> Custom rule
            </Button>
          </CardContent>
        </Card>
      )}

      <div className="mt-6 grid gap-6 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader>
            <CardTitle>Live throughput</CardTitle>
            <CardDescription>All rules, last 5 minutes</CardDescription>
          </CardHeader>
          <CardContent>
            <LiveChart samples={samples} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <div>
              <CardTitle>Recent events</CardTitle>
              <CardDescription>Connections, errors, changes</CardDescription>
            </div>
            <Button variant="ghost" size="sm" asChild>
              <Link to="/events">All <ArrowRight /></Link>
            </Button>
          </CardHeader>
          <CardContent className="space-y-3">
            {events.data?.length === 0 && <p className="text-sm text-muted-foreground">Nothing yet.</p>}
            {events.data?.map((e) => (
              <div key={e.id} className="flex gap-2 text-sm">
                <LevelBadge level={e.level} />
                <div className="min-w-0 flex-1">
                  <p className="line-clamp-2 leading-snug">{e.message}</p>
                  <p className="text-xs text-muted-foreground">{timeAgo(e.ts)}</p>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>

      {list.length > 0 && (
        <Card className="mt-6">
          <CardHeader className="flex flex-row items-center justify-between">
            <div>
              <CardTitle>Rules</CardTitle>
              <CardDescription>Busiest first</CardDescription>
            </div>
            <Button variant="ghost" size="sm" asChild>
              <Link to="/rules">Manage <ArrowRight /></Link>
            </Button>
          </CardHeader>
          <CardContent className="divide-y p-0">
            {top.map((r) => {
              const live = byRule[r.id];
              return (
                <Link key={r.id} to={`/rules/${r.id}`} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-6 py-3 transition-colors hover:bg-muted/50">
                  <StatusDot state={r.status.state} />
                  <span className="min-w-32 flex-1 font-medium">{r.name}</span>
                  <ProtoBadge proto={r.protocol} />
                  <span className="hidden font-mono text-xs text-muted-foreground md:inline">
                    {listenStr(r)} → {targetStr(r)}
                  </span>
                  <span className="w-40 text-right text-xs tabular">
                    <span className="text-chart-1">↓ {fmtRate(live?.out_bps ?? 0)}</span>{" "}
                    <span className="text-chart-2">↑ {fmtRate(live?.in_bps ?? 0)}</span>
                  </span>
                  <span className="w-16 text-right text-xs tabular text-muted-foreground">{live?.active ?? 0} conn</span>
                </Link>
              );
            })}
          </CardContent>
        </Card>
      )}
    </>
  );
}
