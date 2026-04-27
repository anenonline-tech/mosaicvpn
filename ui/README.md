# Mosaic — Atlas UI

Tauri 2 + Vite + React + TypeScript renderer for the Mosaic daemon. Implements the five Atlas screens at `docs/mockups/`:

| # | Screen           | Status         |
| - | ---------------- | -------------- |
| 1 | Atlas (main)     | Phase 3A — done in this slice |
| 2 | Pool gazetteer   | Phase 3B — placeholder |
| 3 | Routing register | Phase 3C — placeholder |
| 4 | Folio (prefs)    | Phase 3D — placeholder |
| 5 | Tray composer    | Phase 3E — placeholder |

## Layout

```
ui/
  index.html        # Vite entry
  package.json
  vite.config.ts
  tsconfig.json
  src/
    main.tsx        # React root
    App.tsx         # screen router
    api/
      types.ts      # mirror of internal/proto/types.go
      client.ts     # HTTP + SSE client; auth via bearer from lockfile
    hooks/
      useStatus.ts  # SSE-backed status hook (single source of truth)
    components/
      StatusSquare.tsx
      Marginalia.tsx
    screens/
      Main.tsx
      Placeholder.tsx
    styles/
      atlas.css     # design tokens (cream / ink / copper / leaf)
      app.css       # screen-level layouts
  src-tauri/
    Cargo.toml
    build.rs
    tauri.conf.json
    src/main.rs     # exposes `daemon_endpoint` Tauri command (reads
                    # %ProgramData%/Mosaic/daemon.lock on Windows etc.)
```

## Daemon discovery

The renderer cannot read the lockfile directly (browser sandbox). Instead, the Rust shell exposes a `daemon_endpoint` Tauri command that returns `{ host, port, token, pid, version, started }` — it reads the same `daemon.lock` JSON the daemon writes on startup. The renderer caches the result and uses it as a `Bearer` token on every API call.

For SSE we use `fetch()` + a manual event parser instead of `EventSource`, because `EventSource` cannot attach an `Authorization` header.

## Running locally

```sh
cd ui
npm ci
npm run dev      # Vite dev server on http://localhost:1420
npm run tauri dev  # full Tauri shell (requires webkit2gtk on Linux)
```

## Build

`npm run build` produces `dist/` (Vite output). `npm run tauri build` produces a platform installer; that is wired into Phase 4 (Windows installer).

## Atlas tokens

Cream `#ece4d0`, ink `#1f1c18`, copper `#b85c2c`, leaf `#4a6e3a`. Fonts: Iowan Old Style / Cormorant / Inter / JetBrains Mono. The single status square (45° rotated) appears in the header and the tray icon — same component, same color logic, driven by `useStatus()`.
