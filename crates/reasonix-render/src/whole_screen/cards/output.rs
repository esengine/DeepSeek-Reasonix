use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::SceneCard;

use super::super::paint::paint_str;
use super::super::theme::{BG, DS_BRIGHT, ERR, FG, FG1, FG2, FG3, INFO, OK};
use super::{
    body_width_for, paint_blank_after, paint_body_line, paint_rail, render_card_header, wrap_visual,
};

pub(super) fn render_cmd_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
) -> u16 {
    let bottom = area.y + area.height;
    let mut row = start_row;
    paint_rail(buf, area, row, OK);
    let mut col = area.x + 2;
    col = paint_str(buf, col, row, "$ ", OK, BG, Modifier::BOLD);
    col = paint_str(buf, col, row, &card.summary, FG, BG, Modifier::empty());
    if let Some(meta) = card.meta.as_deref() {
        let w = meta.width() as u16;
        let mcol = area.x + area.width.saturating_sub(w + 1);
        let fg = if meta.contains("0") && meta.starts_with("exit 0") {
            OK
        } else if meta.starts_with("exit ") {
            ERR
        } else {
            FG2
        };
        paint_str(buf, mcol.max(col + 2), row, meta, fg, BG, Modifier::empty());
    }
    row += 1;
    if let Some(body) = card.body.as_deref() {
        let width = body_width_for(area);
        for raw in body.split('\n') {
            let trimmed = raw.trim_end();
            let fg = if trimmed.starts_with("  ✓") || trimmed.contains("passed") {
                OK
            } else if trimmed.starts_with("  ✕") || trimmed.contains("failed") {
                ERR
            } else if trimmed.starts_with("stdout")
                || trimmed.starts_with("stderr")
                || trimmed.starts_with(" RUN")
            {
                FG2
            } else {
                FG1
            };
            for line in wrap_visual(trimmed, width) {
                if row >= bottom {
                    return row;
                }
                paint_body_line(buf, area, row, OK, &line, fg, Modifier::empty());
                row += 1;
            }
        }
    }
    paint_blank_after(buf, area, row, OK)
}

pub(super) fn render_fileview_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
) -> u16 {
    let bottom = area.y + area.height;
    let mut row = start_row;
    render_card_header(
        buf,
        area,
        row,
        DS_BRIGHT,
        "📄",
        DS_BRIGHT,
        &card.summary,
        DS_BRIGHT,
        card.meta.as_deref().map(|s| (s, FG3)),
    );
    row += 1;
    if let Some(body) = card.body.as_deref() {
        for line in body.split('\n') {
            if row >= bottom {
                return row;
            }
            paint_rail(buf, area, row, DS_BRIGHT);
            let (ln, code) = split_first_colon(line);
            let mut col = area.x + 2;
            col = paint_str(
                buf,
                col,
                row,
                &format!("{ln:>4}"),
                FG3,
                BG,
                Modifier::empty(),
            );
            col = col.saturating_add(2);
            paint_str(buf, col, row, code, FG1, BG, Modifier::empty());
            row += 1;
        }
    }
    paint_blank_after(buf, area, row, DS_BRIGHT)
}

pub(super) fn render_search_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
) -> u16 {
    let bottom = area.y + area.height;
    let mut row = start_row;
    render_card_header(
        buf,
        area,
        row,
        INFO,
        "⌕",
        INFO,
        &card.summary,
        INFO,
        card.meta.as_deref().map(|s| (s, FG3)),
    );
    row += 1;
    if let Some(body) = card.body.as_deref() {
        for line in body.split('\n') {
            if row >= bottom {
                return row;
            }
            paint_rail(buf, area, row, INFO);
            let (file_part, rest) = split_first_colon(line);
            let (ln_part, code_part) = split_first_colon(rest);
            let mut col = area.x + 2;
            col = paint_str(buf, col, row, file_part, DS_BRIGHT, BG, Modifier::empty());
            col = paint_str(buf, col, row, ":", FG3, BG, Modifier::empty());
            col = paint_str(buf, col, row, ln_part, FG2, BG, Modifier::empty());
            col = col.saturating_add(2);
            paint_str(buf, col, row, code_part, FG1, BG, Modifier::empty());
            row += 1;
        }
    }
    paint_blank_after(buf, area, row, INFO)
}

fn split_first_colon(s: &str) -> (&str, &str) {
    match s.split_once(':') {
        Some((a, b)) => (a, b),
        None => (s, ""),
    }
}
