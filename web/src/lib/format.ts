import type { Protocol, Rule } from "./types";

const UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];

export function fmtBytes(n: number, digits = 1): string {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  let i = 0;
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(i === 0 ? 0 : n >= 100 ? 0 : digits)} ${UNITS[i]}`;
}

/** Bytes/second → bit rate string (network convention). */
export function fmtRate(bytesPerSec: number): string {
  let bits = bytesPerSec * 8;
  if (!Number.isFinite(bits) || bits < 1) return "0 bit/s";
  const units = ["bit/s", "kbit/s", "Mbit/s", "Gbit/s"];
  let i = 0;
  while (bits >= 1000 && i < units.length - 1) {
    bits /= 1000;
    i++;
  }
  return `${bits.toFixed(bits >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function fmtDuration(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${sec}s`;
  return `${sec}s`;
}

export function timeAgo(ms: number, now = Date.now()): string {
  const diff = now - ms;
  if (diff < 5000) return "just now";
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

export function fmtDateTime(ms: number): string {
  return new Date(ms).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function fmtTime(ms: number): string {
  return new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function bracketHost(host: string): string {
  return host.includes(":") && !host.startsWith("[") ? `[${host}]` : host;
}

export function portSpan(start: number, end: number): string {
  return start === end ? String(start) : `${start}-${end}`;
}

export function listenStr(r: Pick<Rule, "listen_ip" | "listen_port_start" | "listen_port_end">): string {
  const ip = r.listen_ip ? bracketHost(r.listen_ip) : "*";
  return `${ip}:${portSpan(r.listen_port_start, r.listen_port_end)}`;
}

export function targetStr(r: Pick<Rule, "target_host" | "target_port_start" | "listen_port_start" | "listen_port_end">): string {
  const width = r.listen_port_end - r.listen_port_start;
  return `${bracketHost(r.target_host)}:${portSpan(r.target_port_start, r.target_port_start + width)}`;
}

export function protoLabel(p: Protocol): string {
  return p === "both" ? "TCP+UDP" : p.toUpperCase();
}

/** Parses "9987" or "27000-27050" into [start, end]; null when invalid. */
export function parsePortSpec(s: string): [number, number] | null {
  const m = s.trim().match(/^(\d{1,5})(?:\s*[-–:]\s*(\d{1,5}))?$/);
  if (!m) return null;
  const a = Number(m[1]);
  const b = m[2] ? Number(m[2]) : a;
  if (a < 1 || a > 65535 || b < 1 || b > 65535) return null;
  return [a, b];
}
