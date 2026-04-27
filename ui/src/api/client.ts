/**
 * Daemon HTTP client. Resolves the daemon endpoint via Tauri's Rust
 * side (which reads the lockfile), then issues bearer-authenticated
 * requests against the loopback API.
 */

import { invoke } from "@tauri-apps/api/core";
import type {
  DaemonEndpoint,
  Prefs,
  Rule,
  Server,
  Status,
  Subscription,
} from "./types";

let cached: DaemonEndpoint | null = null;

/**
 * resolveEndpoint returns the cached endpoint or asks the Rust side to
 * read the lockfile. The lockfile may not exist if mosaicd isn't
 * running; in that case Tauri rejects with an error message.
 */
export async function resolveEndpoint(force = false): Promise<DaemonEndpoint> {
  if (!force && cached) return cached;
  cached = await invoke<DaemonEndpoint>("daemon_endpoint");
  return cached;
}

function baseURL(ep: DaemonEndpoint): string {
  return `http://${ep.host}:${ep.port}`;
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const ep = await resolveEndpoint();
  const res = await fetch(baseURL(ep) + path, {
    method,
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${ep.token}`,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    let msg = `${method} ${path} → ${res.status}`;
    try {
      const j = (await res.json()) as { error?: string };
      if (j.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  status: () => request<Status>("GET", "/v1/status"),
  connect: (serverId: string) =>
    request<Status>("POST", "/v1/connect", { server_id: serverId }),
  disconnect: () => request<Status>("POST", "/v1/disconnect"),

  listServers: () => request<Server[]>("GET", "/v1/servers"),

  listSubscriptions: () => request<Subscription[]>("GET", "/v1/subscriptions"),
  addSubscription: (url: string, name?: string) =>
    request<Subscription>("POST", "/v1/subscriptions", { url, name }),
  refreshSubscription: (id: string) =>
    request<Subscription>("POST", `/v1/subscriptions/${id}/refresh`),
  deleteSubscription: (id: string) =>
    request<void>("DELETE", `/v1/subscriptions/${id}`),

  listRules: () => request<Rule[]>("GET", "/v1/rules"),
  addRule: (rule: Partial<Rule>) => request<Rule>("POST", "/v1/rules", rule),
  deleteRule: (id: string) => request<void>("DELETE", `/v1/rules/${id}`),

  getPrefs: () => request<Prefs>("GET", "/v1/prefs"),
  setPrefs: (prefs: Prefs) => request<Prefs>("PUT", "/v1/prefs", prefs),
};

/**
 * subscribeStatus consumes the daemon's /v1/events SSE stream. We use
 * fetch() with a manual SSE parser instead of EventSource because the
 * latter can't attach an Authorization header — and the daemon only
 * accepts bearer-token auth.
 *
 * Returns a tear-down function the caller MUST invoke on unmount.
 */
export async function subscribeStatus(
  onStatus: (s: Status) => void,
  onError?: (err: unknown) => void,
): Promise<() => void> {
  const ep = await resolveEndpoint();
  const ctrl = new AbortController();
  (async () => {
    try {
      const res = await fetch(`${baseURL(ep)}/v1/events`, {
        headers: { authorization: `Bearer ${ep.token}` },
        signal: ctrl.signal,
      });
      if (!res.ok || !res.body) {
        throw new Error(`SSE: ${res.status}`);
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder("utf-8");
      let buf = "";
      while (true) {
        const { value, done } = await reader.read();
        if (done) return;
        buf += decoder.decode(value, { stream: true });
        // Each SSE event ends with a blank line.
        let sep: number;
        while ((sep = buf.indexOf("\n\n")) >= 0) {
          const block = buf.slice(0, sep);
          buf = buf.slice(sep + 2);
          dispatchSSE(block, onStatus);
        }
      }
    } catch (err) {
      if ((err as Error).name === "AbortError") return;
      if (onError) onError(err);
    }
  })();
  return () => ctrl.abort();
}

function dispatchSSE(block: string, onStatus: (s: Status) => void): void {
  let event = "message";
  const dataLines: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith(":")) continue; // comment / heartbeat
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) dataLines.push(line.slice(5).trim());
  }
  if (event !== "status" || dataLines.length === 0) return;
  try {
    const status = JSON.parse(dataLines.join("\n")) as Status;
    onStatus(status);
  } catch (err) {
    console.error("SSE parse failed", err);
  }
}
