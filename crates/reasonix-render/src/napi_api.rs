// NAPI surface for the integrated renderer.
//
// JS calls `createRenderer(onEvent)` and receives a `Renderer` whose
// `emit()` pushes scene-state JSON into the rust event loop, and `close()`
// tears it down. Events from rust (submit, exit, approval, etc.) are
// delivered back via the `onEvent` callback as JSON strings — JS parses.
//
// One in-process renderer per Node process. Concurrent `createRenderer`
// calls return an error rather than racing two ratatui loops over one TTY.

use std::io::{self, BufWriter};
use std::sync::{Arc, Mutex, OnceLock};
use std::thread::JoinHandle;

use anyhow::{Context, Result};
use crossterm::terminal::{disable_raw_mode, enable_raw_mode};
use napi::threadsafe_function::{ErrorStrategy, ThreadsafeFunction, ThreadsafeFunctionCallMode};
use napi::JsFunction;
use napi_derive::napi;
use ratatui::backend::CrosstermBackend;

type RenderTerminal = ratatui::Terminal<CrosstermBackend<BufWriter<io::Stdout>>>;

/// Single-process emitter: ratatui's event-loop thread calls into this to
/// deliver events back to JS. `OnceLock` means there's exactly one renderer
/// alive per Node process — a second `createRenderer` returns an error
/// rather than overwriting.
static ACTIVE_EMITTER: OnceLock<Arc<NapiEmitter>> = OnceLock::new();

pub fn active_emitter() -> Option<Arc<NapiEmitter>> {
    ACTIVE_EMITTER.get().cloned()
}

pub struct NapiEmitter {
    tsfn: ThreadsafeFunction<String, ErrorStrategy::Fatal>,
}

impl NapiEmitter {
    pub fn emit(&self, event: &serde_json::Value) {
        let Ok(s) = serde_json::to_string(event) else {
            return;
        };
        self.tsfn.call(s, ThreadsafeFunctionCallMode::NonBlocking);
    }
}

#[napi]
pub struct Renderer {
    sender: Mutex<Option<std::sync::mpsc::Sender<String>>>,
    join_handle: Mutex<Option<JoinHandle<Result<()>>>>,
}

#[napi]
impl Renderer {
    /// Push a scene-state message (Trace or Setup) to the rust event loop.
    /// `message` is the same shape currently serialized by Node into the
    /// stdin pipe / socket: a JSON object with `type: "trace" | "setup"`.
    #[napi]
    pub fn emit(&self, message: String) -> napi::Result<()> {
        let guard = self.sender.lock().map_err(poisoned)?;
        let Some(tx) = guard.as_ref() else {
            return Err(napi::Error::from_reason("renderer closed"));
        };
        tx.send(message)
            .map_err(|_| napi::Error::from_reason("renderer channel dropped"))?;
        Ok(())
    }

    /// Tear down: drops the scene channel (event loop notices Disconnected
    /// and returns), joins the thread, restores the terminal. Idempotent.
    #[napi]
    pub fn close(&self) -> napi::Result<()> {
        {
            let mut guard = self.sender.lock().map_err(poisoned)?;
            guard.take();
        }
        let handle = {
            let mut guard = self.join_handle.lock().map_err(poisoned)?;
            guard.take()
        };
        if let Some(h) = handle {
            let _ = h.join();
        }
        Ok(())
    }
}

#[napi]
pub fn create_renderer(on_event: JsFunction) -> napi::Result<Renderer> {
    let tsfn: ThreadsafeFunction<String, ErrorStrategy::Fatal> =
        on_event.create_threadsafe_function(0, |ctx| Ok(vec![ctx.value]))?;

    let emitter = Arc::new(NapiEmitter { tsfn });
    ACTIVE_EMITTER
        .set(emitter.clone())
        .map_err(|_| napi::Error::from_reason("renderer already active in this process"))?;

    install_panic_hook();
    install_signal_cleanup();

    let (tx, rx) = std::sync::mpsc::channel::<String>();

    let join = std::thread::Builder::new()
        .name("reasonix-render".to_string())
        .spawn(move || -> Result<()> {
            let mut terminal = init_terminal().context("init terminal")?;
            terminal.hide_cursor().ok();
            terminal.clear().ok();
            let result = crate::integrated::run_integrated_loop(&mut terminal, rx);
            restore_terminal(&mut terminal);
            result
        })
        .map_err(|e| napi::Error::from_reason(format!("spawn render thread: {e}")))?;

    Ok(Renderer {
        sender: Mutex::new(Some(tx)),
        join_handle: Mutex::new(Some(join)),
    })
}

#[napi]
pub fn hello() -> String {
    "reasonix-render napi ok".to_string()
}

fn init_terminal() -> Result<RenderTerminal> {
    enable_raw_mode().context("enable raw mode")?;
    let mut stdout = BufWriter::new(io::stdout());
    crossterm::execute!(stdout, crossterm::terminal::EnterAlternateScreen)
        .context("enter alt screen")?;
    let backend = CrosstermBackend::new(stdout);
    let terminal = ratatui::Terminal::new(backend).context("create terminal")?;
    Ok(terminal)
}

fn restore_terminal(terminal: &mut RenderTerminal) {
    terminal.show_cursor().ok();
    crossterm::execute!(
        terminal.backend_mut(),
        crossterm::terminal::LeaveAlternateScreen
    )
    .ok();
    disable_raw_mode().ok();
}

/// Defensive cleanup so a rust panic on the render thread can't leave the
/// host terminal stuck in alt-screen + raw mode (Node would otherwise keep
/// running, but the user's shell on exit looks broken).
fn install_panic_hook() {
    static INSTALLED: OnceLock<()> = OnceLock::new();
    if INSTALLED.set(()).is_err() {
        return;
    }
    let original = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        let _ = disable_raw_mode();
        let _ = crossterm::execute!(io::stdout(), crossterm::terminal::LeaveAlternateScreen);
        original(info);
    }));
}

/// Same cleanup on SIGTERM / SIGHUP / Windows console-close. Interactive
/// Ctrl+C is handled by `is_quit` inside the event loop (raw mode delivers
/// it as a keypress, not a signal), so this only covers parent-process
/// death paths.
fn install_signal_cleanup() {
    static INSTALLED: OnceLock<()> = OnceLock::new();
    if INSTALLED.set(()).is_err() {
        return;
    }
    let _ = ctrlc::set_handler(|| {
        let _ = disable_raw_mode();
        let _ = crossterm::execute!(
            io::stdout(),
            crossterm::terminal::LeaveAlternateScreen,
            crossterm::cursor::Show,
        );
        std::process::exit(130);
    });
}

fn poisoned<T>(_: T) -> napi::Error {
    napi::Error::from_reason("renderer mutex poisoned")
}
