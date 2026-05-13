use std::env;
use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{ChildStdin, Command, Stdio};
use std::sync::Arc;
use std::sync::mpsc;
use std::thread;
use std::time::Duration;

use anyhow::{Context, Result, anyhow};
use parking_lot::Mutex;
use serde::Serialize;
use tauri::{AppHandle, Emitter, State};
#[cfg(not(debug_assertions))]
use tauri::Manager;
use which::which_all;

#[derive(Default)]
pub struct RpcState {
    inner: Arc<Mutex<Option<RpcHandle>>>,
}

struct RpcHandle {
    stdin: ChildStdin,
    /// Held only so its drop disconnects the wait thread's recv, signalling
    /// it to kill the child. Replaces the old fire-and-forget exit watcher
    /// that left Node orphaned if Tauri exited while a tool call was in flight.
    _kill_signal: mpsc::Sender<()>,
}

#[derive(Clone, Serialize)]
struct LineEvent {
    data: String,
}

#[derive(Clone, Serialize)]
struct ExitEvent {
    code: Option<i32>,
}

fn resolve_cli(app: &AppHandle) -> Result<(String, Vec<String>)> {
    if let Ok(custom) = env::var("REASONIX_CLI") {
        let mut parts = custom.split_whitespace().map(String::from);
        let program = parts
            .next()
            .ok_or_else(|| anyhow!("REASONIX_CLI is empty"))?;
        return Ok((program, parts.collect()));
    }

    // Production path: bundled Node + bundled CLI inside resource_dir.
    // tauri.conf.json bundle.resources maps:
    //   binaries/node.exe → <resource_dir>/node.exe
    //   ../../dist        → <resource_dir>/dist
    //
    // Dev (debug_assertions) skips this entirely — the on-disk node.exe is a
    // 0-byte placeholder kept just so tauri-build's resource validator passes;
    // spawning it is what produced error 193 ("not a valid Win32 application").
    #[cfg(not(debug_assertions))]
    if let Ok(res_dir) = app.path().resource_dir() {
        let node_name = if cfg!(windows) { "node.exe" } else { "node" };
        let node_path = res_dir.join(node_name);
        let cli_path = res_dir.join("dist").join("cli").join("index.js");
        let is_real_node = node_path
            .metadata()
            .map(|m| m.len() > 1_000_000)
            .unwrap_or(false);
        if is_real_node && cli_path.exists() {
            return Ok((
                node_path.to_string_lossy().into_owned(),
                vec![cli_path.to_string_lossy().into_owned(), "desktop".to_string()],
            ));
        }
    }
    let _ = app;

    // Dev path: system Node + repo dist (cargo run / cargo tauri dev).
    let cwd = env::current_dir().context("cwd")?;
    let candidates = [
        cwd.join("../../dist/cli/index.js"),
        cwd.join("../dist/cli/index.js"),
        cwd.join("dist/cli/index.js"),
    ];
    let entry = candidates
        .into_iter()
        .find(|p| p.exists())
        .map(PathBuf::from)
        .ok_or_else(|| anyhow!("dist/cli/index.js not found — run `npm run build` at repo root"))?;

    let node_path = find_real_node().context("node not found")?;
    eprintln!("[reasonix] resolved node: {}", node_path.display());

    Ok((
        node_path.to_string_lossy().into_owned(),
        vec![entry.to_string_lossy().to_string(), "desktop".to_string()],
    ))
}

/// Walk every PATH match for `node` and return the first one that is
/// (a) a real file > 100 KB and (b) NOT inside Windows' App Execution
/// Alias directory — those Microsoft Store stubs are 0-byte and triggered
/// the original "%1 is not a valid Win32 application" (error 193).
fn find_real_node() -> Result<PathBuf> {
    let names: &[&str] = if cfg!(windows) {
        &["node.exe", "node"]
    } else {
        &["node"]
    };
    let mut tried: Vec<String> = Vec::new();
    for name in names {
        if let Ok(iter) = which_all(*name) {
            for p in iter {
                let size = std::fs::metadata(&p).map(|m| m.len()).unwrap_or(0);
                let lower = p.to_string_lossy().to_lowercase();
                let is_ms_store_shim = lower.contains("windowsapps");
                let too_small = size < 100_000;
                if !too_small && !is_ms_store_shim {
                    return Ok(p);
                }
                tried.push(format!(
                    "{} ({} bytes{})",
                    p.display(),
                    size,
                    if is_ms_store_shim { ", MS Store shim" } else { "" },
                ));
            }
        }
    }
    Err(anyhow!(
        "node not found in PATH or only stub binaries present.{}\nInstall Node 22 from nodejs.org and reopen Reasonix.",
        if tried.is_empty() {
            String::new()
        } else {
            format!(" Skipped: {}.", tried.join("; "))
        }
    ))
}

#[tauri::command]
pub fn rpc_spawn(app: AppHandle, state: State<'_, RpcState>) -> Result<(), String> {
    let mut guard = state.inner.lock();
    if guard.is_some() {
        return Err("rpc already spawned".into());
    }

    let (program, args) = resolve_cli(&app).map_err(|e| e.to_string())?;
    let mut cmd = Command::new(program);
    cmd.args(args);
    cmd.stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());

    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }

    if let Ok(cwd) = env::current_dir() {
        let repo_root = cwd
            .ancestors()
            .find(|p| p.join("package.json").exists() && p.join("src/cli").exists())
            .unwrap_or(&cwd)
            .to_path_buf();
        cmd.current_dir(repo_root);
    }

    let mut child = cmd.spawn().map_err(|e| format!("spawn: {e}"))?;
    let stdin = child.stdin.take().ok_or("no stdin")?;
    let stdout = child.stdout.take().ok_or("no stdout")?;
    let stderr = child.stderr.take().ok_or("no stderr")?;

    let (kill_tx, kill_rx) = mpsc::channel::<()>();
    *guard = Some(RpcHandle { stdin, _kill_signal: kill_tx });
    drop(guard);

    let app_for_stdout = app.clone();
    thread::spawn(move || {
        let reader = BufReader::new(stdout);
        for line in reader.lines().map_while(Result::ok) {
            let _ = app_for_stdout.emit("rpc:event", LineEvent { data: line });
        }
    });

    let app_for_stderr = app.clone();
    thread::spawn(move || {
        let reader = BufReader::new(stderr);
        for line in reader.lines().map_while(Result::ok) {
            let _ = app_for_stderr.emit("rpc:stderr", LineEvent { data: line });
        }
    });

    // Wait thread: poll try_wait every 500 ms while watching the kill channel.
    // Disconnected fires when RpcHandle drops (rpc_kill or Tauri exit) — the
    // 500 ms cadence is a tradeoff between exit-detection latency and idle CPU.
    let app_for_exit = app.clone();
    let inner_for_exit = state.inner.clone();
    thread::spawn(move || {
        loop {
            match kill_rx.recv_timeout(Duration::from_millis(500)) {
                Ok(()) | Err(mpsc::RecvTimeoutError::Disconnected) => {
                    let _ = child.kill();
                    let code = child.wait().ok().and_then(|s| s.code());
                    let _ = app_for_exit.emit("rpc:exit", ExitEvent { code });
                    return;
                }
                Err(mpsc::RecvTimeoutError::Timeout) => match child.try_wait() {
                    Ok(Some(status)) => {
                        let _ = inner_for_exit.lock().take();
                        let _ = app_for_exit.emit("rpc:exit", ExitEvent { code: status.code() });
                        return;
                    }
                    Ok(None) => continue,
                    Err(_) => {
                        let _ = child.kill();
                        let _ = inner_for_exit.lock().take();
                        return;
                    }
                },
            }
        }
    });

    Ok(())
}

/// Drop the spawned Node child cleanly. Used by Tauri-side teardown and by a
/// future "restart RPC" UI affordance. Idempotent — no-op if nothing is running.
#[tauri::command]
pub fn rpc_kill(state: State<'_, RpcState>) -> Result<(), String> {
    state.inner.lock().take();
    Ok(())
}

#[tauri::command]
pub fn rpc_send(state: State<'_, RpcState>, line: String) -> Result<(), String> {
    let mut guard = state.inner.lock();
    let handle = guard.as_mut().ok_or("rpc not spawned")?;
    writeln!(handle.stdin, "{line}").map_err(|e| format!("write: {e}"))?;
    handle.stdin.flush().map_err(|e| format!("flush: {e}"))?;
    Ok(())
}
