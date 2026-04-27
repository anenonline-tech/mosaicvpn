import { useEffect, useMemo, useState, type ReactNode } from "react";
import { api } from "../api/client";
import type { Action, Logic, Match, Rule, Server } from "../api/types";

/**
 * Routing — the register of routing rules. Mirrors docs/mockups/power.html.
 *
 * Two columns: left = ordered list of rules (priority i, ii, iii…), right
 * = inspector / editor for the selected rule. Selecting an existing rule
 * shows a read-only summary with Delete + reorder; "+ New rule" opens an
 * empty editor that posts via api.addRule. Reorder is via up/down arrows
 * which call /v1/rules:reorder with the new id ordering.
 *
 * Editing existing rules in place is not yet supported by the daemon API
 * (no PUT /v1/rules/{id}), so changing an existing rule is delete + add.
 */

type EditorState =
  | { mode: "view"; ruleId: string | null }
  | { mode: "create"; draft: DraftRule };

interface DraftRule {
  name: string;
  action: Action;
  target: string;
  enabled: boolean;
  logic: Logic;
  geosite: string[];
  geoip: string[];
  domain_suffix: string[];
  domain_keyword: string[];
  domain: string[];
  ip_cidr: string[];
  process: string[];
  port: string[];
}

const FRESH_DRAFT: DraftRule = {
  name: "",
  action: "proxy",
  target: "",
  enabled: true,
  logic: "or",
  geosite: [],
  geoip: [],
  domain_suffix: [],
  domain_keyword: [],
  domain: [],
  ip_cidr: [],
  process: [],
  port: [],
};

export function Routing(): JSX.Element {
  const [rules, setRules] = useState<Rule[]>([]);
  const [servers, setServers] = useState<Server[]>([]);
  const [editor, setEditor] = useState<EditorState>({
    mode: "view",
    ruleId: null,
  });
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const reload = async () => {
    try {
      const [r, s] = await Promise.all([api.listRules(), api.listServers()]);
      r.sort((a, b) => a.priority - b.priority);
      setRules(r);
      setServers(s);
    } catch (e) {
      setErr((e as Error).message);
    }
  };

  useEffect(() => {
    void reload();
  }, []);

  const selected =
    editor.mode === "view" && editor.ruleId
      ? (rules.find((r) => r.id === editor.ruleId) ?? null)
      : null;

  const onCreate = () => {
    setEditor({ mode: "create", draft: { ...FRESH_DRAFT } });
    setErr(null);
  };
  const onCancelCreate = () => setEditor({ mode: "view", ruleId: null });
  const onPickRule = (id: string) =>
    setEditor({ mode: "view", ruleId: id });

  const onDelete = async (id: string) => {
    const r = rules.find((x) => x.id === id);
    if (!r) return;
    if (!confirm(`Delete rule "${r.name}"?`)) return;
    setBusy(`del:${id}`);
    setErr(null);
    try {
      await api.deleteRule(id);
      if (editor.mode === "view" && editor.ruleId === id) {
        setEditor({ mode: "view", ruleId: null });
      }
      await reload();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const onMove = async (id: string, dir: -1 | 1) => {
    const idx = rules.findIndex((r) => r.id === id);
    if (idx < 0) return;
    const next = idx + dir;
    if (next < 0 || next >= rules.length) return;
    const ids = rules.map((r) => r.id);
    [ids[idx], ids[next]] = [ids[next], ids[idx]];
    setBusy(`reorder:${id}`);
    setErr(null);
    try {
      await api.reorderRules(ids);
      await reload();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const onSubmit = async () => {
    if (editor.mode !== "create") return;
    const d = editor.draft;
    if (!d.name.trim()) {
      setErr("name is required");
      return;
    }
    setBusy("create");
    setErr(null);
    try {
      const match: Match = { logic: d.logic };
      if (d.geosite.length) match.geosite = d.geosite;
      if (d.geoip.length) match.geoip = d.geoip;
      if (d.domain_suffix.length) match.domain_suffix = d.domain_suffix;
      if (d.domain_keyword.length) match.domain_keyword = d.domain_keyword;
      if (d.domain.length) match.domain = d.domain;
      if (d.ip_cidr.length) match.ip_cidr = d.ip_cidr;
      if (d.process.length) match.process = d.process;
      if (d.port.length) match.port = d.port;
      await api.addRule({
        name: d.name.trim(),
        action: d.action,
        target: d.action === "proxy" ? d.target || undefined : undefined,
        enabled: d.enabled,
        priority: rules.length, // appended at end
        match,
      });
      setEditor({ mode: "view", ruleId: null });
      await reload();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="routing-frame">
      <header className="pool-mast">
        <div>
          <div className="pool-name">
            Routing <i>—</i> register
          </div>
          <div className="pool-mast-sub">
            {rules.length} {rules.length === 1 ? "rule" : "rules"}
            {" · "}applied in priority order
          </div>
        </div>
        <div className="routing-mast-actions">
          <button
            className="btn ghost"
            onClick={onCreate}
            disabled={editor.mode === "create"}
          >
            + New rule
          </button>
        </div>
      </header>

      {err ? <div className="pool-error">{err}</div> : null}

      <section className="routing-main">
        <div className="routing-left">
          <div className="routing-head">
            <span></span>
            <span>Rule · Conditions</span>
            <span>Action</span>
            <span>Target</span>
            <span></span>
          </div>
          {rules.length === 0 ? (
            <div className="routing-empty">
              <em>«</em> No rules yet. Click <b>+ New rule</b> to begin.{" "}
              <em>»</em>
            </div>
          ) : (
            rules.map((r, i) => (
              <RuleRow
                key={r.id}
                num={toRomanLower(i + 1)}
                rule={r}
                isSelected={editor.mode === "view" && editor.ruleId === r.id}
                isFirst={i === 0}
                isLast={i === rules.length - 1}
                isBusy={busy === `reorder:${r.id}` || busy === `del:${r.id}`}
                onPick={() => onPickRule(r.id)}
                onMoveUp={() => onMove(r.id, -1)}
                onMoveDown={() => onMove(r.id, 1)}
                onDelete={() => onDelete(r.id)}
              />
            ))
          )}
        </div>

        <div className="routing-right">
          {editor.mode === "create" ? (
            <Editor
              draft={editor.draft}
              setDraft={(d) => setEditor({ mode: "create", draft: d })}
              servers={servers}
              busy={busy === "create"}
              onSubmit={onSubmit}
              onCancel={onCancelCreate}
            />
          ) : selected ? (
            <Inspector rule={selected} servers={servers} />
          ) : (
            <div className="routing-empty">
              <em>«</em> Select a rule to inspect, or click{" "}
              <b>+ New rule</b>. <em>»</em>
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

/* ---------- left: list row ---------- */

function RuleRow({
  num,
  rule,
  isSelected,
  isFirst,
  isLast,
  isBusy,
  onPick,
  onMoveUp,
  onMoveDown,
  onDelete,
}: {
  num: string;
  rule: Rule;
  isSelected: boolean;
  isFirst: boolean;
  isLast: boolean;
  isBusy: boolean;
  onPick: () => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
  onDelete: () => void;
}): JSX.Element {
  const conds = useMemo(() => summarizeMatch(rule.match), [rule.match]);
  return (
    <div
      className={`routing-entry ${isSelected ? "cur" : ""} ${
        rule.enabled ? "" : "disabled"
      }`}
      onClick={onPick}
    >
      <span className="num">{num}</span>
      <div className="body">
        <div className="rule-name">{rule.name || "—"}</div>
        <div className="rule-conds">{conds || "match-all"}</div>
      </div>
      <span className={`action ${rule.action}`}>
        {rule.action.toUpperCase()}
      </span>
      <span className="target">{rule.target || "—"}</span>
      <span className="row-actions" onClick={(e) => e.stopPropagation()}>
        <button
          className="icon-btn"
          disabled={isFirst || isBusy}
          onClick={onMoveUp}
          title="Move up"
        >
          ↑
        </button>
        <button
          className="icon-btn"
          disabled={isLast || isBusy}
          onClick={onMoveDown}
          title="Move down"
        >
          ↓
        </button>
        <button
          className="icon-btn danger"
          disabled={isBusy}
          onClick={onDelete}
          title="Delete"
        >
          ×
        </button>
      </span>
    </div>
  );
}

/* ---------- right: read-only inspector ---------- */

function Inspector({
  rule,
  servers,
}: {
  rule: Rule;
  servers: Server[];
}): JSX.Element {
  const target = servers.find((s) => s.id === rule.target);
  return (
    <div className="inspector">
      <div className="editor-mast">
        <span className="title">{rule.name}</span>
        <span className={`action-pill ${rule.action}`}>
          {rule.action.toUpperCase()}
        </span>
      </div>

      <Row lab="Logic">
        <code className="mono">{rule.match.logic.toUpperCase()}</code>
      </Row>

      {rule.action === "proxy" ? (
        <Row lab="Target">
          {target ? (
            <span>
              {target.name}{" "}
              <span className="ink-mute">
                ({target.protocol} · {target.address}:{target.port})
              </span>
            </span>
          ) : (
            <span className="ink-mute italic">
              {rule.target ? `${rule.target} (not found)` : "auto"}
            </span>
          )}
        </Row>
      ) : null}

      <Row lab="Priority">
        <code className="mono">#{rule.priority}</code>
      </Row>
      <Row lab="Status">
        {rule.enabled ? (
          <span className="leaf">enabled</span>
        ) : (
          <span className="ink-mute">disabled</span>
        )}
      </Row>

      <div className="match-grid">
        <MatchView label="GeoSite" values={rule.match.geosite} />
        <MatchView label="GeoIP" values={rule.match.geoip} />
        <MatchView label="Domain (suffix)" values={rule.match.domain_suffix} />
        <MatchView label="Domain (keyword)" values={rule.match.domain_keyword} />
        <MatchView label="Domain (exact)" values={rule.match.domain} />
        <MatchView label="IP CIDR" values={rule.match.ip_cidr} />
        <MatchView label="Process" values={rule.match.process} />
        <MatchView label="Port" values={rule.match.port} />
      </div>
    </div>
  );
}

function MatchView({
  label,
  values,
}: {
  label: string;
  values?: string[];
}): JSX.Element | null {
  if (!values || values.length === 0) return null;
  return (
    <div className="match-block">
      <div className="lab">{label}</div>
      <div className="chips">
        {values.map((v) => (
          <span key={v} className="chip">
            <b>{v}</b>
          </span>
        ))}
      </div>
    </div>
  );
}

/* ---------- right: editor (create new) ---------- */

function Editor({
  draft,
  setDraft,
  servers,
  busy,
  onSubmit,
  onCancel,
}: {
  draft: DraftRule;
  setDraft: (d: DraftRule) => void;
  servers: Server[];
  busy: boolean;
  onSubmit: () => void;
  onCancel: () => void;
}): JSX.Element {
  const update = <K extends keyof DraftRule>(k: K, v: DraftRule[K]) =>
    setDraft({ ...draft, [k]: v });

  const addChip = (k: ChipField, v: string) => {
    const t = v.trim();
    if (!t) return;
    const arr = draft[k];
    if (arr.includes(t)) return;
    update(k, [...arr, t] as DraftRule[ChipField]);
  };
  const removeChip = (k: ChipField, v: string) =>
    update(
      k,
      draft[k].filter((x) => x !== v) as DraftRule[ChipField],
    );

  return (
    <form
      className="editor"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      <div className="editor-mast">
        <input
          className="editor-title"
          value={draft.name}
          onChange={(e) => update("name", e.target.value)}
          placeholder="Rule name (e.g. Streaming → Singapore)"
          autoFocus
        />
        <span className="editor-actions">
          <button
            type="button"
            className="btn ghost"
            onClick={onCancel}
            disabled={busy}
          >
            Cancel
          </button>
          <button
            type="submit"
            className="btn primary"
            disabled={busy || !draft.name.trim()}
          >
            {busy ? "Saving…" : "Save rule"}
          </button>
        </span>
      </div>

      <Row lab="Action">
        <span className="opts">
          {(["proxy", "direct", "block"] as Action[]).map((a) => (
            <button
              key={a}
              type="button"
              className={draft.action === a ? "on" : ""}
              onClick={() => update("action", a)}
            >
              {a.toUpperCase()}
            </button>
          ))}
        </span>
      </Row>

      {draft.action === "proxy" ? (
        <Row lab="Target">
          <select
            className="select"
            value={draft.target}
            onChange={(e) => update("target", e.target.value)}
          >
            <option value="">— auto / use current —</option>
            {servers.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.protocol})
              </option>
            ))}
          </select>
        </Row>
      ) : null}

      <Row lab="Match logic">
        <span className="opts">
          {(["and", "or"] as Logic[]).map((l) => (
            <button
              key={l}
              type="button"
              className={draft.logic === l ? "on" : ""}
              onClick={() => update("logic", l)}
            >
              {l.toUpperCase()}
            </button>
          ))}
        </span>
      </Row>

      <ChipRow
        lab="GeoSite"
        placeholder="netflix"
        values={draft.geosite}
        onAdd={(v) => addChip("geosite", v)}
        onRemove={(v) => removeChip("geosite", v)}
      />
      <ChipRow
        lab="GeoIP"
        placeholder="cn"
        values={draft.geoip}
        onAdd={(v) => addChip("geoip", v)}
        onRemove={(v) => removeChip("geoip", v)}
      />
      <ChipRow
        lab="Domain (suffix)"
        placeholder="github.com"
        values={draft.domain_suffix}
        onAdd={(v) => addChip("domain_suffix", v)}
        onRemove={(v) => removeChip("domain_suffix", v)}
      />
      <ChipRow
        lab="Domain (keyword)"
        placeholder="doubleclick"
        values={draft.domain_keyword}
        onAdd={(v) => addChip("domain_keyword", v)}
        onRemove={(v) => removeChip("domain_keyword", v)}
      />
      <ChipRow
        lab="Domain (exact)"
        placeholder="example.com"
        values={draft.domain}
        onAdd={(v) => addChip("domain", v)}
        onRemove={(v) => removeChip("domain", v)}
      />
      <ChipRow
        lab="IP CIDR"
        placeholder="192.168.0.0/16"
        values={draft.ip_cidr}
        onAdd={(v) => addChip("ip_cidr", v)}
        onRemove={(v) => removeChip("ip_cidr", v)}
      />
      <ChipRow
        lab="Process"
        placeholder="chrome.exe"
        values={draft.process}
        onAdd={(v) => addChip("process", v)}
        onRemove={(v) => removeChip("process", v)}
      />
      <ChipRow
        lab="Port"
        placeholder="443 or 27015-27050"
        values={draft.port}
        onAdd={(v) => addChip("port", v)}
        onRemove={(v) => removeChip("port", v)}
      />
    </form>
  );
}

type ChipField =
  | "geosite"
  | "geoip"
  | "domain_suffix"
  | "domain_keyword"
  | "domain"
  | "ip_cidr"
  | "process"
  | "port";

function Row({
  lab,
  children,
}: {
  lab: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="ed-row">
      <div className="lab">{lab}</div>
      <div className="v">{children}</div>
    </div>
  );
}

function ChipRow({
  lab,
  placeholder,
  values,
  onAdd,
  onRemove,
}: {
  lab: string;
  placeholder: string;
  values: string[];
  onAdd: (v: string) => void;
  onRemove: (v: string) => void;
}): JSX.Element {
  const [input, setInput] = useState("");
  const submit = () => {
    if (!input.trim()) return;
    onAdd(input);
    setInput("");
  };
  return (
    <div className="ed-row">
      <div className="lab">{lab}</div>
      <div className="v">
        <div className="chips">
          {values.map((v) => (
            <span key={v} className="chip">
              <b>{v}</b>
              <button
                type="button"
                className="chip-x"
                onClick={() => onRemove(v)}
              >
                ×
              </button>
            </span>
          ))}
          <input
            className="chip-input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={placeholder}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === ",") {
                e.preventDefault();
                submit();
              }
            }}
            onBlur={submit}
          />
        </div>
      </div>
    </div>
  );
}

/* ---------- helpers ---------- */

function summarizeMatch(m: Match): string {
  const parts: string[] = [];
  const push = (lab: string, vs?: string[]) => {
    if (!vs || vs.length === 0) return;
    parts.push(`${lab} ${vs.slice(0, 3).join(", ")}${vs.length > 3 ? "…" : ""}`);
  };
  push("geosite", m.geosite);
  push("geoip", m.geoip);
  push("domain-suffix", m.domain_suffix);
  push("domain-keyword", m.domain_keyword);
  push("domain", m.domain);
  push("ip-cidr", m.ip_cidr);
  push("process", m.process);
  push("port", m.port);
  return parts.join(" · ");
}

function toRomanLower(n: number): string {
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
