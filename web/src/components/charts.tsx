import { useMemo } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { ChartContainer, ChartTooltip, type ChartConfig } from "@/components/ui/chart";
import { fmtBytes, fmtRate, fmtTime } from "@/lib/format";
import type { Sample, TrafficPoint } from "@/lib/types";
import { cn } from "@/lib/utils";

const rateConfig = {
  down: { label: "Download (to clients)", color: "var(--chart-1)" },
  up: { label: "Upload (to target)", color: "var(--chart-2)" },
} satisfies ChartConfig;

interface TipProps {
  active?: boolean;
  label?: number;
  payload?: { dataKey?: string | number; value?: number; color?: string }[];
}

function Tip({ active, payload, label, fmt, labelFmt }: TipProps & { fmt: (n: number) => string; labelFmt: (n: number) => string }) {
  if (!active || !payload?.length) return null;
  return (
    <div className="rounded-lg border bg-background px-3 py-2 text-xs shadow-md">
      <div className="mb-1 font-medium">{labelFmt(label ?? 0)}</div>
      {payload.map((p) => (
        <div key={String(p.dataKey)} className="flex items-center gap-2">
          <span className="size-2 rounded-sm" style={{ background: p.color }} />
          <span className="text-muted-foreground">{rateConfig[p.dataKey as keyof typeof rateConfig]?.label ?? p.dataKey}</span>
          <span className="ml-auto pl-3 font-mono tabular">{fmt(p.value ?? 0)}</span>
        </div>
      ))}
    </div>
  );
}

function Legend() {
  return (
    <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
      <span className="flex items-center gap-1.5">
        <span className="size-2 rounded-sm bg-chart-1" /> Download (to clients)
      </span>
      <span className="flex items-center gap-1.5">
        <span className="size-2 rounded-sm bg-chart-2" /> Upload (to target)
      </span>
    </div>
  );
}

/** Live throughput for the last few minutes. ruleId filters to a single rule. */
export function LiveChart({ samples, ruleId, className }: { samples: Sample[]; ruleId?: number; className?: string }) {
  const data = useMemo(() => {
    // Pad to a fixed 5-minute window so the chart doesn't stretch while filling.
    const now = samples.at(-1)?.ts ?? 0;
    const rows = samples.map((s) => {
      if (ruleId) {
        const r = s.rules.find((x) => x.rule_id === ruleId);
        return { t: s.ts, down: r?.out_bps ?? 0, up: r?.in_bps ?? 0 };
      }
      return { t: s.ts, down: s.out_bps, up: s.in_bps };
    });
    if (now > 0 && rows[0].t > now - 299_000) rows.unshift({ t: now - 300_000, down: 0, up: 0 });
    return rows;
  }, [samples, ruleId]);

  return (
    <div className={cn("space-y-2", className)}>
      <Legend />
      <ChartContainer config={rateConfig} className="aspect-auto h-56 w-full">
        <AreaChart data={data} margin={{ left: 4, right: 8, top: 8 }}>
          <defs>
            <linearGradient id="fillDown" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="var(--color-down)" stopOpacity={0.35} />
              <stop offset="95%" stopColor="var(--color-down)" stopOpacity={0.02} />
            </linearGradient>
            <linearGradient id="fillUp" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="var(--color-up)" stopOpacity={0.35} />
              <stop offset="95%" stopColor="var(--color-up)" stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} strokeDasharray="3 3" />
          <XAxis
            dataKey="t"
            type="number"
            domain={["dataMin", "dataMax"]}
            scale="time"
            tickFormatter={(t: number) => fmtTime(t).slice(0, 5)}
            tickLine={false}
            axisLine={false}
            minTickGap={48}
          />
          <YAxis tickFormatter={(v: number) => fmtRate(v)} tickLine={false} axisLine={false} width={92} />
          <ChartTooltip content={<Tip fmt={fmtRate} labelFmt={fmtTime} />} />
          <Area dataKey="down" type="monotone" stroke="var(--color-down)" fill="url(#fillDown)" strokeWidth={1.75} isAnimationActive={false} dot={false} />
          <Area dataKey="up" type="monotone" stroke="var(--color-up)" fill="url(#fillUp)" strokeWidth={1.75} isAnimationActive={false} dot={false} />
        </AreaChart>
      </ChartContainer>
    </div>
  );
}

function bucketLabel(ts: number, bucketS: number) {
  const d = new Date(ts * 1000);
  if (bucketS >= 86400) return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  if (bucketS >= 3600) return d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit" });
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

/** Historical traffic volume per bucket. */
export function TrafficChart({ points, bucketS }: { points: TrafficPoint[]; bucketS: number }) {
  const data = useMemo(() => points.map((p) => ({ t: p.ts, down: p.bytes_out, up: p.bytes_in })), [points]);
  return (
    <div className="space-y-2">
      <Legend />
      <ChartContainer config={rateConfig} className="aspect-auto h-64 w-full">
        <AreaChart data={data} margin={{ left: 4, right: 8, top: 8 }}>
          <CartesianGrid vertical={false} strokeDasharray="3 3" />
          <XAxis dataKey="t" tickFormatter={(t: number) => bucketLabel(t, bucketS)} tickLine={false} axisLine={false} minTickGap={40} />
          <YAxis tickFormatter={(v: number) => fmtBytes(v, 0)} tickLine={false} axisLine={false} width={72} />
          <ChartTooltip
            content={
              <Tip
                fmt={(n) => fmtBytes(n)}
                labelFmt={(t) =>
                  new Date(t * 1000).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
                }
              />
            }
          />
          <Area dataKey="down" type="step" stroke="var(--color-down)" fill="var(--color-down)" fillOpacity={0.2} strokeWidth={1.5} isAnimationActive={false} />
          <Area dataKey="up" type="step" stroke="var(--color-up)" fill="var(--color-up)" fillOpacity={0.2} strokeWidth={1.5} isAnimationActive={false} />
        </AreaChart>
      </ChartContainer>
    </div>
  );
}
