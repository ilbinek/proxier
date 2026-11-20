export type Protocol = "tcp" | "udp" | "both";

export interface Rule {
  id: number;
  name: string;
  enabled: boolean;
  protocol: Protocol;
  listen_ip: string;
  listen_port_start: number;
  listen_port_end: number;
  target_host: string;
  target_port_start: number;
  udp_idle_timeout_s: number;
  notes: string;
  preset_group: string;
  created_at: number;
  updated_at: number;
}

export type RuleState = "running" | "error" | "disabled";

export interface RuleStatus {
  state: RuleState;
  error?: string;
  listeners: number;
  expected: number;
}

export interface RuleView extends Rule {
  status: RuleStatus;
  total_in: number;
  total_out: number;
}

export type RuleInput = Omit<Rule, "id" | "created_at" | "updated_at" | "preset_group"> & { id?: number };

export interface Conn {
  id: string;
  rule_id: number;
  proto: "tcp" | "udp";
  client_ip: string;
  client_port: number;
  listen_port: number;
  target: string;
  started: number;
  last_active: number;
  bytes_in: number;
  bytes_out: number;
}

export interface Ban {
  id: number;
  cidr: string;
  rule_id: number | null;
  reason: string;
  created_at: number;
  expires_at: number | null;
}

export interface PresetRule {
  name: string;
  protocol: Protocol;
  port_start: number;
  port_end: number;
  target_port_start?: number;
  optional: boolean;
  same_port: boolean;
  notes?: string;
  udp_idle_timeout_s?: number;
}

export interface Preset {
  id: string;
  name: string;
  description: string;
  builtin: boolean;
  rules: PresetRule[];
  hint?: string;
  created_at?: number;
}

export type EventLevel = "info" | "warn" | "error";

export interface AppEvent {
  id: number;
  ts: number;
  level: EventLevel;
  type: string;
  rule_id: number | null;
  client_ip: string;
  message: string;
  meta?: Record<string, unknown>;
}

export interface RuleRate {
  rule_id: number;
  in_bps: number;
  out_bps: number;
  active: number;
}

export interface Sample {
  ts: number;
  in_bps: number;
  out_bps: number;
  active: number;
  rules: RuleRate[];
}

export interface TrafficPoint {
  ts: number;
  bytes_in: number;
  bytes_out: number;
  conns_opened: number;
  peak_conns: number;
}

export interface TrafficResp {
  bucket_s: number;
  points: TrafficPoint[];
  total_in: number;
  total_out: number;
  total_conns: number;
}

export interface Settings {
  public_address: string;
  retention_minute_days: number;
  retention_hour_days: number;
  retention_events_days: number;
}

export interface SystemInfo {
  version: string;
  go_version: string;
  os: string;
  started_at: number;
  listen: string;
  tls_mode: string;
  acme_domain: string;
  data_dir: string;
  trust_proxy: boolean;
  interface_ips: string[] | null;
  guessed_address: string;
  connections: number;
}

export interface AuthStatus {
  needs_setup: boolean;
  authenticated: boolean;
  version: string;
}

export interface ImportResult {
  rules: number;
  rules_disabled: number;
  bans: number;
  presets: number;
}
