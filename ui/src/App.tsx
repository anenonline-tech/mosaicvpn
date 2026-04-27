import { useState } from "react";
import { Marginalia } from "./components/Marginalia";
import { useStatus } from "./hooks/useStatus";
import { Main } from "./screens/Main";
import { Placeholder } from "./screens/Placeholder";
import { Pool } from "./screens/Pool";
import { Routing } from "./screens/Routing";

type Screen = "main" | "routing" | "pool" | "folio" | "tray";

const SCREENS: { id: Screen; label: string }[] = [
  { id: "main", label: "Atlas" },
  { id: "pool", label: "Pool" },
  { id: "routing", label: "Routing" },
  { id: "folio", label: "Folio" },
  { id: "tray", label: "Tray" },
];

export function App(): JSX.Element {
  const { status, load, error } = useStatus();
  const [screen, setScreen] = useState<Screen>("main");

  if (load === "loading") {
    return (
      <div className="app-shell">
        <div className="splash">
          <div className="badge">resolving…</div>
          <div className="hint">Reading the daemon lockfile</div>
        </div>
      </div>
    );
  }

  if (load === "no-daemon" || !status) {
    return (
      <div className="app-shell">
        <div className="splash">
          <div className="badge">daemon offline</div>
          <div className="hint">{error ?? "Start mosaicd and reload"}</div>
        </div>
      </div>
    );
  }

  return (
    <div className="app-shell">
      <Marginalia
        status={status}
        plate={plateFor(screen)}
        plateSubtitle={plateSubtitleFor(screen)}
      />
      <nav className="toc">
        {SCREENS.map((s) => (
          <button
            key={s.id}
            className={screen === s.id ? "active" : ""}
            onClick={() => setScreen(s.id)}
          >
            {s.label}
          </button>
        ))}
      </nav>

      {screen === "main" ? <Main status={status} /> : null}
      {screen === "pool" ? <Pool activeServerId={status.server?.id} /> : null}
      {screen === "routing" ? <Routing /> : null}
      {screen === "folio" ? (
        <Placeholder
          title="Folio of preferences"
          subtitle="Phase 3D · DNS · tunnel mode · kill-switch · MCP"
        />
      ) : null}
      {screen === "tray" ? (
        <Placeholder
          title="Tray composer"
          subtitle="Phase 3E · system tray icon & menu"
        />
      ) : null}
    </div>
  );
}

function plateFor(s: Screen): string {
  return { main: "IV", pool: "II", routing: "III", folio: "V", tray: "I" }[s];
}

function plateSubtitleFor(s: Screen): string {
  return {
    main: "The world, projected",
    pool: "Stations & subscriptions",
    routing: "Rules & priorities",
    folio: "Preferences",
    tray: "System tray",
  }[s];
}
