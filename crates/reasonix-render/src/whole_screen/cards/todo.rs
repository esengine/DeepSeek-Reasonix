use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;

use crate::state::SceneCard;

use super::super::paint::paint_str;
use super::super::theme::{BG, DS_BRIGHT, DS_PURPLE, FG, FG1, FG2, FG3, OK};
use super::{paint_blank_after, paint_rail, render_card_header};

#[derive(Clone, Copy)]
pub(in crate::whole_screen) enum TodoState {
    Done,
    Active,
    Pending,
}

pub(in crate::whole_screen) fn parse_todo_items(body: &str) -> Vec<(TodoState, &str)> {
    body.lines()
        .filter_map(|line| {
            let t = line.trim_start();
            if let Some(rest) = t.strip_prefix("[x]") {
                Some((TodoState::Done, rest.trim_start()))
            } else if let Some(rest) = t.strip_prefix("[~]") {
                Some((TodoState::Active, rest.trim_start()))
            } else if let Some(rest) = t.strip_prefix("[ ]") {
                Some((TodoState::Pending, rest.trim_start()))
            } else {
                None
            }
        })
        .collect()
}

pub(super) fn render_todo_card(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    card: &SceneCard,
) -> u16 {
    let bottom = area.y + area.height;
    let body = card.body.as_deref().unwrap_or("");
    let items = parse_todo_items(body);
    let done = items
        .iter()
        .filter(|(s, _)| matches!(s, TodoState::Done))
        .count();
    let meta = format!("{} of {} complete", done, items.len());
    let mut row = start_row;
    render_card_header(
        buf,
        area,
        row,
        DS_PURPLE,
        "◆",
        DS_PURPLE,
        "PLAN",
        DS_PURPLE,
        Some((&meta, FG3)),
    );
    row += 1;
    for (state, label) in items {
        if row >= bottom {
            return row;
        }
        paint_rail(buf, area, row, DS_PURPLE);
        let (marker, marker_fg, label_fg, label_mod) = match state {
            TodoState::Done => ("[x]", OK, FG2, Modifier::empty()),
            TodoState::Active => ("[~]", DS_BRIGHT, FG, Modifier::BOLD),
            TodoState::Pending => ("[ ]", FG3, FG1, Modifier::empty()),
        };
        let mut c = area.x + 2;
        c = paint_str(buf, c, row, marker, marker_fg, BG, Modifier::BOLD);
        c = c.saturating_add(1);
        paint_str(buf, c, row, label, label_fg, BG, label_mod);
        row += 1;
    }
    paint_blank_after(buf, area, row, DS_PURPLE)
}
