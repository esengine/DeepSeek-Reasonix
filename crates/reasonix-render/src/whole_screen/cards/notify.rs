use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::SceneCard;

use super::super::paint::{paint, paint_str};
use super::super::theme::{BG, DS_BRIGHT, ERR, FG, FG1, FG2, FG3, INFO, WARN};
use super::{
    body_indent_col, body_width_for, paint_blank_after, paint_body_line, paint_rail,
    render_card_header, wrap_visual,
};

pub(super) fn render_subagent_card(
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
        "⧪",
        INFO,
        &card.summary,
        INFO,
        card.meta.as_deref().map(|s| (s, FG3)),
    );
    row += 1;
    let width = body_width_for(area);
    if let Some(body) = card.body.as_deref() {
        for (i, raw) in body.split('\n').enumerate() {
            let wrap_w = if i == 0 {
                width
            } else {
                width.saturating_sub(2)
            };
            for (wi, line) in wrap_visual(raw, wrap_w).iter().enumerate() {
                if row >= bottom {
                    return row;
                }
                if i == 0 || wi > 0 {
                    paint_body_line(buf, area, row, INFO, line, FG1, Modifier::empty());
                } else {
                    paint_rail(buf, area, row, INFO);
                    let col = body_indent_col(area);
                    paint(buf, col, row, '→', INFO, BG, Modifier::BOLD);
                    paint_str(buf, col + 2, row, line, FG1, BG, Modifier::empty());
                }
                row += 1;
            }
        }
    }
    paint_blank_after(buf, area, row, INFO)
}

pub(super) fn render_confirm_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
) -> u16 {
    let bottom = area.y + area.height;
    let mut row = start_row;
    let title = format!("permission required · {}", card.summary);
    render_card_header(buf, area, row, WARN, "⚠", WARN, &title, WARN, None);
    row += 1;
    if let Some(body) = card.body.as_deref() {
        let mut lines = body.split('\n');
        if let Some(question) = lines.next() {
            if row < bottom {
                paint_body_line(buf, area, row, WARN, question, FG, Modifier::empty());
                row += 1;
            }
        }
        for opts in lines {
            if row >= bottom {
                return row;
            }
            paint_rail(buf, area, row, WARN);
            let mut col = body_indent_col(area);
            for tok in opts.split_whitespace() {
                col = paint_option_token(buf, col, row, tok);
                col = col.saturating_add(2);
            }
            row += 1;
        }
    }
    paint_blank_after(buf, area, row, WARN)
}

fn paint_option_token(buf: &mut Buffer, x: u16, row: u16, tok: &str) -> u16 {
    let (key, label) = match tok.find(']') {
        Some(close) if tok.starts_with('[') => {
            let key = &tok[1..close];
            let label = &tok[close + 1..];
            (key, label)
        }
        _ => ("", tok),
    };
    let mut col = x;
    if !key.is_empty() {
        let key_fg = match key {
            "y" | "Y" => DS_BRIGHT,
            "n" | "N" => FG2,
            "a" | "A" => ERR,
            _ => FG1,
        };
        col = paint_str(buf, col, row, "[", FG3, BG, Modifier::empty());
        col = paint_str(buf, col, row, key, key_fg, BG, Modifier::BOLD);
        col = paint_str(buf, col, row, "]", FG3, BG, Modifier::empty());
        col = col.saturating_add(1);
    }
    paint_str(buf, col, row, label, FG1, BG, Modifier::empty())
}

pub(super) fn render_await_card(
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
        "?",
        DS_BRIGHT,
        if card.summary.is_empty() {
            "awaiting input"
        } else {
            &card.summary
        },
        DS_BRIGHT,
        None,
    );
    row += 1;
    if let Some(body) = card.body.as_deref() {
        let mut lines = body.split('\n');
        if let Some(question) = lines.next() {
            if row < bottom {
                paint_body_line(buf, area, row, DS_BRIGHT, question, FG, Modifier::empty());
                row += 1;
            }
        }
        for line in lines {
            if row >= bottom {
                return row;
            }
            paint_rail(buf, area, row, DS_BRIGHT);
            let col = body_indent_col(area);
            paint_await_option(buf, col, row, line);
            row += 1;
        }
    }
    paint_blank_after(buf, area, row, DS_BRIGHT)
}

fn paint_await_option(buf: &mut Buffer, x: u16, row: u16, line: &str) {
    if let Some(close) = line.find(')') {
        let key = &line[..close];
        let label = line[close + 1..].trim_start();
        let key_w = key.width() as u16;
        paint_str(buf, x, row, key, DS_BRIGHT, BG, Modifier::BOLD);
        paint_str(buf, x + key_w, row, ")", FG2, BG, Modifier::empty());
        paint_str(buf, x + key_w + 2, row, label, FG1, BG, Modifier::empty());
    } else {
        paint_str(buf, x, row, line, FG1, BG, Modifier::empty());
    }
}

pub(super) fn render_error_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
) -> u16 {
    let bottom = area.y + area.height;
    let mut row = start_row;
    render_card_header(buf, area, row, ERR, "✕", ERR, &card.summary, ERR, None);
    row += 1;
    if let Some(body) = card.body.as_deref() {
        let width = body_width_for(area);
        for raw in body.split('\n') {
            let fg = if raw.trim_start().starts_with("at ") {
                FG2
            } else {
                FG
            };
            for line in wrap_visual(raw, width) {
                if row >= bottom {
                    return row;
                }
                paint_body_line(buf, area, row, ERR, &line, fg, Modifier::empty());
                row += 1;
            }
        }
    }
    paint_blank_after(buf, area, row, ERR)
}
