use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use crate::state::SceneState;

use super::paint::{paint, paint_str};
use super::theme::{
    BG, COMPOSER_PLACEHOLDER, DS_BRIGHT, DS_PURPLE, ERR, FG, FG1, FG2, FG3, INFO, OK, WARN,
};

pub fn render_dock(buf: &mut Buffer, area: Rect, state: &SceneState, tick: u32) {
    if area.height < 3 {
        return;
    }
    let inner_w = area.width;
    let box_h = area.height.saturating_sub(2).max(3);
    render_input_box(buf, area, box_h, state, tick);

    let mut row = area.y + box_h;
    if row < area.y + area.height {
        render_input_meta(buf, area, row, inner_w);
        row += 1;
    }
    if row < area.y + area.height {
        render_status_bar(buf, area, row, inner_w, state);
    }
}

fn render_input_box(buf: &mut Buffer, area: Rect, rows: u16, state: &SceneState, tick: u32) {
    let w = area.width;
    if w < 4 || rows < 3 {
        return;
    }
    let top = area.y;
    let bot = top + rows.saturating_sub(1);
    let right = area.x + w - 1;

    paint(buf, area.x, top, '╭', FG3, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, top, '─', FG3, BG, Modifier::empty());
    }
    paint(buf, right, top, '╮', FG3, BG, Modifier::empty());

    for y in (top + 1)..bot {
        paint(buf, area.x, y, '│', FG3, BG, Modifier::empty());
        paint(buf, right, y, '│', FG3, BG, Modifier::empty());
    }

    paint(buf, area.x, bot, '╰', FG3, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, bot, '─', FG3, BG, Modifier::empty());
    }
    paint(buf, right, bot, '╯', FG3, BG, Modifier::empty());

    let content_rows = rows.saturating_sub(2);
    let prompt_input = state.prompt_input.as_ref();
    let prompt_color = if prompt_input.is_some() {
        WARN
    } else {
        match state
            .composer_text
            .as_deref()
            .and_then(|s| s.chars().next())
        {
            Some('!') => OK,
            Some('/') => DS_BRIGHT,
            Some('@') => DS_PURPLE,
            _ => DS_BRIGHT,
        }
    };
    let raw_text = state.composer_text.as_deref().unwrap_or("");
    let masked_storage: String;
    let text: &str = if let Some(p) = prompt_input {
        if p.secret {
            masked_storage = "•".repeat(raw_text.chars().count());
            &masked_storage
        } else {
            raw_text
        }
    } else {
        raw_text
    };
    let placeholder: &str = if let Some(p) = prompt_input {
        &p.label
    } else {
        COMPOSER_PLACEHOLDER
    };
    let prompt_glyph: &str = if prompt_input.is_some() { "? " } else { "❯ " };
    // Caret stays solid — a blinking caret triggers a full-frame sync-update
    // ANSI burst every 480ms (BeginSynchronizedUpdate/EndSynchronizedUpdate),
    // which on Windows Terminal evicts the IME composition overlay drawn at
    // the cursor position and produces visible flicker while typing CJK.
    let _ = tick;
    let show_caret = true;
    let total_chars = text.chars().count();
    let cursor = state
        .composer_cursor
        .unwrap_or(total_chars)
        .min(total_chars);
    let col_start = area.x + 2;
    let text_start = col_start + 2;
    let inner_w = if right > text_start + 1 {
        (right - text_start - 1) as usize
    } else {
        0
    };

    let (visual_lines, cursor_visual_row, cursor_visual_col) =
        build_composer_visual_lines(text, cursor, inner_w);

    let visual_count = visual_lines.len();
    let scroll_off = (cursor_visual_row + 1).saturating_sub(content_rows as usize);
    let has_more_above = scroll_off > 0;
    let has_more_below = scroll_off + (content_rows as usize) < visual_count;

    for i in 0..content_rows {
        let y = top + 1 + i;
        if i == 0 {
            paint_str(
                buf,
                col_start,
                y,
                prompt_glyph,
                prompt_color,
                BG,
                Modifier::BOLD,
            );
        }
        let vidx = scroll_off + i as usize;
        if text.is_empty() && i == 0 {
            paint_str(buf, text_start, y, placeholder, FG3, BG, Modifier::empty());
            if show_caret {
                paint(buf, text_start, y, '▮', DS_BRIGHT, BG, Modifier::empty());
            }
            continue;
        }
        let Some(line) = visual_lines.get(vidx) else {
            continue;
        };
        if vidx == cursor_visual_row {
            let before: String = line.chars().take(cursor_visual_col).collect();
            let after: String = line.chars().skip(cursor_visual_col).collect();
            let after_col = paint_str(buf, text_start, y, &before, FG, BG, Modifier::empty());
            if show_caret {
                paint(buf, after_col, y, '▮', DS_BRIGHT, BG, Modifier::empty());
                paint_str(buf, after_col + 1, y, &after, FG, BG, Modifier::empty());
            } else {
                paint_str(buf, after_col, y, &after, FG, BG, Modifier::empty());
            }
        } else {
            paint_str(buf, text_start, y, line, FG, BG, Modifier::empty());
        }
    }

    if has_more_above && rows >= 3 {
        paint(
            buf,
            right.saturating_sub(1),
            top + 1,
            '↑',
            FG3,
            BG,
            Modifier::empty(),
        );
    }
    if has_more_below && rows >= 3 {
        paint(
            buf,
            right.saturating_sub(1),
            bot.saturating_sub(1),
            '↓',
            FG3,
            BG,
            Modifier::empty(),
        );
    }
}

fn build_composer_visual_lines(
    text: &str,
    cursor: usize,
    width: usize,
) -> (Vec<String>, usize, usize) {
    let mut out: Vec<String> = Vec::new();
    let mut cur_row = 0usize;
    let mut cur_col = 0usize;
    if text.is_empty() {
        out.push(String::new());
        return (out, 0, 0);
    }
    let w = width.max(1);
    let mut chars_so_far = 0usize;
    let logical: Vec<&str> = text.split('\n').collect();
    for (li, line) in logical.iter().enumerate() {
        let segs: Vec<String> = if line.is_empty() {
            vec![String::new()]
        } else {
            wrap_visual_chars(line, w)
        };
        for seg in segs {
            let seg_chars = seg.chars().count();
            let seg_start = chars_so_far;
            let seg_end = chars_so_far + seg_chars;
            if cursor >= seg_start && cursor <= seg_end {
                cur_row = out.len();
                cur_col = cursor - seg_start;
            }
            chars_so_far = seg_end;
            out.push(seg);
        }
        if li + 1 < logical.len() {
            chars_so_far += 1;
        }
    }
    (out, cur_row, cur_col)
}

fn wrap_visual_chars(line: &str, width: usize) -> Vec<String> {
    if line.is_empty() {
        return vec![String::new()];
    }
    let mut out = Vec::new();
    let mut current = String::new();
    let mut current_w = 0usize;
    for ch in line.chars() {
        let ch_w = unicode_width::UnicodeWidthChar::width(ch).unwrap_or(0);
        if current_w + ch_w > width && !current.is_empty() {
            out.push(std::mem::take(&mut current));
            current_w = 0;
        }
        current.push(ch);
        current_w += ch_w;
    }
    if !current.is_empty() || out.is_empty() {
        out.push(current);
    }
    out
}

fn render_input_meta(buf: &mut Buffer, area: Rect, row: u16, w: u16) {
    let mut col = area.x + 1;
    let left: [(&str, &str); 5] = [
        ("↵", "send"),
        ("⇧↵", "newline"),
        ("/", "cmd"),
        ("@", "file"),
        ("!", "shell"),
    ];
    for (i, (key, label)) in left.iter().enumerate() {
        if i > 0 {
            col = col.saturating_add(2);
        }
        col = paint_str(buf, col, row, key, FG1, BG, Modifier::BOLD);
        col = col.saturating_add(1);
        col = paint_str(buf, col, row, label, FG2, BG, Modifier::empty());
    }

    let right: [(&str, &str); 2] = [("esc", "cancel"), ("↑", "history")];
    let right_w = right_block_width(&right);
    let mut rcol = area.x + w.saturating_sub(right_w + 1);
    for (i, (key, label)) in right.iter().enumerate() {
        if i > 0 {
            rcol = rcol.saturating_add(2);
        }
        rcol = paint_str(buf, rcol, row, key, FG1, BG, Modifier::BOLD);
        rcol = rcol.saturating_add(1);
        rcol = paint_str(buf, rcol, row, label, FG2, BG, Modifier::empty());
    }
}

fn right_block_width(pairs: &[(&str, &str)]) -> u16 {
    let mut w = 0u16;
    for (i, (key, label)) in pairs.iter().enumerate() {
        if i > 0 {
            w = w.saturating_add(2);
        }
        w = w.saturating_add(key.width() as u16 + 1 + label.width() as u16);
    }
    w
}

fn render_status_bar(buf: &mut Buffer, area: Rect, row: u16, w: u16, state: &SceneState) {
    let mut col = area.x + 1;

    // Brand dot pulses to a brighter color while busy so the user can
    // tell the agent is actually doing something even when the cards
    // pane is scrolled off-screen. Visual width stays at 1 cell so the
    // rest of the status bar layout doesn't shift.
    let dot_color = if state.busy { DS_BRIGHT } else { OK };
    paint(buf, col, row, '●', dot_color, BG, Modifier::BOLD);
    col = col.saturating_add(2);
    let label = if state.busy {
        activity_label(state.activity.as_deref())
    } else {
        "reasonix"
    };
    col = paint_str(buf, col, row, label, DS_BRIGHT, BG, Modifier::BOLD);
    col = col.saturating_add(2);

    if let Some(mode) = state.edit_mode.as_ref() {
        let (label, color) = match mode {
            crate::state::EditMode::Review => ("REVIEW", INFO),
            crate::state::EditMode::Auto => ("AUTO", DS_PURPLE),
            crate::state::EditMode::Yolo => ("YOLO", ERR),
        };
        col = paint_pill(buf, col, row, "MODE", label, color);
        col = col.saturating_add(1);
    }

    if let Some(preset) = state.preset.as_deref() {
        let (label, color) = match preset {
            "auto" => ("AUTO", DS_BRIGHT),
            "flash" => ("FLASH", OK),
            "pro" => ("PRO", DS_BRIGHT),
            _ => (preset, FG2),
        };
        let label_upper: String = label.chars().flat_map(char::to_uppercase).collect();
        col = paint_pill(buf, col, row, "MODEL", &label_upper, color);
        col = col.saturating_add(1);
    }

    col = paint_sep(buf, col, row);

    let ctx_tokens = state.ctx_tokens.unwrap_or(0);
    let ctx_cap = state.ctx_cap.unwrap_or(0);
    let ctx_pct = if ctx_cap > 0 {
        (f64::from(ctx_tokens) / f64::from(ctx_cap) * 100.0) as f32
    } else {
        0.0
    };
    col = paint_str(buf, col, row, "ctx ", FG2, BG, Modifier::empty());
    col = paint_ctx_bar(buf, col, row, ctx_pct);
    let ctx_text = if ctx_cap > 0 {
        format!(" {}/{}", format_tokens(ctx_tokens), format_tokens(ctx_cap))
    } else {
        " —".to_string()
    };
    col = paint_str(buf, col, row, &ctx_text, FG, BG, Modifier::empty());
    col = paint_sep(buf, col, row);

    if state.session_input_tokens.is_some() || state.session_output_tokens.is_some() {
        col = paint_str(buf, col, row, "tok ", FG2, BG, Modifier::empty());
        let up = format!(
            "↑{} ",
            format_tokens(state.session_input_tokens.unwrap_or(0))
        );
        col = paint_str(buf, col, row, &up, OK, BG, Modifier::empty());
        let dn = format!(
            "↓{}",
            format_tokens(state.session_output_tokens.unwrap_or(0))
        );
        col = paint_str(buf, col, row, &dn, FG1, BG, Modifier::empty());
        col = paint_sep(buf, col, row);
    }

    col = paint_str(buf, col, row, "cache ", FG2, BG, Modifier::empty());
    let cache_text = state
        .cache_hit_ratio
        .map(|r| format!("{}%", (r * 100.0).round() as i32))
        .unwrap_or_else(|| "—".to_string());
    col = paint_str(buf, col, row, &cache_text, OK, BG, Modifier::empty());
    col = paint_sep(buf, col, row);

    col = paint_str(buf, col, row, "cost ", FG2, BG, Modifier::empty());
    let cost_text = format_cost(state.session_cost_usd, state.wallet_currency.as_deref());
    col = paint_str(buf, col, row, &cost_text, FG, BG, Modifier::empty());

    if let (Some(balance), Some(curr)) = (state.wallet_balance, state.wallet_currency.as_deref()) {
        let bal_text = format!("{} {}", format_balance(balance), curr);
        let bal_w = bal_text.width() as u16 + 2;
        if col + bal_w < area.x + w {
            let rcol = area.x + w.saturating_sub(bal_w);
            paint_str(buf, rcol, row, &bal_text, FG2, BG, Modifier::empty());
        }
    }
}

fn activity_label(activity: Option<&str>) -> &'static str {
    match activity.unwrap_or("") {
        "reasoning" | "thinking" => "reasoning…",
        "streaming" => "streaming…",
        "tool" | "tools" => "running tool…",
        "planning" | "plan" => "planning…",
        "compacting" | "compact" => "compacting context…",
        "" => "working…",
        _ => "working…",
    }
}

fn format_tokens(n: u32) -> String {
    if n >= 1_000_000 {
        format!("{:.1}M", f64::from(n) / 1_000_000.0)
    } else if n >= 100_000 {
        format!("{}k", n / 1000)
    } else if n >= 1_000 {
        format!("{:.1}k", f64::from(n) / 1000.0)
    } else {
        format!("{n}")
    }
}

fn format_cost(usd: Option<f64>, currency: Option<&str>) -> String {
    let Some(v) = usd else {
        return "—".to_string();
    };
    match currency {
        Some("CNY") | Some("RMB") => format!("¥{:.3}", v * 7.2),
        _ => format!("${v:.3}"),
    }
}

fn format_balance(n: f64) -> String {
    if n >= 1000.0 {
        format!("{:.1}", n)
    } else if n >= 100.0 {
        format!("{:.2}", n)
    } else {
        format!("{:.3}", n)
    }
}

fn paint_sep(buf: &mut Buffer, col: u16, row: u16) -> u16 {
    paint_str(buf, col, row, " │ ", FG3, BG, Modifier::empty())
}

fn paint_pill(
    buf: &mut Buffer,
    col: u16,
    row: u16,
    label: &str,
    value: &str,
    accent: ratatui::style::Color,
) -> u16 {
    let mut c = col;
    c = paint_str(buf, c, row, "◇ ", accent, BG, Modifier::BOLD);
    c = paint_str(buf, c, row, label, FG3, BG, Modifier::empty());
    c = paint_str(buf, c, row, " ", BG, BG, Modifier::empty());
    c = paint_str(buf, c, row, value, accent, BG, Modifier::BOLD);
    c
}

fn paint_ctx_bar(buf: &mut Buffer, x: u16, row: u16, pct: f32) -> u16 {
    let cells = 12u16;
    let filled = ((pct.clamp(0.0, 100.0) / 100.0) * f32::from(cells)).round() as u16;
    let bar_fg = ctx_color(pct);
    for i in 0..cells {
        let ch = if i < filled { '▰' } else { '▱' };
        let fg = if i < filled { bar_fg } else { FG3 };
        paint(buf, x + i, row, ch, fg, BG, Modifier::empty());
    }
    x + cells
}

fn ctx_color(pct: f32) -> Color {
    if pct > 80.0 {
        ERR
    } else if pct > 60.0 {
        WARN
    } else {
        DS_BRIGHT
    }
}
