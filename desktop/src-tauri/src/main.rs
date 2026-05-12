#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod rpc;

use rpc::{RpcState, rpc_send, rpc_spawn};

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
    tauri::Builder::default()
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        .manage(RpcState::default())
        .invoke_handler(tauri::generate_handler![rpc_spawn, rpc_send, open_in_editor])
        .setup(|app| {
            if std::env::var("REASONIX_DEVTOOLS").is_ok() {
                #[cfg(debug_assertions)]
                {
                    use tauri::Manager;
                    if let Some(w) = app.get_webview_window("main") {
                        w.open_devtools();
                    }
                }
            }
            let _ = app;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("tauri run failed");
}
