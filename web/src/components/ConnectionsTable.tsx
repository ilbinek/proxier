import { useEffect, useRef, useState } from "react";
import { Ban, MoreHorizontal, Plug, Unplug } from "lucide-react";
import { toast } from "sonner";
import { Link } from "react-router";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState, ProtoBadge } from "@/components/bits";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { useBanConnection, useKick } from "@/hooks/queries";
import { useNow } from "@/hooks/useNow";
import { errMsg } from "@/lib/api";
import { bracketHost, fmtBytes, fmtDuration, fmtRate } from "@/lib/format";
import type { Conn, RuleView } from "@/lib/types";

type Snap = { t: number; i: number; o: number; rate: [number, number] };

/** Per-connection rate computed from consecutive polls (keyed on fetch time). */
function useRates(conns: Conn[], updatedAt: number) {
  const prev = useRef(new Map<string, Snap>());
  const [rates, setRates] = useState(new Map<string, [number, number]>());
  useEffect(() => {
    const now = updatedAt || Date.now();
    const next = new Map<string, Snap>();
    const out = new Map<string, [number, number]>();
    for (const c of conns) {
      const p = prev.current.get(c.id);
      let rate: [number, number] = [0, 0];
      if (p && now > p.t) {
        const dt = (now - p.t) / 1000;
        rate = [(c.bytes_out - p.o) / dt, (c.bytes_in - p.i) / dt];
      }
      next.set(c.id, { t: now, i: c.bytes_in, o: c.bytes_out, rate });
      out.set(c.id, rate);
    }
    prev.current = next;
    setRates(out);
  }, [conns, updatedAt]);
  return (id: string) => rates.get(id) ?? [0, 0];
}

export function ConnectionsTable({
  conns,
  updatedAt = 0,
  rules,
  showRule = true,
}: {
  conns: Conn[];
  /** dataUpdatedAt of the query, so rates refresh even when bytes didn't change. */
  updatedAt?: number;
  rules?: RuleView[];
  showRule?: boolean;
}) {
  const rate = useRates(conns, updatedAt);
  const now = useNow(1000);
  const kick = useKick();
  const ban = useBanConnection();
  const [banTarget, setBanTarget] = useState<{ conn: Conn; global: boolean } | null>(null);
  const ruleName = (id: number) => rules?.find((r) => r.id === id)?.name ?? `#${id}`;

  if (conns.length === 0) {
    return (
      <EmptyState icon={<Plug />} title="No active connections">
        Connections and UDP sessions appear here while clients are using a rule.
      </EmptyState>
    );
  }

  return (
    <>
      <div className="overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Client</TableHead>
              {showRule && <TableHead>Rule</TableHead>}
              <TableHead>Port</TableHead>
              <TableHead>Target</TableHead>
              <TableHead className="text-right">Duration</TableHead>
              <TableHead className="text-right">↓ / ↑ now</TableHead>
              <TableHead className="text-right">Transferred</TableHead>
              <TableHead className="w-10" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {conns.map((c) => {
              const [down, up] = rate(c.id);
              return (
                <TableRow key={c.id}>
                  <TableCell className="font-mono text-xs">
                    <div className="flex items-center gap-2">
                      <ProtoBadge proto={c.proto} />
                      <span>{bracketHost(c.client_ip)}:{c.client_port}</span>
                    </div>
                  </TableCell>
                  {showRule && (
                    <TableCell>
                      <Link to={`/rules/${c.rule_id}`} className="hover:underline">{ruleName(c.rule_id)}</Link>
                    </TableCell>
                  )}
                  <TableCell className="font-mono text-xs">{c.listen_port}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{c.target}</TableCell>
                  <TableCell className="text-right tabular text-xs">{fmtDuration(now - c.started)}</TableCell>
                  <TableCell className="text-right tabular text-xs whitespace-nowrap">
                    <span className="text-chart-1">{fmtRate(down)}</span> / <span className="text-chart-2">{fmtRate(up)}</span>
                  </TableCell>
                  <TableCell className="text-right tabular text-xs whitespace-nowrap">
                    {fmtBytes(c.bytes_out)} / {fmtBytes(c.bytes_in)}
                  </TableCell>
                  <TableCell>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon-sm" aria-label="Actions">
                          <MoreHorizontal />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          onClick={() =>
                            kick.mutate(c.id, {
                              onSuccess: () => toast.success(`Disconnected ${c.client_ip}`),
                              onError: (e) => toast.error(errMsg(e)),
                            })
                          }
                        >
                          <Unplug /> Kick
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onClick={() => setBanTarget({ conn: c, global: false })}>
                          <Ban /> Ban IP on this rule
                        </DropdownMenuItem>
                        <DropdownMenuItem variant="destructive" onClick={() => setBanTarget({ conn: c, global: true })}>
                          <Ban /> Ban IP everywhere
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
      <ConfirmDialog
        open={!!banTarget}
        onOpenChange={(o) => !o && setBanTarget(null)}
        title={`Ban ${banTarget?.conn.client_ip}?`}
        description={
          banTarget?.global
            ? "The IP will be disconnected and blocked on all rules until you remove the ban."
            : `The IP will be disconnected and blocked on “${banTarget ? ruleName(banTarget.conn.rule_id) : ""}”.`
        }
        confirmLabel="Ban"
        destructive
        onConfirm={async () => {
          if (!banTarget) return;
          try {
            await ban.mutateAsync({ id: banTarget.conn.id, global: banTarget.global, reason: "Banned from live connections" });
            toast.success(`Banned ${banTarget.conn.client_ip}`);
          } catch (e) {
            toast.error(errMsg(e));
          }
        }}
      />
    </>
  );
}
