// Mosaic UI — Tauri 2 shell.
//
// The renderer (React + Vite) lives in ../src and talks to the daemon
// over its loopback HTTP API. The shell's only structural job is to
// resolve the daemon's listen address + bearer token from the lockfile
// the daemon writes at startup, and surface that to the renderer via a
// Tauri command. Everything else runs in the webview.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::path::PathBuf;

use serde::{Deserialize, Serialize};

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
        .run(tauri::generate_context!())
        .expect("error while running mosaic-ui");
}
