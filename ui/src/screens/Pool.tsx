import { useEffect, useMemo, useState, type FormEvent } from "react";
import { api } from "../api/client";
import type { Server, Subscription } from "../api/types";

/**
 * Pool — the gazetteer of subscriptions. Mirrors docs/mockups/subs.html.
 *
 * Lists every subscription as a numbered card with stats derived from
 * its servers (live / median / best). Footer adds a new subscription;
 * each card can refresh / delete itself or expand to show its stations.
 */
export function Pool({
  activeServerId,
}: {
  activeServerId?: string;
}): JSX.Element {
  const [subs, setSubs] = useState<Subscription[]>([]);
  const [servers, setServers] = useState<Server[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [url, setUrl] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const reload = async () => {
    try {
      const [s, srv] = await Promise.all([
        api.listSubscriptions(),
        api.listServers(),
      ]);
      setSubs(s);
      setServers(srv);
    } catch (e) {
      setErr((e as Error).message);
    }
  };

  useEffect(() => {
    void reload();
  }, []);

  const onAdd = async (e: FormEvent) => {
    e.preventDefault();
    if (!url.trim()) return;
    setBusy("add");
    setErr(null);
    try {
      await api.addSubscription(url.trim());
      setUrl("");
      await reload();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const onRefresh = async (id: string) => {
    setBusy(`refresh:${id}`);
    setErr(null);
    try {
      await api.refreshSubscription(id);
      await reload();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const onDelete = async (id: string, name: string) => {
    if (!confirm(`Delete subscription "${name}" and all its stations?`)) return;
    setBusy(`del:${id}`);
    setErr(null);
    try {
      await api.deleteSubscription(id);
      await reload();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const totals = useMemo(() => {
    const live = servers.filter(
      (s) => (s.last_test_ms ?? 0) > 0 && !s.last_test_error,
    );
    const med = median(live.map((s) => s.last_test_ms!));
    return { stations: servers.length, median: med };
  }, [servers]);

  return (
    <div className="pool-frame">
      <header className="pool-mast">
        <div>
          <div className="pool-name">
            Pool <i>—</i> gazetteer
          </div>
          <div className="pool-mast-sub">
            {subs.length}{" "}
            {subs.length === 1 ? "subscription" : "subscriptions"}
            {" · "}
            {totals.stations}{" "}
            {totals.stations === 1 ? "station" : "stations"}
          </div>
        </div>
        <div className="pool-mast-right">
          <span className="serif italic">
            Median latency across pools:{" "}
            <em className="copper">{fmtMs(totals.median)}</em>
          </span>
        </div>
      </header>

      {err ? <div className="pool-error">{err}</div> : null}

      <section className="gazetteer">
        {subs.map((sub, i) => (
          <PoolCard
            key={sub.id}
            num={toRomanLower(i + 1)}
            sub={sub}
            servers={servers.filter((s) => s.subscription_id === sub.id)}
            activeServerId={activeServerId}
            expanded={!!expanded[sub.id]}
            onToggleExpand={() =>
              setExpanded((prev) => ({ ...prev, [sub.id]: !prev[sub.id] }))
            }
            onRefresh={() => onRefresh(sub.id)}
            onDelete={() => onDelete(sub.id, sub.name)}
            refreshing={busy === `refresh:${sub.id}`}
            deleting={busy === `del:${sub.id}`}
          />
        ))}
        {subs.length === 0 ? (
          <article className="pool empty">
            <div className="num">i</div>
            <div>
              <div className="pool-card-name italic-mute">
                — no subscriptions yet —
              </div>
              <div className="pool-verse">
                <em>«</em> Paste a subscription URL below. Mosaic auto-detects
                format (sing-box · clash · v2ray base64 · sip008) on first
                fetch. <em>»</em>
              </div>
            </div>
          </article>
        ) : null}
      </section>

      <form className="add-row" onSubmit={onAdd}>
        <span className="add-lab">
          <i>+</i> Add subscription
        </span>
        <span className="add-input">
          <span className="add-pre">URL</span>
          <input
            type="text"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://… or vless://, ss://, hysteria2://"
            disabled={busy === "add"}
          />
          <span className="add-formats">
            sing-box · clash · v2ray base64 · sip008
          </span>
        </span>
        <button
          type="submit"
          className="btn primary"
          disabled={busy === "add" || !url.trim()}
        >
          {busy === "add" ? "Fetching…" : "Fetch"}
        </button>
      </form>
    </div>
  );
}

function PoolCard({
  num,
  sub,
  servers,
  activeServerId,
  expanded,
  onToggleExpand,
  onRefresh,
  onDelete,
  refreshing,
  deleting,
}: {
  num: string;
  sub: Subscription;
  servers: Server[];
  activeServerId?: string;
  expanded: boolean;
  onToggleExpand: () => void;
  onRefresh: () => void;
  onDelete: () => void;
  refreshing: boolean;
  deleting: boolean;
}): JSX.Element {
  const isCur = servers.some((s) => s.id === activeServerId);
  const live = servers.filter(
    (s) => (s.last_test_ms ?? 0) > 0 && !s.last_test_error,
  );
  const dead = servers.filter((s) => (s.last_test_ms ?? 0) < 0).length;
  const med = median(live.map((s) => s.last_test_ms!));
  const best =
    live.length === 0
      ? null
      : live.reduce(
          (a, b) =>
            (a.last_test_ms ?? Infinity) < (b.last_test_ms ?? Infinity) ? a : b,
        );
  const protoMix = computeProtoMix(servers);

  const status = subStatus(sub);

  return (
    <article className={`pool ${isCur ? "cur" : ""}`}>
      <div className="num">{num}</div>
      <div>
        <div className="pool-head">
          <div>
            <div className="pool-card-name">
              {sub.name || hostFrom(sub.url)}
              {isCur ? <em> — in service</em> : null}
            </div>
            <div className="pool-src">
              ⌖ <b>{redactedURL(sub.url)}</b> · {fmtFormat(sub.format)}
            </div>
          </div>
          <span className={`pool-badge ${status.tone}`}>{status.text}</span>
        </div>

        <div className="pool-stats">
          <Cell lab="Stations" val={String(servers.length)} />
          <Cell
            lab="Live"
            val={String(live.length)}
            small={dead > 0 ? `· ${dead} dead` : "· all up"}
          />
          <Cell
            lab="Median"
            val={med === null ? "—" : String(med)}
            small={med === null ? "" : "ms"}
          />
          <Cell
            lab="Best"
            val={best ? String(best.last_test_ms ?? "—") : "—"}
            small={best ? `ms · ${best.city || best.name}` : ""}
            emphasized={!!best}
          />
        </div>

        {protoMix.bar.length > 0 ? (
          <div className="pool-protos">
            PROTOCOLS
            <div className="pool-bar">
              {protoMix.bar.map((seg) => (
                <span
                  key={seg.proto}
                  className={`seg ${seg.proto}`}
                  style={{ width: `${seg.pct.toFixed(1)}%` }}
                />
              ))}
            </div>
            <span className="pool-mix">{protoMix.label}</span>
          </div>
        ) : null}

        <div className="pool-actions">
          <button
            className="btn ghost"
            onClick={onRefresh}
            disabled={refreshing || deleting}
          >
            {refreshing ? "Refreshing…" : "Refresh now"}
          </button>
          <button
            className="btn ghost"
            onClick={onToggleExpand}
            disabled={refreshing || deleting}
          >
            {expanded ? "Hide stations" : "Browse stations"}
          </button>
          <button
            className="btn ghost danger"
            onClick={onDelete}
            disabled={refreshing || deleting}
          >
            {deleting ? "Removing…" : "Delete"}
          </button>
          <span className="pool-foot italic">
            {fmtRefreshHint(sub)}
          </span>
        </div>

        {expanded && servers.length > 0 ? (
          <div className="pool-stations">
            {servers.map((s) => (
              <div
                key={s.id}
                className={`station ${
                  s.id === activeServerId ? "cur" : ""
                }`}
              >
                <span className="city">{s.city || s.name}</span>
                <span className="addr mono">
                  {s.address}:{s.port}
                </span>
                <span className="proto mono">{s.protocol}</span>
                <span className="ms mono">
                  {(s.last_test_ms ?? 0) > 0
                    ? `${s.last_test_ms}ms`
                    : (s.last_test_ms ?? 0) < 0
                      ? "fail"
                      : "—"}
                </span>
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </article>
  );
}

function Cell({
  lab,
  val,
  small,
  emphasized,
}: {
  lab: string;
  val: string;
  small?: string;
  emphasized?: boolean;
}): JSX.Element {
  return (
    <div className="pool-cell">
      <div className="lab">{lab}</div>
      <div className="v">
        {emphasized ? <em>{val}</em> : val}
        {small ? <small>{small}</small> : null}
      </div>
    </div>
  );
}

/* ---------- helpers ---------- */

function median(nums: number[]): number | null {
  if (nums.length === 0) return null;
  const sorted = [...nums].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 0
    ? Math.round((sorted[mid - 1] + sorted[mid]) / 2)
    : sorted[mid];
}

function fmtMs(ms: number | null): string {
  return ms === null ? "—" : `${ms} ms`;
}

function hostFrom(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

function redactedURL(url: string): string {
  try {
    const u = new URL(url);
    if (u.searchParams.has("token")) {
      const tok = u.searchParams.get("token") ?? "";
      const tail = tok.length > 4 ? tok.slice(-4) : tok;
      u.searchParams.set("token", `••••${tail}`);
    }
    return u.toString();
  } catch {
    return url;
  }
}

function fmtFormat(f: string): string {
  switch (f) {
    case "singbox":
      return "sing-box format";
    case "clash":
      return "clash YAML format";
    case "v2ray-base64":
      return "v2ray base64 format";
    case "sip008":
      return "sip008 format";
    default:
      return "auto-detect";
  }
}

function fmtRefreshHint(s: Subscription): string {
  if (!s.auto_refresh || !s.refresh_interval_seconds) return "manual refresh";
  const h = Math.round(s.refresh_interval_seconds / 3600);
  if (h <= 1) return "refreshes hourly";
  return `refreshes every ${h} h`;
}

function subStatus(s: Subscription): { text: string; tone: string } {
  if (s.last_error) return { text: `× ${s.last_error}`, tone: "err" };
  if (!s.last_fetched) return { text: "⏳ never fetched", tone: "warn" };
  const age = Date.now() - new Date(s.last_fetched).getTime();
  return { text: `◆ ${fmtAge(age)} ago`, tone: "ok" };
}

function fmtAge(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s} s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} m`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h`;
  const d = Math.round(h / 24);
  return `${d} d`;
}

function computeProtoMix(servers: Server[]): {
  bar: { proto: string; pct: number }[];
  label: string;
} {
  if (servers.length === 0) return { bar: [], label: "" };
  const counts = new Map<string, number>();
  for (const s of servers) {
    counts.set(s.protocol, (counts.get(s.protocol) ?? 0) + 1);
  }
  const total = servers.length;
  const bar = [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([proto, n]) => ({ proto: protoClass(proto), pct: (n / total) * 100 }));
  const label = [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([p, n]) => `${protoLabel(p)} ${n}`)
    .join(" · ");
  return { bar, label };
}

function protoClass(p: string): string {
  switch (p) {
    case "hysteria2":
      return "hy";
    case "vless":
      return "vl";
    case "vmess":
      return "vl";
    case "naive":
      return "nv";
    case "shadowsocks":
      return "ss";
    case "amneziawg":
      return "wg";
    case "trojan":
      return "tj";
    default:
      return "ot";
  }
}

function protoLabel(p: string): string {
  switch (p) {
    case "hysteria2":
      return "HY²";
    case "vless":
      return "VLESS";
    case "vmess":
      return "VMess";
    case "naive":
      return "NAIVE";
    case "shadowsocks":
      return "SS";
    case "amneziawg":
      return "AWG";
    case "trojan":
      return "TROJ";
    default:
      return p.toUpperCase();
  }
}

function toRomanLower(n: number): string {
  const map: [number, string][] = [
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
