export class ApiError extends Error {
  status: number;
  fields?: Record<string, string>;
  constructor(message: string, status: number, fields?: Record<string, string>) {
    super(message);
    this.status = status;
    this.fields = fields;
  }
}

let unauthorizedHandler: ((path: string) => void) | undefined;

/** Called whenever an API request returns 401 (session expired). */
export function onUnauthorized(fn: (path: string) => void) {
  unauthorizedHandler = fn;
}

interface Options {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  signal?: AbortSignal;
}

export async function api<T>(path: string, opts: Options = {}): Promise<T> {
  const method = opts.method ?? (opts.body !== undefined ? "POST" : "GET");
  const res = await fetch("/api" + path, {
    method,
    headers: { "Content-Type": "application/json", "X-Proxier": "1" },
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    credentials: "same-origin",
    signal: opts.signal,
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    data = undefined;
  }
  if (!res.ok) {
    const d = (data ?? {}) as { error?: string; fields?: Record<string, string> };
    if (res.status === 401 && !path.startsWith("/auth/")) unauthorizedHandler?.(path);
    throw new ApiError(d.error ?? `${res.status} ${res.statusText}`, res.status, d.fields);
  }
  return data as T;
}

/** Human readable message for an unknown error. */
export function errMsg(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.fields && Object.keys(e.fields).length) return Object.values(e.fields).join("; ");
    return e.message;
  }
  if (e instanceof Error) return e.message;
  return String(e);
}
