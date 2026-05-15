#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod rpc;

use rpc::{RpcState, rpc_kill, rpc_send, rpc_spawn};
use serde::Serialize;
use std::path::Path;

/// #892: bundled libwayland in AppImage can ABI-mismatch the host Wayland
/// compositor → WebKitWebProcess `abort()`s on EGL display creation. Redirect
/// the child to the host's libwayland via LD_PRELOAD before WebKit forks.
#[cfg(target_os = "linux")]
fn linux_webkit_compat() {
    fn set_default(key: &str, value: &str) {
        if std::env::var_os(key).is_none() {
            std::env::set_var(key, value);
        }
    }

    // Always-on: DMABUF renderer breaks on a wider set of Mesa stacks than
    // libwayland bundling does. Cheap to disable, slow path is still fine.
    set_default("WEBKIT_DISABLE_DMABUF_RENDERER", "1");

    let in_appimage = std::env::var_os("APPDIR").is_some();
    let on_wayland = std::env::var_os("WAYLAND_DISPLAY").is_some();
    if !(in_appimage && on_wayland) {
        return;
    }

    // Disable accelerated compositing as well — same EGL init path.
    set_default("WEBKIT_DISABLE_COMPOSITING_MODE", "1");

    // Skip /usr/lib/libwayland-client.so.0 — on 64-bit Fedora that path can
    // resolve to a 32-bit library and the loader prints a wrong-ELF-class
    // warning instead of preloading.
    const CANDIDATES: &[&str] = &[
        "/usr/lib64/libwayland-client.so.0",
        "/usr/lib/x86_64-linux-gnu/libwayland-client.so.0",
        "/lib/x86_64-linux-gnu/libwayland-client.so.0",
    ];
    let Some(lib) = CANDIDATES.iter().find(|p| Path::new(p).exists()) else {
        return;
    };
    let existing = std::env::var("LD_PRELOAD").unwrap_or_default();
    let merged = if existing.is_empty() {
        (*lib).to_string()
    } else {
        format!("{lib}:{existing}")
    };
    std::env::set_var("LD_PRELOAD", merged);
}

#[derive(Serialize)]
struct FileEntry {
    path: String,
    depth: u32,
    kind: &'static str,
    name: String,
}

const SKIP_DIRS: &[&str] = &["node_modules", "target", "dist", "build", "out"];
const MAX_ENTRIES: usize = 800;

fn walk_dir(dir: &Path, depth: u32, max_depth: u32, out: &mut Vec<FileEntry>) {
    if depth > max_depth || out.len() >= MAX_ENTRIES {
        return;
    }
    let entries = match std::fs::read_dir(dir) {
        Ok(e) => e,
        Err(_) => return,
    };
    let mut items: Vec<_> = entries.flatten().collect();
    items.sort_by_key(|e| {
        let is_dir = e.file_type().map(|t| t.is_dir()).unwrap_or(false);
        (!is_dir, e.file_name())
    });
    for entry in items {
        if out.len() >= MAX_ENTRIES {
            break;
        }
        let name = entry.file_name().to_string_lossy().into_owned();
        // Hidden files (.git, .next, .env) and well-known noise dirs.
        if name.starts_with('.') || SKIP_DIRS.contains(&name.as_str()) {
            continue;
        }
        let Ok(file_type) = entry.file_type() else { continue };
        let path = entry.path().to_string_lossy().into_owned();
        if file_type.is_dir() {
            out.push(FileEntry {
                path: path.clone(),
                depth,
                kind: "dir",
                name,
            });
            walk_dir(&entry.path(), depth + 1, max_depth, out);
        } else if file_type.is_file() {
            out.push(FileEntry {
                path,
                depth,
                kind: "file",
                name,
            });
        }
    }
}

#[tauri::command]
fn list_workspace_tree(root: String, max_depth: u32) -> Result<Vec<FileEntry>, String> {
    let root_path = Path::new(&root);
    if !root_path.is_dir() {
        return Err(format!("not a directory: {root}"));
    }
    let mut out = Vec::new();
    walk_dir(root_path, 0, max_depth.min(4), &mut out);
    Ok(out)
}

#[derive(Serialize)]
struct GitStatusEntry {
    path: String,
    kind: &'static str,
}

#[tauri::command]
fn git_status(root: String) -> Result<Vec<GitStatusEntry>, String> {
    use std::process::Command;
    let root_path = Path::new(&root);
    if !root_path.is_dir() {
        return Err(format!("not a directory: {root}"));
    }
    let mut cmd = Command::new("git");
    cmd.arg("status").arg("--porcelain").arg("-z").current_dir(root_path);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }
    let output = match cmd.output() {
        Ok(o) => o,
        Err(_) => return Ok(Vec::new()), // not a git repo / no git on PATH — silent
    };
    if !output.status.success() {
        return Ok(Vec::new()); // not a git repo — silent
    }
    let mut out = Vec::new();
    for rec in output.stdout.split(|&b| b == 0) {
        if rec.len() < 4 {
            continue;
        }
        // `git status --porcelain -z` format: `XY ` + path, where X / Y are
        // index / worktree statuses. Map both to a coarse `kind`.
        let x = rec[0];
        let y = rec[1];
        let kind = match (x, y) {
            (b'?', b'?') => "untracked",
            (b'A', _) | (_, b'A') => "added",
            (b'D', _) | (_, b'D') => "deleted",
            (b'M', _) | (_, b'M') => "modified",
            (b'R', _) | (_, b'R') => "renamed",
            _ => continue,
        };
        let path = String::from_utf8_lossy(&rec[3..]).into_owned();
        out.push(GitStatusEntry { path, kind });
    }
    Ok(out)
}

#[tauri::command]
fn open_in_editor(command: String, path: String, line: Option<u32>) -> Result<(), String> {
    use std::process::{Command, Stdio};
    let trimmed = command.trim();
    if trimmed.is_empty() {
        return Err("editor command is empty".into());
    }
    // VS Code / Cursor / Windsurf understand `-g path:line`; harmless for others if `line` is None.
    let mut cmd;
    #[cfg(windows)]
    {
        // Spawn through cmd.exe so `.cmd` shims (code.cmd, cursor.cmd) resolve via PATH.
        cmd = Command::new("cmd");
        cmd.arg("/c").arg(trimmed);
        if let Some(l) = line {
            cmd.arg("-g").arg(format!("{}:{}", path, l));
        } else {
            cmd.arg(&path);
        }
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }
    #[cfg(not(windows))]
    {
        cmd = Command::new(trimmed);
        if let Some(l) = line {
            cmd.arg("-g").arg(format!("{}:{}", path, l));
        } else {
            cmd.arg(&path);
        }
    }
    cmd.stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null());
    cmd.spawn().map_err(|e| format!("spawn {trimmed}: {e}"))?;
    Ok(())
}

fn main() {
    #[cfg(target_os = "linux")]
    linux_webkit_compat();

    tauri::Builder::default()
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(RpcState::default())
        .invoke_handler(tauri::generate_handler![
            rpc_spawn,
            rpc_send,
            rpc_kill,
            open_in_editor,
            list_workspace_tree,
            git_status
        ])
        .setup(|app| {
            use tauri::Manager;
            if let Some(w) = app.get_webview_window("main") {
                // HiDPI fit: the JSON config asks for 1024x720 logical px.
                // On Windows laptops at 200% scale (1920x1080 → 960x540
                // effective logical px) that overflows the screen and the
                // window opens partially off-canvas. Clamp to 90% of the
                // monitor's available logical size whenever the configured
                // size doesn't fit, then recenter.
                if let Ok(Some(monitor)) = w.current_monitor() {
                    let scale = monitor.scale_factor();
                    let phys = monitor.size();
                    let avail_w = phys.width as f64 / scale;
                    let avail_h = phys.height as f64 / scale;
                    let want_w = 1024_f64.min(avail_w * 0.9);
                    let want_h = 720_f64.min(avail_h * 0.9);
                    if want_w < 1024.0 || want_h < 720.0 {
                        let _ = w.set_size(tauri::Size::Logical(tauri::LogicalSize {
                            width: want_w,
                            height: want_h,
                        }));
                        let _ = w.center();
                    }
                }
                if std::env::var("REASONIX_DEVTOOLS").is_ok() {
                    #[cfg(debug_assertions)]
                    w.open_devtools();
                }
            }
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("tauri build failed")
        .run(|app, event| {
            // Tauri 2 normally exits the process via Exit; managed-state drops
            // don't always run. ExitRequested fires before that, so we kill the
            // Node child here too — belt-and-braces vs the Drop on RpcHandle.
            if let tauri::RunEvent::ExitRequested { .. } = event {
                use tauri::Manager;
                let state = app.state::<RpcState>();
                let _ = rpc::rpc_kill(state);
            }
        });
}
