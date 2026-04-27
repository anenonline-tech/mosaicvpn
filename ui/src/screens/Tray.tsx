import { useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import type { Server, Status } from "../api/types";

/**
 * Tray — the compact popup that appears from the system tray.
 * Mirrors docs/mockups/tray.html. Shown as a screen in the main app
 * for now; the dedicated popup window + tray icon registration are
 * wired in src-tauri/src/main.rs.
 */
export function Tray({ status }: { status: Status }): JSX.Element {
  const [servers, setServers] = useState<Server[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .listServers()
      .then((s) => {
        if (!cancelled) setServers(s);
      })
      .catch((e: Error) => {
        if (!cancelled) setErr(e.message);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const recent = useMemo(() => {
    return [...servers]
      .filter((s) => (s.last_test_ms ?? 0) > 0)
      .sort((a, b) => (a.last_test_ms ?? 0) - (b.last_test_ms ?? 0))
      .slice(0, 4);
  }, [servers]);

  const onToggle = async () => {
    setBusy(true);
    setErr(null);
    try {
      if (status.state === "connected" || status.state === "connecting") {
        await api.disconnect();
      } else if (status.server) {
        await api.connect(status.server.id);
      } else if (recent[0]) {
        await api.connect(recent[0].id);
      } else {
        setErr("no servers configured");
      }
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const onPickStation = async (id: string) => {
    setBusy(true);
    setErr(null);
    try {
      await api.connect(id);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const isOn =
    status.state === "connected" || status.state === "connecting";
  const eyebrowLabel = labelFor(status.state);
  const eyebrowSince = status.since ? sinceFmt(status.since) : "—";

  return (
    <div className="tray-frame">
      <div className="popup">
        <div className="tray-mast">
          <div className="tray-mast-name">
            Mosaic <i>·</i> tray
          </div>
          <div className="tray-mast-folio">Folio · plate i</div>
        </div>

        <div className="conn">
          <div className={`tray-eyebrow ${eyebrowToneFor(status.state)}`}>
            <span className={`diamond ${isOn ? "on" : ""}`}></span>
            {eyebrowLabel}
            {isOn ? (
              <span className="tray-since">· {eyebrowSince}</span>
            ) : null}
          </div>
          <div className="tray-city">
            {status.server ? (
              <>
                {status.server.city || status.server.name}
                {status.server.tag ? <em> — {status.server.tag}</em> : null}
              </>
            ) : (
              <span className="ink-mute italic">no station</span>
            )}
          </div>

          <div className="tray-meta">
            <div className="tray-pair">
              <b>{status.latency_ms ?? "—"}</b>
              <small>Latency · ms</small>
            </div>
            <div className="tray-pair">
              <b>{fmtRate(status.bytes_in)}</b>
              <small>Down</small>
            </div>
            <div className="tray-pair">
              <b>{fmtRate(status.bytes_out)}</b>
              <small>Up</small>
            </div>
          </div>
        </div>

        <button
          type="button"
          className={`big-toggle ${isOn ? "" : "off"}`}
          onClick={onToggle}
          disabled={busy}
          title={isOn ? "Click to disconnect" : "Click to connect"}
        >
          <span className="label">
            <span className="t">Tunnel · {isOn ? "ON" : "OFF"}</span>
            <span className="v">
              {isOn ? (
                <>
                  Kill-switch{" "}
                  {status.kill_switch ? "armed" : "disarmed"}
                  {" · "}
                  {status.tunnel_mode || "tun"} mode
                </>
              ) : status.last_error ? (
                status.last_error
              ) : (
                "Idle — pick a station to engage"
              )}
            </span>
          </span>
          <span className="ctl">
            {busy ? "…" : isOn ? "Stop" : "Start"}
          </span>
        </button>

        <div className="tray-quick-head">
          <div className="l">Recent stations</div>
          <div className="r">
            i—{toRomanLower(Math.min(recent.length, 4))} of{" "}
            {toRomanLower(servers.length)}
          </div>
        </div>

        {recent.length === 0 ? (
          <div className="tray-empty">
            <em>«</em> No tested stations yet. Run <b>mosaic test</b> from
            the CLI. <em>»</em>
          </div>
        ) : (
          recent.map((s, i) => (
            <div
              key={s.id}
              className={`qrow ${
                status.server?.id === s.id ? "cur" : ""
              }`}
              onClick={() => onPickStation(s.id)}
            >
              <span className="n">{toRomanLower(i + 1)}</span>
              <div>
                <div className="qname">
                  {s.city || s.name}
                  {s.tag ? <i> — {s.tag}</i> : null}
                </div>
                <div className="pr">{s.protocol.toUpperCase()}</div>
              </div>
              <span className="qms">
                {s.last_test_ms && s.last_test_ms > 0
                  ? `${s.last_test_ms} ms`
                  : "—"}
              </span>
            </div>
          ))
        )}

        <div className="pop-foot">
          <span className="ink-mute italic">
            v{status.daemon_version || "—"} · pid {status.daemon_pid || "—"}
          </span>
        </div>

        {err ? <div className="pool-error">{err}</div> : null}
      </div>
    </div>
  );
}

/* ---------- helpers ---------- */

function labelFor(state: Status["state"]): string {
  switch (state) {
    case "connected":
      return "CONNECTED";
    case "connecting":
      return "CONNECTING";
    case "error":
      return "ERROR";
    default:
      return "STANDBY";
  }
}

function eyebrowToneFor(state: Status["state"]): string {
  switch (state) {
    case "connected":
      return "leaf";
    case "connecting":
      return "warn";
    case "error":
      return "err";
    default:
      return "mute";
  }
}

function sinceFmt(iso: string): string {
  const d = new Date(iso);
  const elapsed = Math.max(0, (Date.now() - d.getTime()) / 1000);
  const h = Math.floor(elapsed / 3600);
  const m = Math.floor((elapsed % 3600) / 60);
  const s = Math.floor(elapsed % 60);
  if (h > 0) {
    return `${h.toString().padStart(2, "0")}:${m
      .toString()
      .padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  }
  return `${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
}

function fmtRate(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024)
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function toRomanLower(n: number): string {
  if (n <= 0) return "0";
  const map: [number, string][] = [
    [100, "c"],
    [90, "xc"],
    [50, "l"],
    [40, "xl"],
    [10, "x"],
    [9, "ix"],
    [5, "v"],
    [4, "iv"],
    [1, "i"],
  ];
  let out = "";
  let rem = n;
  for (const [v, s] of map) {
    while (rem >= v) {
      out += s;
      rem -= v;
    }
  }
  return out;
}
