/**
 * useStatus subscribes to the daemon's /v1/events SSE stream and
 * exposes the latest status snapshot to React. It is the single source
 * of truth for the toggle state, the header dot, the tray icon — i.e.
 * the Atlas single-status-surface principle in code form.
 */

import { useEffect, useState } from "react";
import { api, subscribeStatus } from "../api/client";
import type { Status } from "../api/types";

export type LoadState = "loading" | "ready" | "no-daemon";

export interface StatusHook {
  status: Status | null;
  load: LoadState;
  error: string | null;
}

export function useStatus(): StatusHook {
  const [status, setStatus] = useState<Status | null>(null);
  const [load, setLoad] = useState<LoadState>("loading");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let detach: (() => void) | undefined;
    let cancelled = false;

    (async () => {
      try {
        const initial = await api.status();
        if (cancelled) return;
        setStatus(initial);
        setLoad("ready");
      } catch (err) {
        if (cancelled) return;
        setLoad("no-daemon");
        setError((err as Error).message);
        return;
      }
      detach = await subscribeStatus(
        (s) => {
          if (!cancelled) setStatus(s);
        },
        (err) => {
          if (!cancelled) setError(String(err));
        },
      );
    })();

    return () => {
      cancelled = true;
      detach?.();
    };
  }, []);

  return { status, load, error };
}
