use std::io::{self, BufRead, Write};

use anyhow::{Context, Result};
use crossterm::event::{self, Event};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::Terminal;

use reasonix_render::decode_only::run_decode_only;
use reasonix_render::input::{is_quit, translate_key};
use reasonix_render::render::render_frame;
use reasonix_render::scene::SceneFrame;

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

    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen).context("enter alt screen")?;
    let backend = CrosstermBackend::new(stdout);
    let mut terminal = Terminal::new(backend).context("create terminal")?;

    let result = run_stream_loop(&mut terminal);

    execute!(terminal.backend_mut(), LeaveAlternateScreen).ok();
    terminal.show_cursor().ok();
    result
}

fn run_stream_loop(terminal: &mut Terminal<CrosstermBackend<io::Stdout>>) -> Result<()> {
    let stdin = io::stdin();
    for (lineno, line) in stdin.lock().lines().enumerate() {
        let line = line.with_context(|| format!("read line {}", lineno + 1))?;
        if line.trim().is_empty() {
            continue;
        }
        let frame: SceneFrame =
            serde_json::from_str(&line).with_context(|| format!("decode line {}", lineno + 1))?;
        terminal.draw(|f| {
            let area = f.area();
            render_frame(&frame, f.buffer_mut(), area);
        })?;
    }
    Ok(())
}

fn run_emit_input() -> Result<()> {
    enable_raw_mode().context("enable raw mode")?;
    let result = emit_input_loop();
    disable_raw_mode().ok();
    result
}

fn emit_input_loop() -> Result<()> {
    let stdout = io::stdout();
    let mut out = stdout.lock();
    loop {
        let Event::Key(key) = event::read()? else {
            continue;
        };
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
}
