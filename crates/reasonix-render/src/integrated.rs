use std::io::{self, BufWriter, Write};
use std::sync::mpsc;
use std::thread;
use std::time::{Duration, Instant};

use anyhow::{Context, Result};
use crossterm::event::{
    self, DisableBracketedPaste, DisableMouseCapture, EnableBracketedPaste, EnableMouseCapture,
    Event, KeyCode, KeyEventKind, KeyModifiers, MouseButton, MouseEventKind,
};
use ratatui::backend::CrosstermBackend;
use ratatui::layout::Rect;

use crate::editor::{
    char_to_byte, cursor_on_first_line, cursor_on_last_line, insert_char_at, move_cursor_line,
    next_word_boundary, prev_word_boundary, remove_char_at,
};
use crate::input::is_quit;
use crate::state::{decode_message, Payload, SceneState};
use crate::view::render_setup;
use crate::whole_screen::{
    at_completion, at_match_count, cards_layout, extract_text, slash_arg_completion,
    slash_arg_match_count, slash_completion, slash_is_exact, slash_match_count, Selection,
    WholeScreen,
};

type Terminal = ratatui::Terminal<CrosstermBackend<BufWriter<io::Stdout>>>;

enum Evt {
    Scene(String),
    SceneClosed,
    Term(Event),
    TermClosed,
}

/// Run the integrated event loop. `scene_rx` carries newline-delimited JSON
/// scene messages (Node → Rust via NAPI `Renderer::emit`). The loop exits
/// when the user quits, the channel disconnects, or a fatal terminal error
/// occurs.
pub fn run_integrated_loop(
    terminal: &mut Terminal,
    scene_rx: mpsc::Receiver<String>,
) -> Result<()> {
    let mut stdout = io::stdout();
    let mouse_enabled = crossterm::execute!(stdout, EnableMouseCapture).is_ok();
    let paste_enabled = crossterm::execute!(stdout, EnableBracketedPaste).is_ok();

    let (tx, rx) = mpsc::channel::<Evt>();
    let tx_scene = tx.clone();
    let _scene_reader = thread::spawn(move || loop {
        match scene_rx.recv() {
            Ok(line) => {
                if tx_scene.send(Evt::Scene(line)).is_err() {
                    return;
                }
            }
            Err(_) => {
                let _ = tx_scene.send(Evt::SceneClosed);
                return;
            }
        }
    });
    let tx_term = tx.clone();
    let _term_reader = thread::spawn(move || loop {
        match event::read() {
            Ok(e) => {
                if tx_term.send(Evt::Term(e)).is_err() {
                    return;
                }
            }
            Err(_) => {
                let _ = tx_term.send(Evt::TermClosed);
                return;
            }
        }
    });
    drop(tx);

    let mut scene = SceneState::default();
    let mut have_state = false;
    let mut setup_pending: Option<crate::state::SetupState> = None;
    let mut buffer = String::new();
    let mut cursor: usize = 0;
    let mut scroll_offset: u16 = 0;
    let mut selection: Option<Selection> = None;
    let mut dragging = false;
    let mut scrollbar_drag: Option<i32> = None;
    let mut slash_idx: usize = 0;
    let mut slash_arg_idx: usize = 0;
    let mut at_idx: usize = 0;
    let mut history_cursor: i32 = -1;
    let mut approval_idx: usize = 0;
    let mut last_approval_signature: Option<String> = None;
    let mut mode_picker: Option<usize> = None;
    let mut preset_picker: Option<usize> = None;
    let mut list_picker_idx: usize = 0;
    let mut last_list_picker_id: Option<String> = None;
    let mut sidebar_visible = true;
    let mut tick: u32 = 0;
    let anim_interval = Duration::from_millis(80);
    let mut last_anim_at = Instant::now();
    let mut dirty = true;
    let scroll_step: u16 = 3;
    let page_step: u16 = 10;
    let mut last_size = terminal.size().ok();
    let mut should_exit = false;
    let mut pending_term: Option<Event> = None;
    let mut prev_emitted_buffer: String = String::new();
    // Suppress unused warnings on `have_state` — it's the historical
    // first-frame marker; kept so future "render placeholder until first
    // scene frame lands" logic can re-attach without re-plumbing.
    let _ = have_state;
    // Rust-owned buffer for the Ink Setup screen. In integrated mode Node's
    // Setup gets a null keystroke reader (rust owns the terminal), so the
    // dots / submission have to live here and Node only learns the final
    // text via the `setup-submit` event below.
    let mut setup_buffer: String = String::new();
    // Timestamp of the previous key event — used to detect paste-as-
    // keystrokes when the terminal doesn't speak bracketed paste (Apple
    // Terminal, cmd.exe pre-ConPTY, some tmux configs). Pasted chars
    // arrive at <2ms intervals; humans top out around 80ms, and even
    // key-repeat is usually ≥30ms. A <15ms gap on Enter is treated as
    // "newline inside a paste", not "submit".
    let mut last_key_at: Option<Instant> = None;

    let result: Result<()> = (|| loop {
        while let Ok(evt) = rx.try_recv() {
            match evt {
                Evt::Scene(line) => {
                    if line.trim().is_empty() {
                        continue;
                    }
                    if let Ok(p) = decode_message(&line) {
                        match p {
                            Payload::Trace(s) => {
                                scene = s;
                                have_state = true;
                                setup_pending = None;
                                reset_approval_if_changed(
                                    &scene,
                                    &mut last_approval_signature,
                                    &mut approval_idx,
                                );
                            }
                            Payload::Setup(s) => {
                                // Fresh setup screen — start with an empty
                                // buffer. Re-renders (e.g. error after a
                                // rejected key) come back as Setup payloads
                                // too, but by then we've already cleared the
                                // buffer on submit, so the no-reset branch
                                // is a no-op anyway.
                                if setup_pending.is_none() {
                                    setup_buffer.clear();
                                }
                                setup_pending = Some(s);
                            }
                        }
                        dirty = true;
                    }
                }
                Evt::SceneClosed => {
                    should_exit = true;
                    break;
                }
                Evt::Term(e) => {
                    pending_term = Some(e);
                    break;
                }
                Evt::TermClosed => {
                    should_exit = true;
                    break;
                }
            }
        }

        let current_size = terminal.size().ok();
        if current_size != last_size {
            terminal.clear().ok();
            last_size = current_size;
            dirty = true;
        }

        let buf_chars = buffer.chars().count();
        if cursor > buf_chars {
            cursor = buf_chars;
        }
        let slash_count = slash_match_count(&buffer, &scene);
        if slash_count == 0 {
            slash_idx = 0;
        } else if slash_idx >= slash_count {
            slash_idx = slash_count - 1;
        }
        let slash_arg_count = if slash_count > 0 {
            0
        } else {
            slash_arg_match_count(&buffer, &scene)
        };
        if slash_arg_count == 0 {
            slash_arg_idx = 0;
        } else if slash_arg_idx >= slash_arg_count {
            slash_arg_idx = slash_arg_count - 1;
        }
        let at_count = if slash_count > 0 || slash_arg_count > 0 {
            0
        } else {
            at_match_count(&buffer, &scene)
        };
        if at_count == 0 {
            at_idx = 0;
        } else if at_idx >= at_count {
            at_idx = at_count - 1;
        }
        // Reset list-picker selection when a new picker shows up (id
        // changes) so the cursor doesn't carry over from the previous
        // session into a totally different option set.
        let lp_id = scene.list_picker.as_ref().map(|p| p.id.clone());
        if lp_id != last_list_picker_id {
            list_picker_idx = 0;
            last_list_picker_id = lp_id.clone();
        }
        let list_picker_len = scene
            .list_picker
            .as_ref()
            .map(|p| p.options.len())
            .unwrap_or(0);
        if list_picker_len == 0 {
            list_picker_idx = 0;
        } else if list_picker_idx >= list_picker_len {
            list_picker_idx = list_picker_len - 1;
        }

        if last_anim_at.elapsed() >= anim_interval {
            last_anim_at = Instant::now();
            tick = tick.wrapping_add(1);
            if needs_animation(&scene, setup_pending.is_some(), selection.is_some()) {
                dirty = true;
            }
        }

        // While a prompt-input is active the composer buffer is a
        // private text answer (e.g. an API secret), not a chat draft.
        // Don't echo it back to Node — handle_prompt_input_key emits a
        // single `prompt-response` on Enter instead.
        if scene.prompt_input.is_none() && buffer != prev_emitted_buffer {
            emit_event(serde_json::json!({
                "event": "composer",
                "text": buffer.clone(),
            }));
            prev_emitted_buffer = buffer.clone();
        }

        if dirty {
            let _ = crossterm::execute!(
                terminal.backend_mut(),
                crossterm::terminal::BeginSynchronizedUpdate
            );
            if let Some(setup) = setup_pending.as_ref() {
                // Render the local buffer length, not the value Node sent —
                // Node's Setup buffer always stays empty in integrated mode
                // because its keystroke reader is null. Without this
                // override the user types but sees no dots.
                let mut display = setup.clone();
                display.buffer_length = setup_buffer.chars().count();
                terminal
                    .draw(|f| render_setup(&display, f))
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
                                .with_slash_arg_index(slash_arg_idx)
                                .with_at_index(at_idx)
                                .with_approval_index(approval_idx)
                                .with_mode_picker(mode_picker)
                                .with_preset_picker(preset_picker)
                                .with_list_picker_index(list_picker_idx)
                                .with_sidebar_visible(sidebar_visible)
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
            dirty = false;
        }

        if should_exit {
            return Ok(());
        }

        let evt = if let Some(e) = pending_term.take() {
            e
        } else {
            let wait = anim_interval
                .saturating_sub(last_anim_at.elapsed())
                .max(Duration::from_millis(1));
            match rx.recv_timeout(wait) {
                Ok(Evt::Term(e)) => e,
                Ok(Evt::Scene(line)) => {
                    if !line.trim().is_empty() {
                        if let Ok(p) = decode_message(&line) {
                            match p {
                                Payload::Trace(s) => {
                                    scene = s;
                                    have_state = true;
                                    setup_pending = None;
                                    reset_approval_if_changed(
                                        &scene,
                                        &mut last_approval_signature,
                                        &mut approval_idx,
                                    );
                                }
                                Payload::Setup(s) => {
                                    if setup_pending.is_none() {
                                        setup_buffer.clear();
                                    }
                                    setup_pending = Some(s);
                                }
                            }
                            dirty = true;
                        }
                    }
                    continue;
                }
                Ok(Evt::SceneClosed) => return Ok(()),
                Ok(Evt::TermClosed) => return Ok(()),
                Err(mpsc::RecvTimeoutError::Timeout) => continue,
                Err(mpsc::RecvTimeoutError::Disconnected) => return Ok(()),
            }
        };
        if setup_pending.is_some() {
            // Setup screen — Node's MaskedInput can't read keys (integrated
            // mode hands stdin to rust), so handle them here and forward the
            // final string back as `setup-submit` for Setup.handleSubmit.
            match &evt {
                Event::Key(key) if key.kind == KeyEventKind::Press => {
                    if is_quit(key) {
                        emit_event(serde_json::json!({"event": "exit"}));
                        return Ok(());
                    }
                    if key.code == KeyCode::Char('d')
                        && key.modifiers.contains(KeyModifiers::CONTROL)
                    {
                        emit_event(serde_json::json!({"event": "exit"}));
                        return Ok(());
                    }
                    match key.code {
                        KeyCode::Enter => {
                            let text = std::mem::take(&mut setup_buffer);
                            emit_event(serde_json::json!({
                                "event": "setup-submit",
                                "text": text,
                            }));
                        }
                        KeyCode::Backspace => {
                            // pop one char by char-boundary, not byte.
                            if let Some((idx, _)) = setup_buffer.char_indices().next_back() {
                                setup_buffer.truncate(idx);
                            }
                        }
                        KeyCode::Char(c) if !key.modifiers.contains(KeyModifiers::CONTROL) => {
                            setup_buffer.push(c);
                        }
                        _ => {}
                    }
                }
                Event::Paste(text) => {
                    // API keys often come with a trailing newline from copy —
                    // strip all control chars so the key validates cleanly.
                    for ch in text.chars() {
                        if !ch.is_control() {
                            setup_buffer.push(ch);
                        }
                    }
                }
                _ => {}
            }
            dirty = true;
            continue;
        }
        dirty = true;

        match evt {
            Event::Key(key) if key.kind != KeyEventKind::Press => continue,
            Event::Key(key) => {
                if is_quit(&key) {
                    if let Some(sel) = selection {
                        if let Ok(size) = terminal.size() {
                            let rect = Rect::new(0, 0, size.width, size.height);
                            let text =
                                extract_text(&scene, scroll_offset, rect, sel, sidebar_visible);
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
                if key.code == KeyCode::Char('d') && key.modifiers.contains(KeyModifiers::CONTROL) {
                    emit_event(serde_json::json!({"event": "exit"}));
                    return Ok(());
                }
                if key.code == KeyCode::Char('b') && key.modifiers.contains(KeyModifiers::CONTROL) {
                    sidebar_visible = !sidebar_visible;
                    continue;
                }
                let now = Instant::now();
                let since_last_key = last_key_at.map(|t| now.duration_since(t));
                last_key_at = Some(now);
                let paste_burst = since_last_key
                    .map(|d| d < Duration::from_millis(15))
                    .unwrap_or(false);
                if mode_picker.is_some() || preset_picker.is_some() {
                    handle_mode_picker_key(&key, &mut mode_picker, &mut preset_picker);
                    continue;
                }
                if let Some(picker) = scene.list_picker.as_ref() {
                    handle_list_picker_key(&key, picker, &mut list_picker_idx);
                    continue;
                }
                if let Some(prompt) = scene.prompt_input.as_ref() {
                    // Paste-as-keystrokes safeguard: a mid-burst Enter is
                    // part of the pasted content, not the user submitting.
                    // prompt_input is single-line so we can't insert '\n';
                    // dropping the Enter lets the rest of the paste finish.
                    if paste_burst
                        && key.code == KeyCode::Enter
                        && !key.modifiers.contains(KeyModifiers::SHIFT)
                    {
                        continue;
                    }
                    handle_prompt_input_key(&key, prompt, &mut buffer, &mut cursor);
                    continue;
                }
                if let Some(approval) = scene.approval.as_ref() {
                    approval_log(&format!(
                        "key {:?} (modifiers {:?}) approval={}",
                        key.code,
                        key.modifiers,
                        approval_kind_label(approval)
                    ));
                    handle_approval_key(&key, approval, &mut approval_idx);
                    continue;
                }
                let slash_active = slash_count > 0;
                let slash_arg_active = !slash_active && slash_arg_count > 0;
                let at_active = !slash_active && !slash_arg_active && at_count > 0;
                let slash_complete_only = slash_active && !slash_is_exact(&buffer, &scene);
                match key.code {
                    // Paste-as-keystrokes fallback: when bracketed paste is
                    // off, an Enter in the middle of a burst is part of the
                    // pasted content, not a submit. Wins over the picker
                    // Enter arms because guard arms match top-down.
                    KeyCode::Enter
                        if paste_burst && !key.modifiers.contains(KeyModifiers::SHIFT) =>
                    {
                        selection = None;
                        insert_char_at(&mut buffer, cursor, '\n');
                        cursor += 1;
                        slash_idx = 0;
                        at_idx = 0;
                        scroll_offset = 0;
                    }
                    KeyCode::Char('v') if key.modifiers.contains(KeyModifiers::CONTROL) => {
                        // Explicit clipboard paste — the only path that works
                        // on terminals without bracketed paste (Apple Terminal
                        // default). Bypasses the burst heuristic above.
                        if let Ok(mut cb) = arboard::Clipboard::new() {
                            if let Ok(text) = cb.get_text() {
                                selection = None;
                                for ch in text.chars() {
                                    if ch == '\r' {
                                        continue;
                                    }
                                    insert_char_at(&mut buffer, cursor, ch);
                                    cursor += 1;
                                }
                                slash_idx = 0;
                                at_idx = 0;
                                scroll_offset = 0;
                            }
                        }
                    }
                    KeyCode::Up if slash_active => {
                        slash_idx = slash_idx.saturating_sub(1);
                    }
                    KeyCode::Down if slash_active => {
                        slash_idx = (slash_idx + 1).min(slash_count - 1);
                    }
                    KeyCode::Tab if slash_active => {
                        if let Some(completion) = slash_completion(&buffer, slash_idx, &scene) {
                            buffer = completion.trim_end().to_string();
                            cursor = buffer.chars().count();
                        }
                    }
                    KeyCode::BackTab if slash_active => {
                        slash_idx = if slash_idx == 0 {
                            slash_count - 1
                        } else {
                            slash_idx - 1
                        };
                    }
                    KeyCode::Enter if slash_complete_only => {
                        if let Some(completion) = slash_completion(&buffer, slash_idx, &scene) {
                            cursor = completion.chars().count();
                            buffer = completion;
                        }
                    }
                    KeyCode::Up if slash_arg_active => {
                        slash_arg_idx = slash_arg_idx.saturating_sub(1);
                    }
                    KeyCode::Down if slash_arg_active => {
                        slash_arg_idx = (slash_arg_idx + 1).min(slash_arg_count - 1);
                    }
                    KeyCode::Tab if slash_arg_active => {
                        if let Some(completion) =
                            slash_arg_completion(&buffer, slash_arg_idx, &scene)
                        {
                            cursor = completion.chars().count();
                            buffer = completion;
                        }
                    }
                    KeyCode::BackTab if slash_arg_active => {
                        slash_arg_idx = if slash_arg_idx == 0 {
                            slash_arg_count - 1
                        } else {
                            slash_arg_idx - 1
                        };
                    }
                    KeyCode::Enter if slash_arg_active => {
                        if let Some(completion) =
                            slash_arg_completion(&buffer, slash_arg_idx, &scene)
                        {
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
                        if let Some(completion) = at_completion(&buffer, at_idx, &scene) {
                            cursor = completion.chars().count();
                            buffer = completion;
                        }
                    }
                    KeyCode::BackTab if at_active => {
                        at_idx = if at_idx == 0 {
                            at_count - 1
                        } else {
                            at_idx - 1
                        };
                    }
                    KeyCode::BackTab => {
                        let next = match scene.edit_mode {
                            Some(crate::state::EditMode::Review) => "auto",
                            Some(crate::state::EditMode::Auto) => "yolo",
                            Some(crate::state::EditMode::Yolo) | None => "review",
                        };
                        emit_event(serde_json::json!({"event": "mode-set", "value": next}));
                    }
                    KeyCode::Enter if at_active => {
                        if let Some(completion) = at_completion(&buffer, at_idx, &scene) {
                            cursor = completion.chars().count();
                            buffer = completion;
                        }
                    }
                    KeyCode::Up => {
                        if cursor_on_first_line(&buffer, cursor) {
                            if let Some(text) = history_prev(&scene, &mut history_cursor) {
                                buffer = text;
                                cursor = buffer.chars().count();
                                scroll_offset = 0;
                            }
                        } else {
                            cursor = move_cursor_line(&buffer, cursor, -1);
                        }
                    }
                    KeyCode::Down => {
                        if cursor_on_last_line(&buffer, cursor) {
                            if let Some(text) = history_next(&scene, &mut history_cursor) {
                                cursor = text.chars().count();
                                buffer = text;
                                scroll_offset = 0;
                            }
                        } else {
                            cursor = move_cursor_line(&buffer, cursor, 1);
                        }
                    }
                    KeyCode::Esc => {
                        if selection.is_some() {
                            selection = None;
                        } else {
                            buffer.clear();
                            cursor = 0;
                            slash_idx = 0;
                            at_idx = 0;
                            history_cursor = -1;
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
                        history_cursor = -1;
                    }
                    _ => {}
                }
            }
            Event::Mouse(m) => {
                if let Ok(size) = terminal.size() {
                    let rect = Rect::new(0, 0, size.width, size.height);
                    let layout = cards_layout(rect, &scene, scroll_offset, sidebar_visible);
                    let scrollbar = layout.scrollbar(scroll_offset);
                    match m.kind {
                        MouseEventKind::Down(MouseButton::Left) => {
                            if let Some(hit) = pill_hit_test(m.column, m.row, &scene, size) {
                                match hit {
                                    "mode-cycle" => {
                                        preset_picker = None;
                                        mode_picker = Some(current_mode_idx(&scene));
                                    }
                                    "preset-cycle" => {
                                        mode_picker = None;
                                        preset_picker = Some(current_preset_idx(&scene));
                                    }
                                    _ => {}
                                }
                                continue;
                            }
                            if let Some(sb) = scrollbar {
                                if sb.contains(m.column, m.row) {
                                    selection = None;
                                    dragging = false;
                                    let anchor = if sb.thumb_contains(m.row) {
                                        i32::from(m.row) - i32::from(sb.thumb_top)
                                    } else {
                                        i32::from(sb.thumb_size / 2)
                                    };
                                    scrollbar_drag = Some(anchor);
                                    let mouse_rel = i32::from(m.row) - i32::from(sb.track_top);
                                    let new_top_rel = (mouse_rel - anchor)
                                        .clamp(0, i32::from(sb.track_space))
                                        as u16;
                                    scroll_offset = sb.offset_for_thumb_top_rel(new_top_rel);
                                    continue;
                                }
                            }
                            if layout.contains_screen(m.column, m.row) {
                                let (col, virt_y) = layout.project_clamped(m.column, m.row);
                                selection = Some(Selection::new(col, virt_y));
                                dragging = true;
                            } else {
                                selection = None;
                                dragging = false;
                            }
                        }
                        MouseEventKind::Drag(MouseButton::Left) if scrollbar_drag.is_some() => {
                            let anchor = scrollbar_drag.unwrap_or(0);
                            if let Some(sb) = scrollbar {
                                let mouse_rel = i32::from(m.row) - i32::from(sb.track_top);
                                let new_top_rel =
                                    (mouse_rel - anchor).clamp(0, i32::from(sb.track_space)) as u16;
                                scroll_offset = sb.offset_for_thumb_top_rel(new_top_rel);
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
                            scrollbar_drag = None;
                            if let Some(sel) = selection {
                                if !sel.is_empty() {
                                    let text = extract_text(
                                        &scene,
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
            Event::Paste(text) => {
                if let Some(prompt) = scene.prompt_input.as_ref() {
                    if prompt.secret {
                        // Don't accept multi-line / control chars into a
                        // secret prompt — pasted secrets in a non-line
                        // editor are messy. Strip newlines and append.
                        let clean: String = text.chars().filter(|c| !c.is_control()).collect();
                        for ch in clean.chars() {
                            insert_char_at(&mut buffer, cursor, ch);
                            cursor += 1;
                        }
                    } else {
                        // Strip leading/trailing whitespace + collapse
                        // newlines to a single line — same shape an
                        // interactive setup prompt would expect.
                        let line = text.lines().next().unwrap_or("").trim();
                        for ch in line.chars() {
                            insert_char_at(&mut buffer, cursor, ch);
                            cursor += 1;
                        }
                    }
                } else {
                    selection = None;
                    for ch in text.chars() {
                        if ch == '\r' {
                            continue;
                        }
                        insert_char_at(&mut buffer, cursor, ch);
                        cursor += 1;
                    }
                    slash_idx = 0;
                    at_idx = 0;
                    scroll_offset = 0;
                }
            }
            Event::Resize(_, _) => {}
            _ => {}
        }
    })();

    if mouse_enabled {
        let _ = crossterm::execute!(io::stdout(), DisableMouseCapture);
    }
    if paste_enabled {
        let _ = crossterm::execute!(io::stdout(), DisableBracketedPaste);
    }
    result
}

fn needs_animation(scene: &SceneState, setup_pending: bool, has_selection: bool) -> bool {
    if setup_pending || has_selection {
        return false;
    }
    // Only redraw on the anim tick when something is actually moving
    // (spinner / streaming reveal). Idle scenes leave the frame buffer
    // alone so IME composition isn't repeatedly overwritten by sync-update
    // bursts. User keypresses still mark dirty via the event branch below.
    scene.busy
}

fn history_prev(scene: &SceneState, cursor: &mut i32) -> Option<String> {
    let hist = scene.prompt_history.as_ref()?;
    if hist.is_empty() {
        return None;
    }
    let next = (*cursor + 1).min(hist.len() as i32 - 1);
    *cursor = next;
    hist.get(hist.len() - 1 - next as usize).cloned()
}

fn history_next(scene: &SceneState, cursor: &mut i32) -> Option<String> {
    if *cursor < 0 {
        return None;
    }
    let hist = scene.prompt_history.as_ref()?;
    let next = *cursor - 1;
    *cursor = next;
    if next < 0 {
        return Some(String::new());
    }
    hist.get(hist.len() - 1 - next as usize).cloned()
}

fn emit_event(event: serde_json::Value) {
    // NAPI path: in-process JS callback installed by `create_renderer`.
    // Falls back to stderr (one-line JSON) for standalone debug / replay
    // runs that don't go through the renderer (the napi feature is off
    // for `cargo test` / clippy / the standalone bin).
    #[cfg(feature = "napi")]
    {
        if let Some(emitter) = crate::napi_api::active_emitter() {
            emitter.emit(&event);
            if let Ok(s) = serde_json::to_string(&event) {
                approval_log(&format!("emit {s}"));
            }
            return;
        }
    }
    if let Ok(s) = serde_json::to_string(&event) {
        approval_log(&format!("emit {s}"));
        let mut out = io::stderr().lock();
        let _ = writeln!(out, "{s}");
        let _ = out.flush();
    }
}

fn approval_log(msg: &str) {
    let home = std::env::var("USERPROFILE")
        .or_else(|_| std::env::var("HOME"))
        .unwrap_or_default();
    if home.is_empty() {
        return;
    }
    let path = format!("{home}/.reasonix/approval-trace.log");
    if let Some(parent) = std::path::Path::new(&path).parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    if let Ok(mut f) = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&path)
    {
        let now = chrono::Local::now().format("%H:%M:%S%.3f");
        let _ = writeln!(f, "[{now}] {msg}");
    }
}

const MODE_VALUES: [&str; 3] = ["review", "auto", "yolo"];
const PRESET_VALUES: [&str; 3] = ["auto", "flash", "pro"];

fn current_mode_idx(scene: &SceneState) -> usize {
    match scene.edit_mode {
        Some(crate::state::EditMode::Review) => 0,
        Some(crate::state::EditMode::Auto) => 1,
        Some(crate::state::EditMode::Yolo) => 2,
        None => 0,
    }
}

fn current_preset_idx(scene: &SceneState) -> usize {
    match scene.preset.as_deref() {
        Some("auto") => 0,
        Some("flash") => 1,
        Some("pro") => 2,
        _ => 0,
    }
}

fn handle_list_picker_key(
    key: &crossterm::event::KeyEvent,
    picker: &crate::state::ListPicker,
    selected: &mut usize,
) {
    let n = picker.options.len();
    if n == 0 {
        if matches!(key.code, KeyCode::Esc) {
            emit_event(serde_json::json!({
                "event": "list-picker-response",
                "id": picker.id,
                "cancelled": true,
            }));
        }
        return;
    }
    match key.code {
        KeyCode::Up | KeyCode::Char('k') => {
            *selected = if *selected == 0 { n - 1 } else { *selected - 1 };
        }
        KeyCode::Down | KeyCode::Char('j') => {
            *selected = (*selected + 1) % n;
        }
        KeyCode::Char(d) if d.is_ascii_digit() => {
            let pick = d.to_digit(10).unwrap_or(0) as usize;
            if pick >= 1 && pick <= n {
                *selected = pick - 1;
            }
        }
        KeyCode::Enter => {
            let chosen = (*selected).min(n - 1);
            emit_event(serde_json::json!({
                "event": "list-picker-response",
                "id": picker.id,
                "key": picker.options[chosen].key,
            }));
        }
        KeyCode::Esc => {
            emit_event(serde_json::json!({
                "event": "list-picker-response",
                "id": picker.id,
                "cancelled": true,
            }));
        }
        _ => {}
    }
}

fn handle_prompt_input_key(
    key: &crossterm::event::KeyEvent,
    prompt: &crate::state::PromptInput,
    buffer: &mut String,
    cursor: &mut usize,
) {
    match key.code {
        KeyCode::Enter => {
            let answer = if buffer.is_empty() {
                prompt.default_value.clone().unwrap_or_default()
            } else {
                std::mem::take(buffer)
            };
            *cursor = 0;
            emit_event(serde_json::json!({
                "event": "prompt-response",
                "id": prompt.id,
                "text": answer,
            }));
        }
        KeyCode::Esc => {
            buffer.clear();
            *cursor = 0;
            emit_event(serde_json::json!({
                "event": "prompt-response",
                "id": prompt.id,
                "cancelled": true,
            }));
        }
        KeyCode::Backspace if *cursor > 0 => {
            remove_char_at(buffer, *cursor - 1);
            *cursor -= 1;
        }
        KeyCode::Left => {
            *cursor = cursor.saturating_sub(1);
        }
        KeyCode::Right => {
            *cursor = (*cursor + 1).min(buffer.chars().count());
        }
        KeyCode::Home => *cursor = 0,
        KeyCode::End => *cursor = buffer.chars().count(),
        KeyCode::Char('v') if key.modifiers.contains(KeyModifiers::CONTROL) => {
            // Explicit clipboard paste — same shape as the Event::Paste
            // path: secrets strip every control char (API keys often have a
            // trailing newline), non-secrets keep the first line only since
            // prompt_input is single-line by design.
            if let Ok(mut cb) = arboard::Clipboard::new() {
                if let Ok(text) = cb.get_text() {
                    if prompt.secret {
                        for ch in text.chars().filter(|c| !c.is_control()) {
                            insert_char_at(buffer, *cursor, ch);
                            *cursor += 1;
                        }
                    } else {
                        let first = text.lines().next().unwrap_or("").trim();
                        for ch in first.chars() {
                            insert_char_at(buffer, *cursor, ch);
                            *cursor += 1;
                        }
                    }
                }
            }
        }
        KeyCode::Char(c) if !key.modifiers.contains(KeyModifiers::CONTROL) => {
            insert_char_at(buffer, *cursor, c);
            *cursor += 1;
        }
        _ => {}
    }
}

fn handle_mode_picker_key(
    key: &crossterm::event::KeyEvent,
    mode_picker: &mut Option<usize>,
    preset_picker: &mut Option<usize>,
) {
    if mode_picker.is_some() {
        if let Some(value) = cycle_or_pick(key, mode_picker, &MODE_VALUES) {
            emit_event(serde_json::json!({"event": "mode-set", "value": value}));
        }
        return;
    }
    if preset_picker.is_some() {
        if let Some(value) = cycle_or_pick(key, preset_picker, &PRESET_VALUES) {
            emit_event(serde_json::json!({"event": "preset-set", "value": value}));
        }
    }
}

/// Returns Some(value) when Enter was pressed and the picker should close. Mutates the index for navigation keys.
fn cycle_or_pick(
    key: &crossterm::event::KeyEvent,
    slot: &mut Option<usize>,
    values: &[&'static str],
) -> Option<&'static str> {
    let n = values.len();
    let idx = slot.as_mut()?;
    match key.code {
        KeyCode::Up | KeyCode::Char('k') => {
            *idx = if *idx == 0 { n - 1 } else { *idx - 1 };
            None
        }
        KeyCode::Down | KeyCode::Char('j') | KeyCode::Tab | KeyCode::BackTab => {
            *idx = (*idx + 1) % n;
            None
        }
        KeyCode::Char(d) if d.is_ascii_digit() => {
            let pick = d.to_digit(10).unwrap_or(0) as usize;
            if pick >= 1 && pick <= n {
                *idx = pick - 1;
            }
            None
        }
        KeyCode::Enter => {
            let value = values[*idx];
            *slot = None;
            Some(value)
        }
        KeyCode::Esc => {
            *slot = None;
            None
        }
        _ => None,
    }
}

fn pill_hit_test(
    col: u16,
    row: u16,
    scene: &SceneState,
    size: ratatui::layout::Size,
) -> Option<&'static str> {
    use unicode_width::UnicodeWidthStr;
    if size.height == 0 {
        return None;
    }
    let status_row = size.height - 1;
    if row != status_row {
        return None;
    }
    let mut c: u16 = 1;
    c += 2; // ● glyph + space
    c += "reasonix".width() as u16;
    c += 2; // gap

    if let Some(mode) = scene.edit_mode.as_ref() {
        let label = match mode {
            crate::state::EditMode::Review => "REVIEW",
            crate::state::EditMode::Auto => "AUTO",
            crate::state::EditMode::Yolo => "YOLO",
        };
        let pill = format!("◇ MODE {label}");
        let w = pill.width() as u16;
        if col >= c && col < c + w {
            return Some("mode-cycle");
        }
        c += w + 1;
    }
    if let Some(preset) = scene.preset.as_deref() {
        let value: String = preset.chars().flat_map(char::to_uppercase).collect();
        let pill = format!("◇ MODEL {value}");
        let w = pill.width() as u16;
        if col >= c && col < c + w {
            return Some("preset-cycle");
        }
    }
    None
}

fn approval_kind_label(approval: &crate::state::Approval) -> &'static str {
    use crate::state::Approval;
    match approval {
        Approval::Plan { .. } => "plan",
        Approval::Shell { .. } => "shell",
        Approval::Path { .. } => "path",
        Approval::Edit { .. } => "edit",
        Approval::Choice { .. } => "choice",
        Approval::Checkpoint { .. } => "checkpoint",
    }
}

fn approval_signature(scene: &SceneState) -> Option<String> {
    use crate::state::Approval;
    match scene.approval.as_ref()? {
        Approval::Plan { body, steps } => Some(format!("plan:{}:{}", body.len(), steps.len())),
        Approval::Shell { command, .. } => Some(format!("shell:{command}")),
        Approval::Path { path, intent, .. } => Some(format!("path:{intent}:{path}")),
        Approval::Edit {
            path,
            search,
            replace,
        } => Some(format!("edit:{path}:{}:{}", search.len(), replace.len())),
        Approval::Choice {
            question, options, ..
        } => Some(format!("choice:{}:{}", question.len(), options.len())),
        Approval::Checkpoint {
            completed, total, ..
        } => Some(format!("checkpoint:{completed}/{total}")),
    }
}

fn reset_approval_if_changed(
    scene: &SceneState,
    last_sig: &mut Option<String>,
    choice_idx: &mut usize,
) {
    let sig = approval_signature(scene);
    if sig != *last_sig {
        approval_log(&format!("approval state -> {sig:?}"));
        *choice_idx = 0;
        *last_sig = sig;
    }
}

fn handle_approval_key(
    key: &crossterm::event::KeyEvent,
    approval: &crate::state::Approval,
    choice_idx: &mut usize,
) {
    use crate::state::Approval;
    let kind = match approval {
        Approval::Plan { .. } => "plan",
        Approval::Shell { .. } => "shell",
        Approval::Path { .. } => "path",
        Approval::Edit { .. } => "edit",
        Approval::Choice { .. } => "choice",
        Approval::Checkpoint { .. } => "checkpoint",
    };
    let choice: Option<serde_json::Value> = match approval {
        Approval::Plan { .. } => match key.code {
            KeyCode::Enter => Some(serde_json::json!("approve")),
            KeyCode::Char('r') | KeyCode::Char('R') => Some(serde_json::json!("refine")),
            KeyCode::Char('v') | KeyCode::Char('V') => Some(serde_json::json!("revise")),
            KeyCode::Char('n') | KeyCode::Char('N') | KeyCode::Esc => {
                Some(serde_json::json!("cancel"))
            }
            _ => None,
        },
        Approval::Shell { .. } | Approval::Path { .. } => match key.code {
            KeyCode::Enter => Some(serde_json::json!("run_once")),
            KeyCode::Char('a') | KeyCode::Char('A') => Some(serde_json::json!("always_allow")),
            KeyCode::Char('n') | KeyCode::Char('N') | KeyCode::Esc => {
                Some(serde_json::json!("deny"))
            }
            _ => None,
        },
        Approval::Edit { .. } => match key.code {
            KeyCode::Enter => Some(serde_json::json!("apply")),
            KeyCode::Char('r') | KeyCode::Char('R') | KeyCode::Esc => {
                Some(serde_json::json!("reject"))
            }
            KeyCode::Char('t') | KeyCode::Char('T') => {
                Some(serde_json::json!("apply-rest-of-turn"))
            }
            KeyCode::Char('y') | KeyCode::Char('Y') => Some(serde_json::json!("flip-to-auto")),
            _ => None,
        },
        Approval::Checkpoint { .. } => match key.code {
            KeyCode::Enter => Some(serde_json::json!("continue")),
            KeyCode::Char('r') | KeyCode::Char('R') => Some(serde_json::json!("revise")),
            KeyCode::Char('s') | KeyCode::Char('S') | KeyCode::Esc => {
                Some(serde_json::json!("stop"))
            }
            _ => None,
        },
        Approval::Choice {
            options,
            allow_custom,
            ..
        } => {
            let n = options.len();
            if *choice_idx >= n && n > 0 {
                *choice_idx = n - 1;
            }
            match key.code {
                KeyCode::Esc => Some(serde_json::json!({"kind": "cancel"})),
                KeyCode::Up | KeyCode::Char('k') if n > 0 => {
                    *choice_idx = if *choice_idx == 0 {
                        n - 1
                    } else {
                        *choice_idx - 1
                    };
                    None
                }
                KeyCode::Down | KeyCode::Char('j') if n > 0 => {
                    *choice_idx = (*choice_idx + 1) % n;
                    None
                }
                KeyCode::Enter if n > 0 => {
                    Some(serde_json::json!({"kind": "pick", "optionId": options[*choice_idx].id}))
                }
                KeyCode::Char('c') | KeyCode::Char('C') if *allow_custom => {
                    Some(serde_json::json!({"kind": "custom"}))
                }
                KeyCode::Char(d) if d.is_ascii_digit() => {
                    let pick = d.to_digit(10).unwrap_or(0) as usize;
                    if pick >= 1 && pick <= n {
                        *choice_idx = pick - 1;
                        Some(serde_json::json!({"kind": "pick", "optionId": options[pick - 1].id}))
                    } else {
                        None
                    }
                }
                _ => None,
            }
        }
    };
    if let Some(c) = choice {
        emit_event(serde_json::json!({
            "event": "approval-response",
            "kind": kind,
            "choice": c
        }));
    }
}
