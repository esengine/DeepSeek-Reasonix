use std::io::{self, BufRead, Write};
use std::time::Duration;

use anyhow::{Context, Result};
use crossterm::event::{self, Event, KeyCode};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::Terminal;

use reasonix_render::render::render_frame;
use reasonix_render::scene::SceneFrame;

fn main() -> Result<()> {
    let frames = read_frames()?;
    if frames.is_empty() {
        anyhow::bail!("no frames on stdin");
    }
    let mut stdout = io::stdout();
    enable_raw_mode().context("enable raw mode")?;
    execute!(stdout, EnterAlternateScreen).context("enter alt screen")?;
    let backend = CrosstermBackend::new(stdout);
    let mut terminal = Terminal::new(backend).context("create terminal")?;

    let result = run_loop(&mut terminal, &frames);

    disable_raw_mode().ok();
    execute!(terminal.backend_mut(), LeaveAlternateScreen).ok();
    terminal.show_cursor().ok();
    result
}

fn read_frames() -> Result<Vec<SceneFrame>> {
    let stdin = io::stdin();
    let mut out = Vec::new();
    for (lineno, line) in stdin.lock().lines().enumerate() {
        let line = line.with_context(|| format!("read line {}", lineno + 1))?;
        if line.trim().is_empty() {
            continue;
        }
        let frame: SceneFrame =
            serde_json::from_str(&line).with_context(|| format!("decode line {}", lineno + 1))?;
        out.push(frame);
    }
    Ok(out)
}

fn run_loop(
    terminal: &mut Terminal<CrosstermBackend<io::Stdout>>,
    frames: &[SceneFrame],
) -> Result<()> {
    let mut idx = 0usize;
    let dwell = Duration::from_millis(800);
    loop {
        let frame = &frames[idx];
        terminal.draw(|f| {
            let area = f.area();
            render_frame(frame, f.buffer_mut(), area);
        })?;
        if event::poll(dwell)? {
            if let Event::Key(k) = event::read()? {
                match k.code {
                    KeyCode::Char('q') | KeyCode::Esc => return Ok(()),
                    KeyCode::Char('n') | KeyCode::Right => {
                        idx = (idx + 1) % frames.len();
                        continue;
                    }
                    KeyCode::Char('p') | KeyCode::Left => {
                        idx = if idx == 0 { frames.len() - 1 } else { idx - 1 };
                        continue;
                    }
                    _ => {}
                }
            }
        }
        idx = (idx + 1) % frames.len();
        writeln!(io::sink()).ok();
    }
}
