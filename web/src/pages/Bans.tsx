import { useState } from "react";
import { Link } from "react-router";
import { Ban, Loader2, ShieldOff } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState, FieldError, PageHeader } from "@/components/bits";
import { useBans, useCreateBan, useDeleteBan, useRules } from "@/hooks/queries";
import { ApiError, errMsg } from "@/lib/api";
import { fmtDateTime } from "@/lib/format";

const EXPIRY = [
  { v: "0", label: "Permanent" },
  { v: "3600", label: "1 hour" },
  { v: "86400", label: "24 hours" },
  { v: "604800", label: "7 days" },
  { v: "2592000", label: "30 days" },
];

export function BansPage() {
  const bans = useBans();
  const rules = useRules();
  const create = useCreateBan();
  const del = useDeleteBan();
  const [cidr, setCidr] = useState("");
  const [scope, setScope] = useState("global");
  const [reason, setReason] = useState("");
  const [expiry, setExpiry] = useState("0");
  const [errors, setErrors] = useState<Record<string, string>>({});

  const ruleName = (id: number | null) => (id ? rules.data?.find((r) => r.id === id)?.name ?? `#${id}` : null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrors({});
    try {
      await create.mutateAsync({
        cidr,
        rule_id: scope === "global" ? null : Number(scope),
        reason,
        expires_in_s: Number(expiry),
      });
      toast.success(`Banned ${cidr}`);
      setCidr("");
      setReason("");
    } catch (err) {
      if (err instanceof ApiError && err.fields) setErrors(err.fields);
      else toast.error(errMsg(err));
    }
  };

  return (
    <>
      <PageHeader
        title="Bans"
        description="Blocked IPs and subnets. Banned clients are disconnected immediately and can't open new connections."
      />
      <Card className="mb-6">
        <CardHeader>
          <CardTitle>Add ban</CardTitle>
          <CardDescription>Single IP (203.0.113.7) or subnet (203.0.113.0/24, 2001:db8::/32).</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="grid gap-3 md:grid-cols-[1fr_1fr_1.5fr_10rem_auto] md:items-end">
            <div className="space-y-2">
              <Label htmlFor="cidr">IP or CIDR</Label>
              <Input id="cidr" value={cidr} onChange={(e) => setCidr(e.target.value)} className="font-mono" placeholder="203.0.113.7" />
            </div>
            <div className="space-y-2">
              <Label>Applies to</Label>
              <Select value={scope} onValueChange={setScope}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">All rules</SelectItem>
                  {(rules.data ?? []).map((r) => <SelectItem key={r.id} value={String(r.id)}>{r.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="reason">Reason</Label>
              <Input id="reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Optional" />
            </div>
            <div className="space-y-2">
              <Label>Duration</Label>
              <Select value={expiry} onValueChange={setExpiry}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {EXPIRY.map((x) => <SelectItem key={x.v} value={x.v}>{x.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <Button type="submit" disabled={!cidr.trim() || create.isPending}>
              {create.isPending ? <Loader2 className="animate-spin" /> : <Ban />} Ban
            </Button>
          </form>
          <div className="mt-2 space-y-1">
            {Object.values(errors).map((m) => <FieldError key={m} msg={m} />)}
          </div>
        </CardContent>
      </Card>

      {bans.data?.length === 0 ? (
        <EmptyState icon={<ShieldOff />} title="No bans">Everyone can connect to your forwarded ports.</EmptyState>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Address</TableHead>
                <TableHead>Scope</TableHead>
                <TableHead>Reason</TableHead>
                <TableHead>Added</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(bans.data ?? []).map((b) => (
                <TableRow key={b.id}>
                  <TableCell className="font-mono">{b.cidr}</TableCell>
                  <TableCell>
                    {b.rule_id ? <Link to={`/rules/${b.rule_id}`} className="hover:underline">{ruleName(b.rule_id)}</Link> : <span className="text-muted-foreground">All rules</span>}
                  </TableCell>
                  <TableCell className="max-w-64 truncate text-muted-foreground">{b.reason || "—"}</TableCell>
                  <TableCell className="text-xs whitespace-nowrap">{fmtDateTime(b.created_at * 1000)}</TableCell>
                  <TableCell className="text-xs whitespace-nowrap">{b.expires_at ? fmtDateTime(b.expires_at * 1000) : "Never"}</TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        del.mutate(b.id, {
                          onSuccess: () => toast.success(`Unbanned ${b.cidr}`),
                          onError: (e) => toast.error(errMsg(e)),
                        })
                      }
                    >
                      Remove
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </>
  );
}
