use std::env;
use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

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
    /// Shared with the natural-exit watcher thread. `rpc_kill` takes the
    /// Child on app shutdown so it can poll try_wait synchronously and then
    /// tree-kill on timeout — Tauri's stock `child.kill()` only hits the
    /// direct Node child and leaves vite / http-server descendants orphaned
    /// past app shutdown (#907).
    child: Arc<parking_lot::Mutex<Option<Child>>>,
    child_pid: u32,
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
    let child_pid = child.id();

    let child_shared = Arc::new(parking_lot::Mutex::new(Some(child)));
    *guard = Some(RpcHandle {
        stdin,
        child: child_shared.clone(),
        child_pid,
    });
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

    // Natural-exit watcher: emits rpc:exit if Node dies on its own (crash,
    // explicit process.exit). rpc_kill owns the active-shutdown path; this
    // thread bails the moment that path takes the Child.
    let app_for_exit = app.clone();
    let inner_for_exit = state.inner.clone();
    let child_for_wait = child_shared;
    thread::spawn(move || loop {
        let status = {
            let mut guard = child_for_wait.lock();
            match guard.as_mut() {
                Some(c) => c.try_wait(),
                None => return,
            }
        };
        match status {
            Ok(Some(s)) => {
                child_for_wait.lock().take();
                let _ = inner_for_exit.lock().take();
                let _ = app_for_exit.emit("rpc:exit", ExitEvent { code: s.code() });
                return;
            }
            Ok(None) => thread::sleep(Duration::from_millis(500)),
            Err(_) => {
                let _ = inner_for_exit.lock().take();
                return;
            }
        }
    });

    Ok(())
}

fn kill_process_tree(pid: u32) {
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        let _ = Command::new("taskkill")
            .args(["/F", "/T", "/PID", &pid.to_string()])
            .creation_flags(CREATE_NO_WINDOW)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status();
    }
    #[cfg(not(windows))]
    {
        let _ = Command::new("kill")
            .args(["-KILL", &pid.to_string()])
            .status();
        let _ = Command::new("pkill")
            .args(["-KILL", "-P", &pid.to_string()])
            .status();
    }
}

/// Drop the spawned Node child cleanly. Used by Tauri-side teardown.
/// Closes Node's stdin (which trips its readline `close` → graceful job
/// shutdown), polls the child for up to 3 s, then tree-kills any survivors
/// so dev-server grandchildren don't outlive the desktop (#907).
#[tauri::command]
pub fn rpc_kill(state: State<'_, RpcState>) -> Result<(), String> {
    let handle_opt = state.inner.lock().take();
    let Some(handle) = handle_opt else { return Ok(()) };

    drop(handle.stdin);

    let deadline = Instant::now() + Duration::from_secs(3);
    let exited = loop {
        let done = {
            let mut guard = handle.child.lock();
            match guard.as_mut() {
                Some(c) => match c.try_wait() {
                    Ok(Some(_)) => {
                        guard.take();
                        true
                    }
                    Ok(None) => false,
                    Err(_) => {
                        guard.take();
                        true
                    }
                },
                None => true,
            }
        };
        if done {
            break true;
        }
        if Instant::now() >= deadline {
            break false;
        }
        thread::sleep(Duration::from_millis(50));
    };

    if !exited {
        kill_process_tree(handle.child_pid);
        if let Some(mut c) = handle.child.lock().take() {
            let _ = c.wait();
        }
    }

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
