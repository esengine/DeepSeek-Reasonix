use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;

use crate::state::SceneCard;

use super::super::md_render::render_markdown;
use super::super::paint::{format_ts, paint};
use super::super::theme::{BG, DS_BRIGHT, DS_PURPLE, FG, FG1, FG3};
use super::{
    body_indent_col, body_width_for, paint_blank_after, paint_body_line, render_card_header,
    wrap_visual,
};

pub(super) fn render_user_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
) -> u16 {
    let bottom = area.y + area.height;
    let mut row = start_row;
    let ts = card.ts.map(format_ts);
    render_card_header(
        buf,
        area,
        row,
        FG,
        "❯",
        FG,
        "YOU",
        FG,
        ts.as_deref().map(|s| (s, FG3)),
    );
    row += 1;
    let width = body_width_for(area);
    if let Some(body) = card.body.as_deref() {
        for raw in body.split('\n') {
            for line in wrap_visual(raw, width) {
                if row >= bottom {
                    return row;
                }
                paint_body_line(buf, area, row, FG, &line, FG, Modifier::empty());
                row += 1;
            }
        }
    }
    paint_blank_after(buf, area, row, FG)
}

pub(super) fn render_reasoning_card(
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
        DS_PURPLE,
        "◇",
        DS_PURPLE,
        "THINKING",
        DS_PURPLE,
        card.meta.as_deref().map(|s| (s, FG3)),
    );
    row += 1;
    let width = body_width_for(area);
    if let Some(body) = card.body.as_deref() {
        for raw in body.split('\n') {
            for line in wrap_visual(raw, width) {
                if row >= bottom {
                    return row;
                }
                paint_body_line(buf, area, row, DS_PURPLE, &line, FG1, Modifier::ITALIC);
                row += 1;
            }
        }
    }
    paint_blank_after(buf, area, row, DS_PURPLE)
}

pub(super) fn render_assistant_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
    streaming: bool,
    tick: u32,
) -> u16 {
    let bottom = area.y + area.height;
    let mut row = start_row;
    let meta_text = if streaming {
        Some("streaming…".to_string())
    } else {
        card.ts.map(format_ts)
    };
    render_card_header(
        buf,
        area,
        row,
        DS_BRIGHT,
        "◆",
        DS_BRIGHT,
        "REASONIX",
        DS_BRIGHT,
        meta_text.as_deref().map(|s| (s, FG3)),
    );
    row += 1;
    if let Some(body) = card.body.as_deref() {
        let revealed: String = if streaming {
            let total_chars = body.chars().count();
            let n = ((tick as usize) / 2).min(total_chars);
            body.chars().take(n).collect()
        } else {
            body.to_string()
        };
        let prev_row = row;
        row = render_markdown(buf, area, row, bottom, DS_BRIGHT, FG, &revealed);
        if streaming && (tick / 6).is_multiple_of(2) {
            let caret_row = if row > prev_row { row - 1 } else { prev_row };
            if caret_row < bottom {
                let col = body_indent_col(area) + 4;
                paint(buf, col, caret_row, '▊', DS_BRIGHT, BG, Modifier::empty());
            }
        }
        if streaming && row == prev_row {
            row = (prev_row + 1).min(bottom);
        }
    }
    paint_blank_after(buf, area, row, DS_BRIGHT)
}
