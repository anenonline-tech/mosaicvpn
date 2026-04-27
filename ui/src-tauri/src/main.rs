// Mosaic UI — Tauri 2 shell.
//
// The renderer (React + Vite) lives in ../src and talks to the daemon
// over its loopback HTTP API. The shell's only structural job is to
// resolve the daemon's listen address + bearer token from the lockfile
// the daemon writes at startup, and to surface a system-tray icon
// with show / hide / quit. Everything else runs in the webview.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::path::PathBuf;

use serde::{Deserialize, Serialize};
use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Manager,
};

#[derive(Serialize, Deserialize, Debug, Clone)]
struct DaemonEndpoint {
    host: String,
    port: u16,
    token: String,
    pid: u32,
    #[serde(default)]
    version: String,
    #[serde(default)]
    started: String,
}

/// data_dir mirrors internal/paths.DataDir on the Go side. We can't link
/// against the Go binary, so we re-derive the same per-OS layout here.
fn data_dir() -> Option<PathBuf> {
    #[cfg(target_os = "windows")]
    {
        if let Ok(d) = std::env::var("ProgramData") {
            return Some(PathBuf::from(d).join("Mosaic"));
        }
    }
    #[cfg(target_os = "macos")]
    {
        if let Some(home) = dirs::home_dir() {
            return Some(home.join("Library").join("Application Support").join("Mosaic"));
        }
    }
    #[cfg(target_os = "linux")]
    {
        if let Some(d) = dirs::data_dir() {
            return Some(d.join("mosaic"));
        }
    }
    None
}

#[tauri::command]
fn daemon_endpoint() -> Result<DaemonEndpoint, String> {
    let dir = data_dir().ok_or_else(|| "could not determine data dir".to_string())?;
    let lock = dir.join("daemon.lock");
    let raw = std::fs::read(&lock).map_err(|e| format!("read {}: {}", lock.display(), e))?;
    serde_json::from_slice::<DaemonEndpoint>(&raw)
        .map_err(|e| format!("decode lockfile: {}", e))
}

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![daemon_endpoint])
        .setup(|app| {
            // Tray menu: Show window · separator · Quit. Connection
            // toggles live in the popup itself; the tray menu is for
            // window/lifecycle controls only.
            let show = MenuItem::with_id(app, "show", "Show window", true, None::<&str>)?;
            let hide = MenuItem::with_id(app, "hide", "Hide window", true, None::<&str>)?;
            let sep = PredefinedMenuItem::separator(app)?;
            let quit = MenuItem::with_id(app, "quit", "Quit Mosaic", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show, &hide, &sep, &quit])?;

            let icon = app
                .default_window_icon()
                .cloned()
                .ok_or("no default window icon")?;

            let _tray = TrayIconBuilder::with_id("mosaic-tray")
                .tooltip("Mosaic VPN")
                .icon(icon)
                .menu(&menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "show" => focus_main(app),
                    "hide" => {
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.hide();
                        }
                    }
                    "quit" => {
                        app.exit(0);
                    }
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        focus_main(tray.app_handle());
                    }
                })
                .build(app)?;

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running mosaic-ui");
}

fn focus_main(app: &tauri::AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
    }
}
