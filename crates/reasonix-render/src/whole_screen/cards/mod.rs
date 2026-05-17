mod diff;
mod message;
mod notify;
mod output;
mod todo;
mod tool;

pub(super) use todo::{parse_todo_items, TodoState};

use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use crate::state::{SceneCard, SceneState};

use super::paint::{paint, paint_str};
use super::theme::{BG, DS, FG1, FG2, FG3, OK};

pub fn render_cards(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    state: &SceneState,
    scroll_offset: u16,
    tick: u32,
) {
    let bottom = area.y + area.height;
    if start_row >= bottom {
        return;
    }
    if state.cards.is_empty() {
        render_idle_banner(buf, area, start_row);
        return;
    }
    let avail = bottom - start_row;
    let scroll_w = if area.width > 0 { area.width - 1 } else { 0 };
    if scroll_w == 0 {
        return;
    }
    let body_w = body_width_for(Rect::new(0, 0, scroll_w, 1));
    let total: u16 = state.cards.iter().map(|c| card_height(c, body_w)).sum();
    if total == 0 {
        return;
    }
    let virt_rect = Rect::new(0, 0, scroll_w, total);
    let mut virt = Buffer::empty(virt_rect);
    let mut row = 0u16;
    let last_idx = state.cards.len().saturating_sub(1);
    for (i, card) in state.cards.iter().enumerate() {
        let streaming = state.busy && i == last_idx;
        row = render_card(&mut virt, virt_rect, row, card, streaming, tick);
    }

    let max_offset = total.saturating_sub(avail);
    let offset = scroll_offset.min(max_offset);
    let view_h = avail.min(total);
    let view_top = total.saturating_sub(view_h).saturating_sub(offset);
    let dest_top = bottom.saturating_sub(view_h);

    for dy in 0..view_h {
        let src_y = view_top + dy;
        if src_y >= total {
            break;
        }
        for dx in 0..scroll_w {
            buf[(area.x + dx, dest_top + dy)] = virt[(dx, src_y)].clone();
        }
    }

    render_scrollbar(buf, area, start_row, total, view_h, offset);
}

fn render_scrollbar(
    buf: &mut Buffer,
    area: Rect,
    start_row: u16,
    total: u16,
    view_h: u16,
    offset: u16,
) {
    if area.width == 0 || view_h == 0 || total <= view_h {
        return;
    }
    let track_x = area.x + area.width - 1;
    let track_top = area.y + area.height.saturating_sub(view_h);
    let max_offset = total - view_h;
    let thumb_size = ((view_h as f32 / total as f32) * view_h as f32).round() as u16;
    let thumb_size = thumb_size.max(1).min(view_h);
    let track_space = view_h - thumb_size;
    let thumb_top = if max_offset == 0 {
        track_space
    } else {
        let ratio = 1.0 - (offset as f32 / max_offset as f32);
        (ratio * track_space as f32).round() as u16
    };

    for y in 0..view_h {
        let abs_y = track_top + y;
        if abs_y < start_row {
            continue;
        }
        let in_thumb = y >= thumb_top && y < thumb_top + thumb_size;
        let (ch, fg) = if in_thumb { ('█', FG2) } else { ('│', FG3) };
        paint(buf, track_x, abs_y, ch, fg, BG, Modifier::empty());
    }
}

fn render_idle_banner(buf: &mut Buffer, area: Rect, start_row: u16) {
    let bottom = area.y + area.height;
    if start_row >= bottom {
        return;
    }
    paint_rail(buf, area, start_row, OK);
    let mut col = area.x + 2;
    col = paint_str(buf, col, start_row, "● ", OK, BG, Modifier::BOLD);
    col = paint_str(buf, col, start_row, "idle", OK, BG, Modifier::BOLD);
    let meta = "ready for next task";
    let mcol = area.x + area.width.saturating_sub(meta.len() as u16 + 1);
    paint_str(
        buf,
        mcol.max(col + 2),
        start_row,
        meta,
        FG3,
        BG,
        Modifier::empty(),
    );

    let row = start_row + 1;
    if row >= bottom {
        return;
    }
    paint_rail(buf, area, row, OK);
    let mut tcol = area.x + 4;
    tcol = paint_str(buf, tcol, row, "type below", FG1, BG, Modifier::empty());
    let pairs: [(&str, &str); 3] = [("/", "commands"), ("@", "file refs"), ("!", "shell")];
    for (key, label) in pairs {
        tcol = paint_str(buf, tcol, row, "  ·  ", FG3, BG, Modifier::empty());
        tcol = paint_str(buf, tcol, row, key, DS, BG, Modifier::BOLD);
        tcol = paint_str(buf, tcol, row, " ", FG2, BG, Modifier::empty());
        tcol = paint_str(buf, tcol, row, label, FG2, BG, Modifier::empty());
    }

    // Starter hints — surfaces the most-likely-useful slash commands
    // upfront, matches the Node TUI WelcomeBanner; helps brand-new
    // users discover the surface without /help.
    let starters: [(&str, &str); 4] = [
        ("/help", "see all commands"),
        ("/cost", "session spend + tokens"),
        ("/init", "scan repo into REASONIX.md"),
        ("/sessions", "switch / resume past sessions"),
    ];
    for (i, (cmd, desc)) in starters.iter().enumerate() {
        let r = start_row + 3 + i as u16;
        if r >= bottom {
            break;
        }
        paint_rail(buf, area, r, OK);
        let mut sc = area.x + 4;
        sc = paint_str(buf, sc, r, cmd, DS, BG, Modifier::BOLD);
        sc = paint_str(buf, sc, r, "  ", FG3, BG, Modifier::empty());
        paint_str(buf, sc, r, desc, FG2, BG, Modifier::empty());
    }
}

pub(super) fn total_cards_height(state: &SceneState, body_width: u16) -> u16 {
    state.cards.iter().map(|c| card_height(c, body_width)).sum()
}

pub(super) fn render_card_to(
    buf: &mut Buffer,
    area: Rect,
    row: u16,
    card: &SceneCard,
    streaming: bool,
) -> u16 {
    render_card(buf, area, row, card, streaming, u32::MAX)
}

pub(super) fn body_width_for(area: Rect) -> u16 {
    area.width.saturating_sub(4)
}

pub(super) fn wrap_visual(line: &str, width: u16) -> Vec<String> {
    let w = width as usize;
    if w == 0 {
        return vec![String::new()];
    }
    if line.is_empty() {
        return vec![String::new()];
    }
    if UnicodeWidthStr::width(line) <= w {
        return vec![line.to_string()];
    }
    let mut out = Vec::new();
    let mut current = String::new();
    let mut current_w = 0usize;
    for ch in line.chars() {
        let ch_w = unicode_width::UnicodeWidthChar::width(ch).unwrap_or(0);
        if current_w + ch_w > w && !current.is_empty() {
            out.push(std::mem::take(&mut current));
            current_w = 0;
        }
        current.push(ch);
        current_w += ch_w;
    }
    if !current.is_empty() {
        out.push(current);
    }
    if out.is_empty() {
        out.push(String::new());
    }
    out
}

fn body_lines_height(body: &str, width: u16) -> u16 {
    let mut total = 0u32;
    for line in body.split('\n') {
        let count = wrap_visual(line, width).len().max(1) as u32;
        total += count;
    }
    total.min(u16::MAX as u32) as u16
}

fn card_height(card: &SceneCard, body_width: u16) -> u16 {
    let is_md = matches!(card.kind.as_str(), "assistant" | "streaming");
    let body_lines = card
        .body
        .as_deref()
        .map(|b| {
            if is_md {
                super::md_render::count_visual_rows(b, body_width).min(u16::MAX as usize) as u16
            } else {
                body_lines_height(b, body_width)
            }
        })
        .unwrap_or(0);
    match card.kind.as_str() {
        "user" | "reasoning" | "think" | "thinking" | "assistant" | "streaming" | "diff"
        | "cmd" | "fileview" | "search" | "subagent" | "confirm" | "await" | "await_input"
        | "error" | "info" | "warn" | "usage" | "doctor" | "memory" | "ctx" | "plan-card"
        | "task" => 1 + body_lines + 1,
        "todo" | "plan" => {
            let items = card
                .body
                .as_deref()
                .map(|b| parse_todo_items(b).len() as u16)
                .unwrap_or(0);
            1 + items + 1
        }
        "tool" => 1,
        _ => 0,
    }
}

fn render_card(
    buf: &mut Buffer,
    area: Rect,
    row: u16,
    card: &SceneCard,
    streaming: bool,
    tick: u32,
) -> u16 {
    match card.kind.as_str() {
        "user" => message::render_user_card(buf, area, row, card),
        "reasoning" | "think" | "thinking" => message::render_reasoning_card(buf, area, row, card),
        "tool" => tool::render_tool_card(buf, area, row, card, tick),
        "assistant" | "streaming" => {
            message::render_assistant_card(buf, area, row, card, streaming, tick)
        }
        "todo" | "plan" => todo::render_todo_card(buf, area, row, card),
        "diff" => diff::render_diff_card(buf, area, row, card),
        "cmd" => output::render_cmd_card(buf, area, row, card),
        "fileview" => output::render_fileview_card(buf, area, row, card),
        "search" => output::render_search_card(buf, area, row, card),
        "subagent" => notify::render_subagent_card(buf, area, row, card),
        "confirm" => notify::render_confirm_card(buf, area, row, card),
        "await" | "await_input" => notify::render_await_card(buf, area, row, card),
        "error" => notify::render_error_card(buf, area, row, card),
        "info" => notify::render_info_card(buf, area, row, card, super::theme::INFO, "ⓘ"),
        "warn" => notify::render_info_card(buf, area, row, card, super::theme::WARN, "▲"),
        "usage" => notify::render_info_card(buf, area, row, card, super::theme::DS_BRIGHT, "$"),
        "doctor" => notify::render_info_card(buf, area, row, card, super::theme::OK, "✚"),
        "memory" => notify::render_info_card(buf, area, row, card, super::theme::DS_PURPLE, "◈"),
        "ctx" => notify::render_info_card(buf, area, row, card, super::theme::INFO, "▦"),
        "plan-card" => notify::render_info_card(buf, area, row, card, super::theme::DS_PURPLE, "◆"),
        "task" => notify::render_info_card(buf, area, row, card, super::theme::DS_BRIGHT, "▸"),
        _ => row,
    }
}

pub(super) fn paint_rail(buf: &mut Buffer, area: Rect, row: u16, color: Color) {
    paint(buf, area.x, row, '▎', color, BG, Modifier::empty());
}

#[allow(clippy::too_many_arguments)]
pub(super) fn render_card_header(
    buf: &mut Buffer,
    area: Rect,
    row: u16,
    rail_color: Color,
    glyph: &str,
    glyph_fg: Color,
    label: &str,
    label_fg: Color,
    meta_right: Option<(&str, Color)>,
) {
    paint_rail(buf, area, row, rail_color);
    let mut col = area.x + 2;
    col = paint_str(buf, col, row, glyph, glyph_fg, BG, Modifier::BOLD);
    col = col.saturating_add(1);
    paint_str(buf, col, row, label, label_fg, BG, Modifier::BOLD);
    if let Some((text, fg)) = meta_right {
        let w = text.width() as u16;
        let mcol = area.x + area.width.saturating_sub(w + 1);
        paint_str(buf, mcol, row, text, fg, BG, Modifier::empty());
    }
}

pub(super) fn body_indent_col(area: Rect) -> u16 {
    area.x + 4
}

pub(super) fn paint_body_line(
    buf: &mut Buffer,
    area: Rect,
    row: u16,
    rail_color: Color,
    line: &str,
    fg: Color,
    modifier: Modifier,
) {
    paint_rail(buf, area, row, rail_color);
    paint_str(buf, body_indent_col(area), row, line, fg, BG, modifier);
}

pub(super) fn paint_blank_after(buf: &mut Buffer, area: Rect, row: u16, rail_color: Color) -> u16 {
    if row >= area.y + area.height {
        return row;
    }
    paint_rail(buf, area, row, rail_color);
    row + 1
}
