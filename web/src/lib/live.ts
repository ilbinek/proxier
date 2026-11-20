import { create } from "zustand";
import type { AppEvent, RuleRate, Sample } from "./types";

const MAX_SAMPLES = 300;

type WsStatus = "connecting" | "open" | "closed";

interface LiveState {
  status: WsStatus;
  samples: Sample[];
  byRule: Record<number, RuleRate>;
  setStatus: (s: WsStatus) => void;
  seed: (samples: Sample[]) => void;
  push: (s: Sample) => void;
}

function index(s?: Sample): Record<number, RuleRate> {
  const out: Record<number, RuleRate> = {};
  for (const r of s?.rules ?? []) out[r.rule_id] = r;
  return out;
}

export const useLive = create<LiveState>((set) => ({
  status: "connecting",
  samples: [],
  byRule: {},
  setStatus: (status) => set({ status }),
  seed: (samples) =>
    set((st) => {
      // Merge: keep any samples received over WS that are newer than the seed.
      const last = samples.at(-1)?.ts ?? 0;
      const merged = [...samples, ...st.samples.filter((x) => x.ts > last)].slice(-MAX_SAMPLES);
      return { samples: merged, byRule: index(merged.at(-1)) };
    }),
  push: (s) =>
    set((st) => {
      const samples = st.samples.length >= MAX_SAMPLES ? [...st.samples.slice(1), s] : [...st.samples, s];
      return { samples, byRule: index(s) };
    }),
}));

type Listener = (e: AppEvent) => void;
const listeners = new Set<Listener>();

/** Subscribe to live events from the WebSocket. Returns an unsubscribe function. */
export function onLiveEvent(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Opens the WebSocket and keeps it connected with exponential backoff. */
export function connectLive(): () => void {
  let ws: WebSocket | undefined;
  let stopped = false;
  let retry = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const open = () => {
    if (stopped) return;
    useLive.getState().setStatus("connecting");
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    ws = new WebSocket(`${proto}//${location.host}/api/ws`);
    ws.onopen = () => {
      retry = 0;
      useLive.getState().setStatus("open");
    };
    ws.onmessage = (msg) => {
      try {
        const m = JSON.parse(msg.data as string) as { type: string; data: unknown };
        if (m.type === "stats") useLive.getState().push(m.data as Sample);
        else if (m.type === "event") for (const l of listeners) l(m.data as AppEvent);
      } catch {
        /* ignore malformed frames */
      }
    };
    ws.onclose = () => {
      useLive.getState().setStatus("closed");
      if (stopped) return;
      const delay = Math.min(30_000, 1000 * 2 ** retry++);
      timer = setTimeout(open, delay);
    };
  };
  open();
  return () => {
    stopped = true;
    clearTimeout(timer);
    ws?.close();
  };
}
