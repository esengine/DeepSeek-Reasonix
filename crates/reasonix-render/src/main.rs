use std::io::{self, BufRead, Write};

use anyhow::{Context, Result};
use crossterm::event::{
    self, DisableBracketedPaste, DisableMouseCapture, EnableBracketedPaste, EnableMouseCapture,
    Event,
};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::Terminal;

use reasonix_render::decode_only::run_decode_only;
use reasonix_render::input::{is_quit, paste_event, translate_key, translate_mouse};
use reasonix_render::producer::{build_setup_frame, build_trace_frame};
use reasonix_render::render::render_frame;
use reasonix_render::scene::SceneFrame;
use reasonix_render::state::{Message, SceneState, SetupState};

fn debug_log(msg: &str) {
    let Ok(enabled) = std::env::var("REASONIX_RENDER_DEBUG") else {
        return;
    };
    if enabled.is_empty() || enabled == "0" {
        return;
    }
    let path = std::env::var("REASONIX_RENDER_DEBUG_LOG").unwrap_or_else(|_| {
        let home =
            std::env::var("USERPROFILE").or_else(|_| std::env::var("HOME")).unwrap_or_default();
        if home.is_empty() {
            "reasonix-render-debug.log".to_string()
        } else {
            format!("{home}/.reasonix/rust-render-debug.log")
        }
    });
    if let Some(parent) = std::path::Path::new(&path).parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    if let Ok(mut f) = std::fs::OpenOptions::new().create(true).append(true).open(&path) {
        let _ = writeln!(f, "[reasonix-render] {msg}");
    }
}

fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.iter().any(|a| a == "--decode-only") {
        let stdin = io::stdin();
        let stdout = io::stdout();
        run_decode_only(stdin.lock(), stdout.lock())?;
        return Ok(());
    }
    if args.iter().any(|a| a == "--emit-input") {
        return run_emit_input();
    }

    enable_raw_mode().context("enable raw mode")?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen).context("enter alt screen")?;
    let backend = CrosstermBackend::new(stdout);
    let mut terminal = Terminal::new(backend).context("create terminal")?;
    terminal.hide_cursor().ok();
    terminal.clear().ok();

    if let Ok(size) = terminal.size() {
        debug_log(&format!("startup terminal.size = {}x{}", size.width, size.height));
    }

    let result = run_stream_loop(&mut terminal);

    terminal.show_cursor().ok();
    execute!(terminal.backend_mut(), LeaveAlternateScreen).ok();
    disable_raw_mode().ok();
    result
}

fn run_stream_loop(terminal: &mut Terminal<CrosstermBackend<io::Stdout>>) -> Result<()> {
    let stdin = io::stdin();
    let mut logged_first_frame = false;
    for (lineno, line) in stdin.lock().lines().enumerate() {
        let line = line.with_context(|| format!("read line {}", lineno + 1))?;
        if line.trim().is_empty() {
            continue;
        }
        let frame = decode_to_frame(&line, terminal_size(terminal))
            .with_context(|| format!("decode line {}", lineno + 1))?;
        terminal.draw(|f| {
            let area = f.area();
            if !logged_first_frame {
                debug_log(&format!(
                    "first frame area = x={} y={} w={} h={}",
                    area.x, area.y, area.width, area.height
                ));
            }
            render_frame(&frame, f.buffer_mut(), area);
        })?;
        logged_first_frame = true;
    }
    Ok(())
}

fn terminal_size(terminal: &Terminal<CrosstermBackend<io::Stdout>>) -> (u16, u16) {
    match terminal.size() {
        Ok(size) => (size.width, size.height),
        Err(_) => (80, 24),
    }
}

fn decode_to_frame(line: &str, (cols, rows): (u16, u16)) -> Result<SceneFrame> {
    if let Ok(msg) = serde_json::from_str::<Message>(line) {
        return Ok(match msg {
            Message::Trace(state) => build_trace_frame(&state, cols, rows),
            Message::Setup(state) => build_setup_frame(&state, cols, rows),
        });
    }
    if let Ok(state) = serde_json::from_str::<SceneState>(line) {
        return Ok(build_trace_frame(&state, cols, rows));
    }
    if let Ok(state) = serde_json::from_str::<SetupState>(line) {
        return Ok(build_setup_frame(&state, cols, rows));
    }
    let legacy: SceneFrame = serde_json::from_str(line)?;
    Ok(legacy)
}

fn run_emit_input() -> Result<()> {
    enable_raw_mode().context("enable raw mode")?;
    let mut stdout_for_setup = io::stdout();
    let paste_enabled = execute!(stdout_for_setup, EnableBracketedPaste).is_ok();
    let mouse_enabled = execute!(stdout_for_setup, EnableMouseCapture).is_ok();
    let result = emit_input_loop();
    if mouse_enabled {
        execute!(stdout_for_setup, DisableMouseCapture).ok();
    }
    if paste_enabled {
        execute!(stdout_for_setup, DisableBracketedPaste).ok();
    }
    disable_raw_mode().ok();
    result
}

fn emit_input_loop() -> Result<()> {
    let stdout = io::stdout();
    let mut out = stdout.lock();
    loop {
        match event::read()? {
            Event::Key(key) => {
                if is_quit(&key) {
                    return Ok(());
                }
                let Some(translated) = translate_key(&key) else {
                    continue;
                };
                let json = serde_json::to_string(&translated).context("serialize input event")?;
                writeln!(out, "{json}").context("write input event")?;
                out.flush().context("flush stdout")?;
            }
            Event::Paste(text) => {
                let event = paste_event(text);
                let json = serde_json::to_string(&event).context("serialize paste event")?;
                writeln!(out, "{json}").context("write paste event")?;
                out.flush().context("flush stdout")?;
            }
            Event::Mouse(m) => {
                let Some(translated) = translate_mouse(&m) else {
                    continue;
                };
                let json = serde_json::to_string(&translated).context("serialize mouse event")?;
                writeln!(out, "{json}").context("write mouse event")?;
                out.flush().context("flush stdout")?;
            }
            _ => {}
        }
    }
}
