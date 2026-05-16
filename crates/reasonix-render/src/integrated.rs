use std::io::{self, BufRead, BufWriter, Write};
use std::sync::mpsc;
use std::thread;
use std::time::Duration;

use anyhow::{Context, Result};
use crossterm::event::{
    self, DisableMouseCapture, EnableMouseCapture, Event, KeyCode, KeyEventKind, KeyModifiers,
    MouseButton, MouseEventKind,
};
use ratatui::backend::CrosstermBackend;
use ratatui::layout::Rect;

use crate::editor::{
    char_to_byte, insert_char_at, move_cursor_line, next_word_boundary, prev_word_boundary,
    remove_char_at,
};
use crate::input::is_quit;
use crate::state::{decode_message, Payload, SceneState};
use crate::view::render_setup;
use crate::whole_screen::{
    at_completion, at_match_count, cards_layout, extract_text, slash_completion,
    slash_match_count, Selection, WholeScreen,
};

type Terminal = ratatui::Terminal<CrosstermBackend<BufWriter<io::Stdout>>>;

pub fn run_integrated_loop(terminal: &mut Terminal) -> Result<()> {
    let mut stdout = io::stdout();
    let mouse_enabled = crossterm::execute!(stdout, EnableMouseCapture).is_ok();

    let (tx, rx) = mpsc::channel::<String>();
    let _reader = thread::spawn(move || {
        let stdin = io::stdin();
        for line in stdin.lock().lines() {
            let Ok(l) = line else {
                break;
            };
            if tx.send(l).is_err() {
                break;
            }
        }
    });

    let mut scene = SceneState::default();
    let mut have_state = false;
    let mut setup_pending: Option<crate::state::SetupState> = None;
    let mut buffer = String::new();
    let mut cursor: usize = 0;
    let mut scroll_offset: u16 = 0;
    let mut selection: Option<Selection> = None;
    let mut dragging = false;
    let mut slash_idx: usize = 0;
    let mut at_idx: usize = 0;
    let mut tick: u32 = 0;
    let tick_period = Duration::from_millis(80);
    let scroll_step: u16 = 3;
    let page_step: u16 = 10;
    let mut last_size = terminal.size().ok();

    let result: Result<()> = (|| loop {
        let mut stdin_closed = false;
        loop {
            match rx.try_recv() {
                Ok(line) => {
                    if line.trim().is_empty() {
                        continue;
                    }
                    if let Ok(p) = decode_message(&line) {
                        match p {
                            Payload::Trace(s) => {
                                scene = s;
                                have_state = true;
                                setup_pending = None;
                            }
                            Payload::Setup(s) => {
                                setup_pending = Some(s);
                            }
                        }
                    }
                }
                Err(mpsc::TryRecvError::Empty) => break,
                Err(mpsc::TryRecvError::Disconnected) => {
                    stdin_closed = true;
                    break;
                }
            }
        }

        let current_size = terminal.size().ok();
        if current_size != last_size {
            terminal.clear().ok();
            last_size = current_size;
        }

        let buf_chars = buffer.chars().count();
        if cursor > buf_chars {
            cursor = buf_chars;
        }
        let slash_count = slash_match_count(&buffer);
        if slash_count == 0 {
            slash_idx = 0;
        } else if slash_idx >= slash_count {
            slash_idx = slash_count - 1;
        }
        let at_count = if slash_count > 0 {
            0
        } else {
            at_match_count(&buffer)
        };
        if at_count == 0 {
            at_idx = 0;
        } else if at_idx >= at_count {
            at_idx = at_count - 1;
        }

        let _ = crossterm::execute!(
            terminal.backend_mut(),
            crossterm::terminal::BeginSynchronizedUpdate
        );
        if let Some(setup) = setup_pending.as_ref() {
            terminal
                .draw(|f| render_setup(setup, f))
                .context("terminal draw")?;
        } else {
            let mut display = scene.clone();
            display.composer_text = Some(buffer.clone());
            display.composer_cursor = Some(cursor);
            terminal
                .draw(|f| {
                    let area = f.area();
                    f.render_widget(
                        WholeScreen::new(&display)
                            .with_scroll(scroll_offset)
                            .with_selection(selection)
                            .with_slash_index(slash_idx)
                            .with_at_index(at_idx)
                            .with_tick(tick),
                        area,
                    );
                })
                .context("terminal draw")?;
        }
        let _ = crossterm::execute!(
            terminal.backend_mut(),
            crossterm::terminal::EndSynchronizedUpdate
        );

        if stdin_closed && !have_state {
            return Ok(());
        }

        if !event::poll(tick_period)? {
            tick = tick.wrapping_add(1);
            continue;
        }

        let evt = event::read()?;
        if setup_pending.is_some() {
            tick = tick.wrapping_add(1);
            continue;
        }

        match evt {
            Event::Key(key) if key.kind != KeyEventKind::Press => continue,
            Event::Key(key) => {
                if is_quit(&key) {
                    if let Some(sel) = selection {
                        if let Ok(size) = terminal.size() {
                            let rect = Rect::new(0, 0, size.width, size.height);
                            let text = extract_text(&scene, scroll_offset, rect, sel);
                            if !text.is_empty() {
                                if let Ok(mut cb) = arboard::Clipboard::new() {
                                    let _ = cb.set_text(text);
                                }
                            }
                        }
                        selection = None;
                        continue;
                    }
                    if scene.busy {
                        emit_event(serde_json::json!({"event": "interrupt"}));
                        continue;
                    }
                    emit_event(serde_json::json!({"event": "exit"}));
                    return Ok(());
                }
                if key.code == KeyCode::Char('d')
                    && key.modifiers.contains(KeyModifiers::CONTROL)
                {
                    emit_event(serde_json::json!({"event": "exit"}));
                    return Ok(());
                }
                let slash_active = slash_count > 0;
                let at_active = !slash_active && at_count > 0;
                match key.code {
                    KeyCode::Up if slash_active => {
                        slash_idx = slash_idx.saturating_sub(1);
                    }
                    KeyCode::Down if slash_active => {
                        slash_idx = (slash_idx + 1).min(slash_count - 1);
                    }
                    KeyCode::Tab if slash_active => {
                        slash_idx = if key.modifiers.contains(KeyModifiers::SHIFT) {
                            slash_idx.saturating_sub(1)
                        } else {
                            (slash_idx + 1) % slash_count
                        };
                    }
                    KeyCode::BackTab if slash_active => {
                        slash_idx = if slash_idx == 0 {
                            slash_count - 1
                        } else {
                            slash_idx - 1
                        };
                    }
                    KeyCode::Enter if slash_active => {
                        if let Some(completion) = slash_completion(&buffer, slash_idx) {
                            cursor = completion.chars().count();
                            buffer = completion;
                        }
                    }
                    KeyCode::Up if at_active => {
                        at_idx = at_idx.saturating_sub(1);
                    }
                    KeyCode::Down if at_active => {
                        at_idx = (at_idx + 1).min(at_count - 1);
                    }
                    KeyCode::Tab if at_active => {
                        at_idx = if key.modifiers.contains(KeyModifiers::SHIFT) {
                            at_idx.saturating_sub(1)
                        } else {
                            (at_idx + 1) % at_count
                        };
                    }
                    KeyCode::BackTab if at_active => {
                        at_idx = if at_idx == 0 {
                            at_count - 1
                        } else {
                            at_idx - 1
                        };
                    }
                    KeyCode::Enter if at_active => {
                        if let Some(completion) = at_completion(&buffer, at_idx) {
                            cursor = completion.chars().count();
                            buffer = completion;
                        }
                    }
                    KeyCode::Up => {
                        cursor = move_cursor_line(&buffer, cursor, -1);
                    }
                    KeyCode::Down => {
                        cursor = move_cursor_line(&buffer, cursor, 1);
                    }
                    KeyCode::Esc => {
                        if selection.is_some() {
                            selection = None;
                        } else {
                            buffer.clear();
                            cursor = 0;
                            slash_idx = 0;
                            at_idx = 0;
                        }
                    }
                    KeyCode::PageUp => {
                        scroll_offset = scroll_offset.saturating_add(page_step);
                    }
                    KeyCode::PageDown => {
                        scroll_offset = scroll_offset.saturating_sub(page_step);
                    }
                    KeyCode::Left if key.modifiers.contains(KeyModifiers::CONTROL) => {
                        cursor = prev_word_boundary(&buffer, cursor);
                    }
                    KeyCode::Right if key.modifiers.contains(KeyModifiers::CONTROL) => {
                        cursor = next_word_boundary(&buffer, cursor);
                    }
                    KeyCode::Left => {
                        cursor = cursor.saturating_sub(1);
                    }
                    KeyCode::Right => {
                        cursor = (cursor + 1).min(buffer.chars().count());
                    }
                    KeyCode::Home => {
                        cursor = 0;
                    }
                    KeyCode::End => {
                        cursor = buffer.chars().count();
                    }
                    KeyCode::Char(c) if !key.modifiers.contains(KeyModifiers::CONTROL) => {
                        selection = None;
                        insert_char_at(&mut buffer, cursor, c);
                        cursor += 1;
                        slash_idx = 0;
                        at_idx = 0;
                        scroll_offset = 0;
                    }
                    KeyCode::Backspace if key.modifiers.contains(KeyModifiers::CONTROL) => {
                        selection = None;
                        let new_cursor = prev_word_boundary(&buffer, cursor);
                        let from_byte = char_to_byte(&buffer, new_cursor);
                        let to_byte = char_to_byte(&buffer, cursor);
                        buffer.drain(from_byte..to_byte);
                        cursor = new_cursor;
                        slash_idx = 0;
                        at_idx = 0;
                    }
                    KeyCode::Backspace => {
                        selection = None;
                        if cursor > 0 {
                            remove_char_at(&mut buffer, cursor - 1);
                            cursor -= 1;
                        }
                        slash_idx = 0;
                        at_idx = 0;
                    }
                    KeyCode::Delete => {
                        if cursor < buffer.chars().count() {
                            remove_char_at(&mut buffer, cursor);
                        }
                        slash_idx = 0;
                        at_idx = 0;
                    }
                    KeyCode::Enter if key.modifiers.contains(KeyModifiers::SHIFT) => {
                        selection = None;
                        insert_char_at(&mut buffer, cursor, '\n');
                        cursor += 1;
                        slash_idx = 0;
                        at_idx = 0;
                    }
                    KeyCode::Enter => {
                        selection = None;
                        let text = buffer.trim().to_string();
                        if !text.is_empty() {
                            emit_event(serde_json::json!({"event": "submit", "text": text}));
                        }
                        buffer.clear();
                        cursor = 0;
                        slash_idx = 0;
                        at_idx = 0;
                        scroll_offset = 0;
                    }
                    _ => {}
                }
            }
            Event::Mouse(m) => {
                if let Ok(size) = terminal.size() {
                    let rect = Rect::new(0, 0, size.width, size.height);
                    let layout = cards_layout(rect, &scene, scroll_offset);
                    match m.kind {
                        MouseEventKind::Down(MouseButton::Left) => {
                            if layout.contains_screen(m.column, m.row) {
                                let (col, virt_y) = layout.project_clamped(m.column, m.row);
                                selection = Some(Selection::new(col, virt_y));
                                dragging = true;
                            } else {
                                selection = None;
                                dragging = false;
                            }
                        }
                        MouseEventKind::Drag(MouseButton::Left) if dragging => {
                            let (col, virt_y) = layout.project_clamped(m.column, m.row);
                            if let Some(s) = selection.as_mut() {
                                s.extend(col, virt_y);
                            }
                            let top = layout.screen_rect.y;
                            let bottom = layout.screen_rect.bottom();
                            if m.row < top {
                                scroll_offset = scroll_offset.saturating_add(1);
                            } else if bottom > 0 && m.row >= bottom.saturating_sub(1) {
                                scroll_offset = scroll_offset.saturating_sub(1);
                            }
                        }
                        MouseEventKind::Up(MouseButton::Left) => {
                            dragging = false;
                            if let Some(sel) = selection {
                                if !sel.is_empty() {
                                    let text = extract_text(&scene, scroll_offset, rect, sel);
                                    if !text.is_empty() {
                                        if let Ok(mut cb) = arboard::Clipboard::new() {
                                            let _ = cb.set_text(text);
                                        }
                                    }
                                }
                            }
                        }
                        MouseEventKind::ScrollUp => {
                            scroll_offset = scroll_offset.saturating_add(scroll_step);
                        }
                        MouseEventKind::ScrollDown => {
                            scroll_offset = scroll_offset.saturating_sub(scroll_step);
                        }
                        _ => {}
                    }
                }
            }
            Event::Resize(_, _) => {}
            _ => {}
        }
        tick = tick.wrapping_add(1);
    })();

    if mouse_enabled {
        let _ = crossterm::execute!(io::stdout(), DisableMouseCapture);
    }
    result
}

fn emit_event(event: serde_json::Value) {
    if let Ok(s) = serde_json::to_string(&event) {
        let mut out = io::stderr().lock();
        let _ = writeln!(out, "{s}");
        let _ = out.flush();
    }
}
