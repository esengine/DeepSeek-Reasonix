use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use crate::state::SceneState;

use super::paint::{paint, paint_str};
use super::theme::{
    BG, COMPOSER_PLACEHOLDER, DS_BRIGHT, DS_PURPLE, ERR, FG, FG1, FG2, FG3, OK, WARN,
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
    let prompt_color = match state.composer_text.as_deref().and_then(|s| s.chars().next()) {
        Some('!') => OK,
        Some('/') => DS_BRIGHT,
        Some('@') => DS_PURPLE,
        _ => DS_BRIGHT,
    };
    let text = state.composer_text.as_deref().unwrap_or("");
    let show_caret = (tick / 6) % 2 == 0;
    let total_chars = text.chars().count();
    let cursor = state.composer_cursor.unwrap_or(total_chars).min(total_chars);
    let (cursor_line, cursor_col) = locate_cursor(text, cursor);

    let lines: Vec<&str> = if text.is_empty() {
        Vec::new()
    } else {
        text.split('\n').collect()
    };

    let scroll_off = if cursor_line + 1 > content_rows as usize {
        cursor_line + 1 - content_rows as usize
    } else {
        0
    };
    let has_more_above = scroll_off > 0;
    let has_more_below = scroll_off + (content_rows as usize) < lines.len();

    for i in 0..content_rows {
        let y = top + 1 + i;
        let col_start = area.x + 2;
        if i == 0 {
            paint_str(buf, col_start, y, "❯ ", prompt_color, BG, Modifier::BOLD);
        }
        let text_start = col_start + 2;
        let line_idx = scroll_off + i as usize;
        if text.is_empty() && i == 0 {
            paint_str(buf, text_start, y, COMPOSER_PLACEHOLDER, FG3, BG, Modifier::empty());
            if show_caret {
                paint(buf, text_start, y, '▮', DS_BRIGHT, BG, Modifier::empty());
            }
            continue;
        }
        let Some(line) = lines.get(line_idx) else {
            continue;
        };
        if line_idx == cursor_line {
            let before: String = line.chars().take(cursor_col).collect();
            let after: String = line.chars().skip(cursor_col).collect();
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
        paint(buf, right.saturating_sub(1), top + 1, '↑', FG3, BG, Modifier::empty());
    }
    if has_more_below && rows >= 3 {
        paint(buf, right.saturating_sub(1), bot.saturating_sub(1), '↓', FG3, BG, Modifier::empty());
    }
}

fn locate_cursor(text: &str, cursor: usize) -> (usize, usize) {
    let mut line = 0usize;
    let mut col = 0usize;
    let mut count = 0usize;
    for ch in text.chars() {
        if count == cursor {
            return (line, col);
        }
        if ch == '\n' {
            line += 1;
            col = 0;
        } else {
            col += 1;
        }
        count += 1;
    }
    (line, col)
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

fn render_status_bar(buf: &mut Buffer, area: Rect, row: u16, w: u16, _state: &SceneState) {
    let mut col = area.x + 1;

    paint(buf, col, row, '●', OK, BG, Modifier::BOLD);
    col = col.saturating_add(2);
    col = paint_str(buf, col, row, "reasonix", DS_BRIGHT, BG, Modifier::BOLD);
    col = paint_sep(buf, col, row);

    col = paint_str(buf, col, row, "ctx ", FG2, BG, Modifier::empty());
    col = paint_ctx_bar(buf, col, row, 15.0);
    col = paint_str(buf, col, row, " 19.2k/128k", FG, BG, Modifier::empty());
    col = paint_sep(buf, col, row);

    col = paint_str(buf, col, row, "tok ", FG2, BG, Modifier::empty());
    col = paint_str(buf, col, row, "↑12.4k ", OK, BG, Modifier::empty());
    col = paint_str(buf, col, row, "↓3.2k", FG1, BG, Modifier::empty());
    col = paint_sep(buf, col, row);

    col = paint_str(buf, col, row, "cost ", FG2, BG, Modifier::empty());
    paint_str(buf, col, row, "$0.043", FG, BG, Modifier::empty());

    let right_lbl = "git main";
    let right_w = right_lbl.width() as u16 + 3;
    let rcol = area.x + w.saturating_sub(right_w);
    paint_str(buf, rcol, row, right_lbl, FG2, BG, Modifier::empty());
    paint_str(buf, rcol + right_lbl.width() as u16 + 1, row, "●3", WARN, BG, Modifier::BOLD);
}

fn paint_sep(buf: &mut Buffer, col: u16, row: u16) -> u16 {
    paint_str(buf, col, row, " │ ", FG3, BG, Modifier::empty())
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
