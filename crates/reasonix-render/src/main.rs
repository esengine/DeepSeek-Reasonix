use std::io::{self, BufRead, BufWriter, Write};

use anyhow::{Context, Result};
use crossterm::event::{
    self, DisableBracketedPaste, DisableMouseCapture, EnableBracketedPaste, EnableMouseCapture,
    Event,
};
use crossterm::execute;
use crossterm::terminal::{disable_raw_mode, enable_raw_mode};
use ratatui::backend::CrosstermBackend;

use reasonix_render::decode_only::run_decode_only;
use reasonix_render::editor::{
    char_to_byte, insert_char_at, move_cursor_line, next_word_boundary, prev_word_boundary,
    remove_char_at,
};
use reasonix_render::frame_cache::FrameCache;
use reasonix_render::input::{is_quit, paste_event, translate_key, translate_mouse};
use reasonix_render::state::{decode_message, Payload};
use reasonix_render::view::render_setup;
use reasonix_render::whole_screen::{
    at_completion, at_match_count, cards_layout, demo_state, extract_text, slash_completion,
    slash_match_count, Selection, WholeScreen,
};

type RenderTerminal = ratatui::Terminal<CrosstermBackend<BufWriter<io::Stdout>>>;

fn debug_log(msg: &str) {
    let Ok(enabled) = std::env::var("REASONIX_RENDER_DEBUG") else {
        return;
    };
    if enabled.is_empty() || enabled == "0" {
        return;
    }
    let path = std::env::var("REASONIX_RENDER_DEBUG_LOG").unwrap_or_else(|_| {
        let home = std::env::var("USERPROFILE")
            .or_else(|_| std::env::var("HOME"))
            .unwrap_or_default();
        if home.is_empty() {
            "reasonix-render-debug.log".to_string()
        } else {
            format!("{home}/.reasonix/rust-render-debug.log")
        }
    });
    if let Some(parent) = std::path::Path::new(&path).parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    if let Ok(mut f) = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&path)
    {
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

    install_panic_hook();
    let mut terminal = init_terminal().context("init terminal")?;
    terminal.hide_cursor().ok();
    terminal.clear().ok();

    if let Ok(size) = terminal.size() {
        debug_log(&format!(
            "startup terminal.size = {}x{}",
            size.width, size.height
        ));
    }

    let result = if args.iter().any(|a| a == "--demo") {
        run_demo_loop(&mut terminal)
    } else if args.iter().any(|a| a == "--integrated") {
        reasonix_render::integrated::run_integrated_loop(&mut terminal)
    } else {
        run_stream_loop(&mut terminal)
    };

    restore_terminal(&mut terminal);
    result
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

fn install_panic_hook() {
    let original = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        let home = std::env::var("USERPROFILE")
            .or_else(|_| std::env::var("HOME"))
            .unwrap_or_default();
        if !home.is_empty() {
            let path = format!("{home}/.reasonix/rust-panic.log");
            if let Some(parent) = std::path::Path::new(&path).parent() {
                let _ = std::fs::create_dir_all(parent);
            }
            if let Ok(mut f) = std::fs::OpenOptions::new()
                .create(true)
                .append(true)
                .open(&path)
            {
                let now = chrono::Local::now().format("%Y-%m-%d %H:%M:%S%.3f");
                let _ = writeln!(f, "[{now}] {info}");
                let bt = std::backtrace::Backtrace::force_capture();
                let _ = writeln!(f, "{bt}\n");
            }
        }
        let _ = disable_raw_mode();
        let _ = crossterm::execute!(io::stdout(), crossterm::terminal::LeaveAlternateScreen);
        original(info);
    }));
}

fn run_demo_loop(terminal: &mut RenderTerminal) -> Result<()> {
    use crossterm::event::{
        DisableMouseCapture, EnableMouseCapture, KeyCode, KeyEventKind, KeyModifiers, MouseButton,
        MouseEventKind,
    };
    use reasonix_render::state::SceneCard;

    let mut stdout = io::stdout();
    let mouse_enabled = crossterm::execute!(stdout, EnableMouseCapture).is_ok();

    let mut state = demo_state();
    let mut buffer = String::new();
    let mut cursor: usize = 0;
    let mut scroll_offset: u16 = 0;
    let mut selection: Option<Selection> = None;
    let mut dragging = false;
    let mut slash_idx: usize = 0;
    let mut at_idx: usize = 0;
    let mut sidebar_visible = true;
    let mut tick: u32 = 0;
    let tick_period = std::time::Duration::from_millis(80);
    let scroll_step: u16 = 3;
    let page_step: u16 = 10;

    let result: Result<()> = (|| loop {
        let buf_chars = buffer.chars().count();
        if cursor > buf_chars {
            cursor = buf_chars;
        }
        state.composer_text = Some(buffer.clone());
        state.composer_cursor = Some(cursor);

        let slash_count = slash_match_count(&buffer, &state);
        if slash_count == 0 {
            slash_idx = 0;
        } else if slash_idx >= slash_count {
            slash_idx = slash_count - 1;
        }
        let at_count = if slash_count > 0 {
            0
        } else {
            at_match_count(&buffer, &state)
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
        terminal
            .draw(|f| {
                let area = f.area();
                f.render_widget(
                    WholeScreen::new(&state)
                        .with_scroll(scroll_offset)
                        .with_selection(selection)
                        .with_slash_index(slash_idx)
                        .with_at_index(at_idx)
                        .with_sidebar_visible(sidebar_visible)
                        .with_tick(tick),
                    area,
                );
            })
            .context("terminal draw")?;
        let _ = crossterm::execute!(
            terminal.backend_mut(),
            crossterm::terminal::EndSynchronizedUpdate
        );

        if !event::poll(tick_period)? {
            tick = tick.wrapping_add(1);
            continue;
        }

        match event::read()? {
            Event::Key(key) if key.kind != KeyEventKind::Press => continue,
            Event::Key(key) => {
                if is_quit(&key) {
                    if let Some(sel) = selection {
                        if let Ok(size) = terminal.size() {
                            let rect = ratatui::layout::Rect::new(0, 0, size.width, size.height);
                            let text =
                                extract_text(&state, scroll_offset, rect, sel, sidebar_visible);
                            if !text.is_empty() {
                                if let Ok(mut cb) = arboard::Clipboard::new() {
                                    let _ = cb.set_text(text);
                                }
                            }
                        }
                        selection = None;
                        continue;
                    }
                    return Ok(());
                }
                if key.code == KeyCode::Char('d') && key.modifiers.contains(KeyModifiers::CONTROL) {
                    return Ok(());
                }
                if key.code == KeyCode::Char('b') && key.modifiers.contains(KeyModifiers::CONTROL) {
                    sidebar_visible = !sidebar_visible;
                    continue;
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
                        if key.modifiers.contains(KeyModifiers::SHIFT) {
                            slash_idx = slash_idx.saturating_sub(1);
                        } else {
                            slash_idx = (slash_idx + 1) % slash_count;
                        }
                    }
                    KeyCode::BackTab if slash_active => {
                        slash_idx = if slash_idx == 0 {
                            slash_count - 1
                        } else {
                            slash_idx - 1
                        };
                    }
                    KeyCode::Enter if slash_active => {
                        if let Some(completion) = slash_completion(&buffer, slash_idx, &state) {
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
                        if key.modifiers.contains(KeyModifiers::SHIFT) {
                            at_idx = at_idx.saturating_sub(1);
                        } else {
                            at_idx = (at_idx + 1) % at_count;
                        }
                    }
                    KeyCode::BackTab if at_active => {
                        at_idx = if at_idx == 0 {
                            at_count - 1
                        } else {
                            at_idx - 1
                        };
                    }
                    KeyCode::Enter if at_active => {
                        if let Some(completion) = at_completion(&buffer, at_idx, &state) {
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
                            state.cards.push(SceneCard {
                                kind: "user".to_string(),
                                body: Some(text),
                                ts: Some(chrono::Local::now().timestamp()),
                                ..Default::default()
                            });
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
                let term_size = terminal.size().ok();
                let term_rect =
                    term_size.map(|s| ratatui::layout::Rect::new(0, 0, s.width, s.height));
                match m.kind {
                    MouseEventKind::Down(MouseButton::Left) => {
                        if let Some(rect) = term_rect {
                            let layout = cards_layout(rect, &state, scroll_offset, sidebar_visible);
                            if layout.contains_screen(m.column, m.row) {
                                let (col, virt_y) = layout.project_clamped(m.column, m.row);
                                selection = Some(Selection::new(col, virt_y));
                                dragging = true;
                            } else {
                                selection = None;
                                dragging = false;
                            }
                        }
                    }
                    MouseEventKind::Drag(MouseButton::Left) if dragging => {
                        if let Some(rect) = term_rect {
                            let layout = cards_layout(rect, &state, scroll_offset, sidebar_visible);
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
                    }
                    MouseEventKind::Up(MouseButton::Left) => {
                        dragging = false;
                        if let Some(sel) = selection {
                            if !sel.is_empty() {
                                if let Ok(size) = terminal.size() {
                                    let rect =
                                        ratatui::layout::Rect::new(0, 0, size.width, size.height);
                                    let text = extract_text(
                                        &state,
                                        scroll_offset,
                                        rect,
                                        sel,
                                        sidebar_visible,
                                    );
                                    if !text.is_empty() {
                                        if let Ok(mut cb) = arboard::Clipboard::new() {
                                            let _ = cb.set_text(text);
                                        }
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

fn run_stream_loop(terminal: &mut RenderTerminal) -> Result<()> {
    use crossterm::event::{DisableMouseCapture, EnableMouseCapture, MouseButton, MouseEventKind};
    use std::sync::mpsc;
    use std::thread;
    use std::time::Duration;

    let mouse_enabled = crossterm::execute!(io::stdout(), EnableMouseCapture).is_ok();

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

    let mut logged_first_frame = false;
    let mut last_size = terminal.size().ok();
    let mut current_state: Option<Payload> = None;
    let mut scroll_offset: u16 = 0;
    let mut selection: Option<Selection> = None;
    let mut dragging = false;
    let mut tick: u32 = 0;
    let tick_period = Duration::from_millis(80);
    let scroll_step: u16 = 3;
    let mut frame_cache = FrameCache::default();

    let result: Result<()> = (|| loop {
        let mut state_changed = false;
        let mut stdin_closed = false;
        loop {
            match rx.try_recv() {
                Ok(line) => {
                    if line.trim().is_empty() {
                        continue;
                    }
                    if !frame_cache.is_new(&line) {
                        continue;
                    }
                    if let Ok(p) = decode_message(&line) {
                        current_state = Some(p);
                        state_changed = true;
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
            state_changed = true;
        }

        if state_changed {
            if let Some(payload) = &current_state {
                draw_atomic_with_ui(
                    terminal,
                    payload,
                    &mut logged_first_frame,
                    scroll_offset,
                    selection,
                    tick,
                )?;
            }
        }

        if stdin_closed && current_state.is_none() {
            return Ok(());
        }

        if crossterm::event::poll(tick_period)? {
            let evt = crossterm::event::read()?;
            let mut dirty = false;
            if let Event::Mouse(m) = evt {
                if let Some(Payload::Trace(state)) = current_state.as_ref() {
                    if let Ok(size) = terminal.size() {
                        let rect = ratatui::layout::Rect::new(0, 0, size.width, size.height);
                        let layout = cards_layout(rect, state, scroll_offset, true);
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
                                dirty = true;
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
                                dirty = true;
                            }
                            MouseEventKind::Up(MouseButton::Left) => {
                                dragging = false;
                                if let Some(sel) = selection {
                                    if !sel.is_empty() {
                                        let text =
                                            extract_text(state, scroll_offset, rect, sel, true);
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
                                dirty = true;
                            }
                            MouseEventKind::ScrollDown => {
                                scroll_offset = scroll_offset.saturating_sub(scroll_step);
                                dirty = true;
                            }
                            _ => {}
                        }
                    }
                }
            }
            if dirty {
                if let Some(payload) = &current_state {
                    draw_atomic_with_ui(
                        terminal,
                        payload,
                        &mut logged_first_frame,
                        scroll_offset,
                        selection,
                        tick,
                    )?;
                }
            }
        } else if let Some(payload) = &current_state {
            tick = tick.wrapping_add(1);
            draw_atomic_with_ui(
                terminal,
                payload,
                &mut logged_first_frame,
                scroll_offset,
                selection,
                tick,
            )?;
        }
    })();

    if mouse_enabled {
        let _ = crossterm::execute!(io::stdout(), DisableMouseCapture);
    }
    result
}

fn draw_atomic_with_ui(
    terminal: &mut RenderTerminal,
    payload: &Payload,
    logged_first_frame: &mut bool,
    scroll_offset: u16,
    selection: Option<Selection>,
    tick: u32,
) -> Result<()> {
    let _ = crossterm::execute!(
        terminal.backend_mut(),
        crossterm::terminal::BeginSynchronizedUpdate
    );
    let draw_err = terminal
        .draw(|f| {
            let area = f.area();
            if !*logged_first_frame {
                debug_log(&format!(
                    "first frame area = x={} y={} w={} h={}",
                    area.x, area.y, area.width, area.height
                ));
                *logged_first_frame = true;
            }
            match payload {
                Payload::Trace(state) => {
                    f.render_widget(
                        WholeScreen::new(state)
                            .with_scroll(scroll_offset)
                            .with_selection(selection)
                            .with_tick(tick),
                        area,
                    );
                }
                Payload::Setup(state) => render_setup(state, f),
            }
        })
        .err();
    let _ = crossterm::execute!(
        terminal.backend_mut(),
        crossterm::terminal::EndSynchronizedUpdate
    );
    if let Some(e) = draw_err {
        return Err(e).context("terminal draw");
    }
    Ok(())
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
