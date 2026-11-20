import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Loader2, ScrollText } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { EmptyState, LevelBadge, PageHeader } from "@/components/bits";
import { eventsPath, qk, useRules, type EventFilter } from "@/hooks/queries";
import { api } from "@/lib/api";
import { fmtDateTime } from "@/lib/format";
import type { AppEvent } from "@/lib/types";

const PAGE = 100;

const TYPES = [
  "conn_open", "conn_close", "conn_rejected", "conn_kicked", "dial_error",
  "rule_started", "rule_error", "rule_stopped", "rule_created", "rule_updated", "rule_deleted", "rule_enabled", "rule_disabled",
  "ban_added", "ban_removed", "preset_applied", "preset_created",
  "login", "login_failed", "login_throttled", "password_changed", "setup_complete", "setup_failed",
  "settings_changed", "config_imported", "server_started", "server_stopped",
];

export function EventsPage() {
  const [params, setParams] = useSearchParams();
  const [q, setQ] = useState(params.get("q") ?? "");
  const [debouncedQ, setDebouncedQ] = useState(q);
  const level = params.get("level") ?? "all";
  const type = params.get("type") ?? "all";
  const rule = params.get("rule") ?? "all";
  const rules = useRules();

  useEffect(() => {
    const t = setTimeout(() => setDebouncedQ(q), 300);
    return () => clearTimeout(t);
  }, [q]);

  const setParam = (k: string, v: string) => {
    const n = new URLSearchParams(params);
    if (v === "all" || !v) n.delete(k);
    else n.set(k, v);
    setParams(n, { replace: true });
  };

  const filter: EventFilter = {
    level: level === "all" ? undefined : level,
    type: type === "all" ? undefined : type,
    rule: rule === "all" ? undefined : Number(rule),
    q: debouncedQ || undefined,
    limit: PAGE,
  };

  const events = useInfiniteQuery({
    queryKey: [...qk.events, "list", filter],
    queryFn: ({ pageParam }) => api<AppEvent[]>(eventsPath(filter, pageParam || undefined)),
    initialPageParam: 0,
    getNextPageParam: (last) => (last.length === PAGE ? last[last.length - 1].id : undefined),
  });

  const all = events.data?.pages.flat() ?? [];
  const ruleName = (id: number | null) => (id ? rules.data?.find((r) => r.id === id)?.name ?? `#${id}` : null);

  return (
    <>
      <PageHeader title="Events" description="Connections, errors, configuration changes and logins. Updates live." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Input placeholder="Search message or IP…" value={q} onChange={(e) => setQ(e.target.value)} className="w-full sm:w-64" />
        <Select value={level} onValueChange={(v) => setParam("level", v)}>
          <SelectTrigger className="w-full sm:w-32"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All levels</SelectItem>
            <SelectItem value="info">Info</SelectItem>
            <SelectItem value="warn">Warning</SelectItem>
            <SelectItem value="error">Error</SelectItem>
          </SelectContent>
        </Select>
        <Select value={type} onValueChange={(v) => setParam("type", v)}>
          <SelectTrigger className="w-full sm:w-48"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All types</SelectItem>
            {TYPES.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={rule} onValueChange={(v) => setParam("rule", v)}>
          <SelectTrigger className="w-full sm:w-48"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All rules</SelectItem>
            {(rules.data ?? []).map((r) => <SelectItem key={r.id} value={String(r.id)}>{r.name}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      {events.isLoading ? (
        <Loader2 className="mx-auto mt-12 animate-spin text-muted-foreground" />
      ) : all.length === 0 ? (
        <EmptyState icon={<ScrollText />} title="No matching events" />
      ) : (
        <div className="divide-y rounded-lg border">
          {all.map((e) => (
            <div key={e.id} className="grid gap-x-4 gap-y-1 px-4 py-2.5 text-sm sm:grid-cols-[11rem_4rem_1fr_auto]">
              <span className="text-xs tabular text-muted-foreground sm:pt-0.5">{fmtDateTime(e.ts)}</span>
              <LevelBadge level={e.level} />
              <div className="min-w-0">
                <p className="break-words">{e.message}</p>
                <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">
                  {e.type}
                  {e.client_ip && <> · <button className="hover:underline" onClick={() => setQ(e.client_ip)}>{e.client_ip}</button></>}
                </p>
              </div>
              <span className="text-xs sm:text-right">
                {e.rule_id && <Link to={`/rules/${e.rule_id}`} className="text-muted-foreground hover:underline">{ruleName(e.rule_id)}</Link>}
              </span>
            </div>
          ))}
        </div>
      )}
      {events.hasNextPage && (
        <div className="mt-4 flex justify-center">
          <Button variant="outline" onClick={() => events.fetchNextPage()} disabled={events.isFetchingNextPage}>
            {events.isFetchingNextPage && <Loader2 className="animate-spin" />} Load older
          </Button>
        </div>
      )}
    </>
  );
}
