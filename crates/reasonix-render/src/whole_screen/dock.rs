use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use crate::state::SceneState;

use super::paint::{paint, paint_str};
use super::theme::{
    BG, COMPOSER_PLACEHOLDER, DS_BRIGHT, ERR, FG, FG1, FG2, FG3, OK, WARN,
};

pub fn render_dock(buf: &mut Buffer, area: Rect, state: &SceneState, tick: u32) {
    if area.height < 3 {
        return;
    }
    let inner_w = area.width;
    let composer_rows = 3u16.min(area.height);
    render_input_box(buf, area, composer_rows, state, tick);

    let mut row = area.y + composer_rows;
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
    if w < 4 || rows == 0 {
        return;
    }
    let top = area.y;
    let mid = top + 1;
    let bot = top + rows.saturating_sub(1);
    let right = area.x + w - 1;

    paint(buf, area.x, top, '╭', FG3, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, top, '─', FG3, BG, Modifier::empty());
    }
    paint(buf, right, top, '╮', FG3, BG, Modifier::empty());

    if rows >= 2 {
        paint(buf, area.x, mid, '│', FG3, BG, Modifier::empty());
        paint(buf, right, mid, '│', FG3, BG, Modifier::empty());
        let mut col = area.x + 2;
        col = paint_str(buf, col, mid, "❯ ", DS_BRIGHT, BG, Modifier::BOLD);
        let text = state.composer_text.as_deref().unwrap_or("");
        let show_caret = (tick / 6) % 2 == 0;
        let caret_col = if text.is_empty() {
            paint_str(buf, col, mid, COMPOSER_PLACEHOLDER, FG3, BG, Modifier::empty());
            col
        } else {
            paint_str(buf, col, mid, text, FG, BG, Modifier::empty())
        };
        if show_caret {
            paint(buf, caret_col, mid, '▮', DS_BRIGHT, BG, Modifier::empty());
        }
    }

    if rows >= 3 {
        paint(buf, area.x, bot, '╰', FG3, BG, Modifier::empty());
        for x in 1..w - 1 {
            paint(buf, area.x + x, bot, '─', FG3, BG, Modifier::empty());
        }
        paint(buf, right, bot, '╯', FG3, BG, Modifier::empty());
    }
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
