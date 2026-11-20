import { useMemo, useState } from "react";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { PageHeader } from "@/components/bits";
import { ConnectionsTable } from "@/components/ConnectionsTable";
import { useConnections, useRules } from "@/hooks/queries";

export function ConnectionsPage() {
  const [rule, setRule] = useState("all");
  const [q, setQ] = useState("");
  const [proto, setProto] = useState("all");
  const conns = useConnections(rule === "all" ? undefined : Number(rule));
  const rules = useRules();

  const filtered = useMemo(
    () =>
      (conns.data ?? []).filter(
        (c) => (proto === "all" || c.proto === proto) && (!q || c.client_ip.includes(q.trim()) || c.target.includes(q.trim())),
      ),
    [conns.data, proto, q],
  );

  return (
    <>
      <PageHeader
        title="Connections"
        description="Live TCP connections and UDP sessions across all rules. Refreshes every 2 seconds."
      />
      <div className="mb-4 flex flex-wrap gap-2">
        <Input placeholder="Filter by IP…" value={q} onChange={(e) => setQ(e.target.value)} className="w-full font-mono sm:w-56" />
        <Select value={rule} onValueChange={setRule}>
          <SelectTrigger className="w-full sm:w-56"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All rules</SelectItem>
            {(rules.data ?? []).map((r) => <SelectItem key={r.id} value={String(r.id)}>{r.name}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={proto} onValueChange={setProto}>
          <SelectTrigger className="w-full sm:w-32"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">TCP + UDP</SelectItem>
            <SelectItem value="tcp">TCP</SelectItem>
            <SelectItem value="udp">UDP</SelectItem>
          </SelectContent>
        </Select>
        <span className="ml-auto self-center text-sm text-muted-foreground tabular">{filtered.length} shown</span>
      </div>
      <ConnectionsTable conns={filtered} updatedAt={conns.dataUpdatedAt} rules={rules.data} />
    </>
  );
}
