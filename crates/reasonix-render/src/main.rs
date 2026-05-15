use std::io::{self, BufRead};

use anyhow::{Context, Result};
use crossterm::execute;
use crossterm::terminal::{EnterAlternateScreen, LeaveAlternateScreen};
use ratatui::backend::CrosstermBackend;
use ratatui::Terminal;

use reasonix_render::decode_only::run_decode_only;
use reasonix_render::render::render_frame;
use reasonix_render::scene::SceneFrame;

fn main() -> Result<()> {
    if std::env::args().skip(1).any(|a| a == "--decode-only") {
        let stdin = io::stdin();
        let stdout = io::stdout();
        run_decode_only(stdin.lock(), stdout.lock())?;
        return Ok(());
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
