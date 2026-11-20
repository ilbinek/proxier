import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { ArrowLeftRight, Copy, LayoutTemplate, MoreHorizontal, Pencil, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState, FieldError, PageHeader, ProtoBadge, StatusBadge } from "@/components/bits";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { RuleEditor } from "@/components/RuleEditor";
import { useCreatePreset, useDeleteRule, useRules, useToggleRule } from "@/hooks/queries";
import { ApiError, errMsg } from "@/lib/api";
import { fmtBytes, fmtRate, listenStr, targetStr } from "@/lib/format";
import { useLive } from "@/lib/live";
import type { Rule, RuleView } from "@/lib/types";

export function useRuleActions() {
  const toggle = useToggleRule();
  const del = useDeleteRule();
  const [editing, setEditing] = useState<{ rule?: Rule; template?: Partial<Rule> } | null>(null);
  const [deleting, setDeleting] = useState<RuleView | null>(null);
  const navigate = useNavigate();

  const setEnabled = (r: RuleView, enabled: boolean) =>
    toggle.mutate(
      { id: r.id, enabled },
      {
        onSuccess: (v) =>
          v.status.state === "error"
            ? toast.error(`“${r.name}” enabled but failed to start`, { description: v.status.error })
            : toast.success(`“${r.name}” ${enabled ? "enabled" : "disabled"}`),
        onError: (e) => toast.error(`Can't enable “${r.name}”`, { description: errMsg(e) }),
      },
    );

  const duplicate = (r: Rule) => {
    const copy: Partial<Rule> = { ...r, name: `${r.name} (copy)`, enabled: false };
    delete copy.id;
    setEditing({ template: copy });
  };

  const dialogs = (
    <>
      <RuleEditor open={!!editing} onOpenChange={(o) => !o && setEditing(null)} rule={editing?.rule} template={editing?.template} />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete “${deleting?.name}”?`}
        description="The port is released immediately, active connections are closed and the rule's traffic history is removed."
        confirmLabel="Delete"
        destructive
        onConfirm={async () => {
          if (!deleting) return;
          try {
            await del.mutateAsync(deleting.id);
            toast.success(`Deleted “${deleting.name}”`);
            navigate("/rules");
          } catch (e) {
            toast.error(errMsg(e));
          }
        }}
      />
    </>
  );

  return { setEnabled, edit: (r: Rule) => setEditing({ rule: r }), create: () => setEditing({}), duplicate, remove: setDeleting, dialogs };
}

type Actions = ReturnType<typeof useRuleActions>;

function RuleMenu({ r, actions, className }: { r: RuleView; actions: Actions; className?: string }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" className={className} aria-label="Actions">
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => actions.edit(r)}><Pencil /> Edit</DropdownMenuItem>
        <DropdownMenuItem onClick={() => actions.duplicate(r)}><Copy /> Duplicate</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={() => actions.remove(r)}><Trash2 /> Delete</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function SavePresetDialog({ ids, open, onOpenChange }: { ids: number[]; open: boolean; onOpenChange: (o: boolean) => void }) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [err, setErr] = useState("");
  const create = useCreatePreset();
  useEffect(() => {
    if (open) {
      setName("");
      setDesc("");
      setErr("");
    }
  }, [open]);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await create.mutateAsync({ name, description: desc, rule_ids: ids });
      toast.success(`Preset “${name}” saved`);
      onOpenChange(false);
    } catch (e) {
      setErr(e instanceof ApiError ? errMsg(e) : String(e));
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Save {ids.length} rule{ids.length === 1 ? "" : "s"} as preset</DialogTitle>
          <DialogDescription>Ports and protocols are saved; the target host is asked for when you apply it.</DialogDescription>
        </DialogHeader>
        <form id="save-preset" onSubmit={submit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="pn">Name</Label>
            <Input id="pn" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </div>
          <div className="space-y-2">
            <Label htmlFor="pd">Description</Label>
            <Input id="pd" value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="Optional" />
          </div>
          <FieldError msg={err} />
        </form>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button type="submit" form="save-preset" disabled={!name.trim() || create.isPending}>Save preset</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function RulesPage() {
  const rules = useRules();
  const byRule = useLive((s) => s.byRule);
  const actions = useRuleActions();
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [savingPreset, setSavingPreset] = useState(false);
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();

  useEffect(() => {
    if (params.get("new")) {
      actions.create();
      setParams({}, { replace: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const list = rules.data ?? [];
  const allSelected = list.length > 0 && list.every((r) => selected.has(r.id));
  const toggleSel = (id: number) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  return (
    <>
      <PageHeader
        title="Rules"
        description="Each rule listens on a port of this server and forwards to a target."
        actions={
          <>
            {selected.size > 0 && (
              <Button variant="outline" onClick={() => setSavingPreset(true)}>
                <Save /> Save {selected.size} as preset
              </Button>
            )}
            <Button variant="outline" onClick={() => navigate("/presets")}>
              <LayoutTemplate /> From preset
            </Button>
            <Button onClick={actions.create}>
              <Plus /> New rule
            </Button>
          </>
        }
      />

      {rules.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => <Skeleton key={i} className="h-12 w-full" />)}
        </div>
      ) : list.length === 0 ? (
        <EmptyState icon={<ArrowLeftRight />} title="No rules yet">
          Create a rule like “UDP 9987 → ts.example.ru:9987”, or start from a preset.
          <div className="mt-4 flex justify-center gap-2">
            <Button size="sm" onClick={actions.create}><Plus /> New rule</Button>
            <Button size="sm" variant="outline" onClick={() => navigate("/presets")}><LayoutTemplate /> Presets</Button>
          </div>
        </EmptyState>
      ) : (
        <>
          {/* Phones: one card per rule */}
          <div className="space-y-3 md:hidden">
            {list.map((r) => {
              const live = byRule[r.id];
              return (
                <div key={r.id} className="rounded-lg border p-4">
                  <div className="flex items-start gap-3">
                    <Link to={`/rules/${r.id}`} className="min-w-0 flex-1">
                      <div className="truncate font-medium">{r.name}</div>
                      <div className="mt-1 flex flex-wrap items-center gap-1.5 font-mono text-xs text-muted-foreground">
                        <ProtoBadge proto={r.protocol} />
                        <span className="break-all">{listenStr(r)} → {targetStr(r)}</span>
                      </div>
                    </Link>
                    <Switch checked={r.enabled} onCheckedChange={(v) => actions.setEnabled(r, v)} aria-label="Enabled" />
                  </div>
                  <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                    <StatusBadge state={r.status.state} error={r.status.error} />
                    <span className="tabular">
                      <span className="text-chart-1">↓ {fmtRate(live?.out_bps ?? 0)}</span>{" "}
                      <span className="text-chart-2">↑ {fmtRate(live?.in_bps ?? 0)}</span>
                    </span>
                    <span className="tabular text-muted-foreground">
                      {live?.active ?? 0} conn · {fmtBytes(r.total_in + r.total_out)}
                    </span>
                    <RuleMenu r={r} actions={actions} className="ml-auto" />
                  </div>
                  {r.status.state === "error" && <p className="mt-2 text-xs text-destructive">{r.status.error}</p>}
                </div>
              );
            })}
          </div>

          {/* Tablets and up: table */}
          <div className="hidden overflow-x-auto rounded-lg border md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8">
                    <Checkbox
                      checked={allSelected}
                      onCheckedChange={(v) => setSelected(v ? new Set(list.map((r) => r.id)) : new Set())}
                      aria-label="Select all"
                    />
                  </TableHead>
                  <TableHead className="w-12">On</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Forwarding</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Live ↓ / ↑</TableHead>
                  <TableHead className="text-right">Conns</TableHead>
                  <TableHead className="text-right">Total</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.map((r) => {
                  const live = byRule[r.id];
                  return (
                    <TableRow key={r.id} data-state={selected.has(r.id) ? "selected" : undefined}>
                      <TableCell>
                        <Checkbox checked={selected.has(r.id)} onCheckedChange={() => toggleSel(r.id)} aria-label={`Select ${r.name}`} />
                      </TableCell>
                      <TableCell>
                        <Switch checked={r.enabled} onCheckedChange={(v) => actions.setEnabled(r, v)} aria-label="Enabled" />
                      </TableCell>
                      <TableCell className="max-w-56">
                        <Link to={`/rules/${r.id}`} className="block truncate font-medium hover:underline">{r.name}</Link>
                        {r.notes && <p className="truncate text-xs text-muted-foreground">{r.notes}</p>}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2 whitespace-nowrap font-mono text-xs">
                          <ProtoBadge proto={r.protocol} />
                          <span>{listenStr(r)}</span>
                          <span className="text-muted-foreground">→</span>
                          <span>{targetStr(r)}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <StatusBadge state={r.status.state} error={r.status.error} />
                      </TableCell>
                      <TableCell className="text-right text-xs tabular whitespace-nowrap">
                        <span className="text-chart-1">{fmtRate(live?.out_bps ?? 0)}</span>
                        {" / "}
                        <span className="text-chart-2">{fmtRate(live?.in_bps ?? 0)}</span>
                      </TableCell>
                      <TableCell className="text-right tabular">{live?.active ?? 0}</TableCell>
                      <TableCell className="text-right text-xs tabular whitespace-nowrap text-muted-foreground">
                        {fmtBytes(r.total_in + r.total_out)}
                      </TableCell>
                      <TableCell>
                        <RuleMenu r={r} actions={actions} />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        </>
      )}
      {actions.dialogs}
      <SavePresetDialog
        ids={[...selected]}
        open={savingPreset}
        onOpenChange={(o) => {
          setSavingPreset(o);
          if (!o) setSelected(new Set());
        }}
      />
    </>
  );
}
