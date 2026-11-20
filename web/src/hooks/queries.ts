import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type {
  AppEvent,
  AuthStatus,
  Ban,
  Conn,
  ImportResult,
  Preset,
  RuleInput,
  RuleView,
  Sample,
  Settings,
  SystemInfo,
  TrafficResp,
} from "@/lib/types";

export const qk = {
  auth: ["auth"] as const,
  rules: ["rules"] as const,
  rule: (id: number) => ["rules", id] as const,
  connections: (rule?: number) => ["connections", rule ?? 0] as const,
  bans: ["bans"] as const,
  events: ["events"] as const,
  presets: ["presets"] as const,
  settings: ["settings"] as const,
  system: ["system"] as const,
  traffic: (range: string, rule?: number) => ["traffic", range, rule ?? 0] as const,
};

export const useAuthStatus = () =>
  useQuery({ queryKey: qk.auth, queryFn: () => api<AuthStatus>("/auth/status"), staleTime: 60_000 });

export const useRules = () => useQuery({ queryKey: qk.rules, queryFn: () => api<RuleView[]>("/rules") });

export const useRule = (id: number) =>
  useQuery({ queryKey: qk.rule(id), queryFn: () => api<RuleView>(`/rules/${id}`), enabled: id > 0 });

export const useConnections = (rule?: number) =>
  useQuery({
    queryKey: qk.connections(rule),
    queryFn: () => api<Conn[]>(`/connections${rule ? `?rule=${rule}` : ""}`),
    refetchInterval: 2000,
  });

export const useBans = () => useQuery({ queryKey: qk.bans, queryFn: () => api<Ban[]>("/bans") });

export interface EventFilter {
  level?: string;
  type?: string;
  rule?: number;
  q?: string;
  limit?: number;
}

export function eventsPath(f: EventFilter, before?: number) {
  const p = new URLSearchParams();
  if (f.level) p.set("level", f.level);
  if (f.type) p.set("type", f.type);
  if (f.rule) p.set("rule", String(f.rule));
  if (f.q) p.set("q", f.q);
  if (f.limit) p.set("limit", String(f.limit));
  if (before) p.set("before", String(before));
  const s = p.toString();
  return `/events${s ? `?${s}` : ""}`;
}

export const useEvents = (f: EventFilter) =>
  useQuery({ queryKey: [...qk.events, f], queryFn: () => api<AppEvent[]>(eventsPath(f)) });

export const usePresets = () => useQuery({ queryKey: qk.presets, queryFn: () => api<Preset[]>("/presets") });

export const useSettings = () => useQuery({ queryKey: qk.settings, queryFn: () => api<Settings>("/settings") });

export const useSystem = () =>
  useQuery({ queryKey: qk.system, queryFn: () => api<SystemInfo>("/system"), staleTime: 30_000 });

export const useTraffic = (range: string, rule?: number) =>
  useQuery({
    queryKey: qk.traffic(range, rule),
    queryFn: () => api<TrafficResp>(`/stats/traffic?range=${range}${rule ? `&rule=${rule}` : ""}`),
    refetchInterval: 60_000,
  });

export const fetchLiveHistory = () => api<{ history: Sample[]; connections: number }>("/stats/live");

/** The address users should connect to: configured public address or a guess. */
export function usePublicAddress(): string {
  const settings = useSettings();
  const system = useSystem();
  return settings.data?.public_address || system.data?.guessed_address || location.hostname;
}

// ---------- mutations ----------

function useInvalidate() {
  const qc = useQueryClient();
  return (...keys: readonly (readonly unknown[])[]) => Promise.all(keys.map((k) => qc.invalidateQueries({ queryKey: k })));
}

export function useSaveRule() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (r: RuleInput) =>
      r.id ? api<RuleView>(`/rules/${r.id}`, { method: "PUT", body: r }) : api<RuleView>("/rules", { body: r }),
    onSuccess: () => inv(qk.rules),
  });
}

export function useToggleRule() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      api<RuleView>(`/rules/${id}/${enabled ? "enable" : "disable"}`, { method: "POST" }),
    onSettled: () => inv(qk.rules),
  });
}

export function useDeleteRule() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api<void>(`/rules/${id}`, { method: "DELETE" }),
    onSuccess: () => inv(qk.rules),
  });
}

export function useKick() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (id: string) => api<void>(`/connections/${id}`, { method: "DELETE" }),
    onSuccess: () => inv(["connections"]),
  });
}

export function useBanConnection() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: ({ id, global, reason }: { id: string; global: boolean; reason: string }) =>
      api<Ban>(`/connections/${id}/ban`, { body: { global, reason } }),
    onSuccess: () => inv(qk.bans, ["connections"]),
  });
}

export function useCreateBan() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (b: { cidr: string; rule_id: number | null; reason: string; expires_in_s: number }) =>
      api<Ban>("/bans", { body: b }),
    onSuccess: () => inv(qk.bans, ["connections"]),
  });
}

export function useDeleteBan() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => api<void>(`/bans/${id}`, { method: "DELETE" }),
    onSuccess: () => inv(qk.bans),
  });
}

export interface ApplyPresetItem {
  index: number;
  selected: boolean;
  listen_port_start: number;
  target_port_start: number;
}

export function useApplyPreset() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; target_host: string; listen_ip: string; name_prefix: string; items: ApplyPresetItem[] }) =>
      api<RuleView[]>(`/presets/${encodeURIComponent(id)}/apply`, { body }),
    onSuccess: () => inv(qk.rules),
  });
}

export function useCreatePreset() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (b: { name: string; description: string; rule_ids: number[] }) => api<Preset>("/presets", { body: b }),
    onSuccess: () => inv(qk.presets),
  });
}

export function useDeletePreset() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (id: string) => api<void>(`/presets/${encodeURIComponent(id)}`, { method: "DELETE" }),
    onSuccess: () => inv(qk.presets),
  });
}

export function useSaveSettings() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (s: Settings) => api<Settings>("/settings", { method: "PUT", body: s }),
    onSuccess: () => inv(qk.settings),
  });
}

export function useImport() {
  const inv = useInvalidate();
  return useMutation({
    mutationFn: ({ doc, mode }: { doc: unknown; mode: "merge" | "replace" }) =>
      api<ImportResult>(`/import?mode=${mode}`, { body: doc }),
    onSuccess: () => inv(qk.rules, qk.bans, qk.presets, qk.settings),
  });
}
