use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::ListPicker;

use super::paint::{paint, paint_str};
use super::theme::{BG, DS, DS_BRIGHT, FG, FG2, FG3};

pub fn render_list_picker(buf: &mut Buffer, area: Rect, picker: &ListPicker, selected: usize) {
    if picker.options.is_empty() {
        return;
    }
    let max_label_w = picker
        .options
        .iter()
        .map(|o| o.label.width() + o.sublabel.as_deref().map(|s| s.width() + 4).unwrap_or(0))
        .max()
        .unwrap_or(20);
    let max_meta_w = picker
        .options
        .iter()
        .map(|o| o.meta.as_deref().map(UnicodeWidthStr::width).unwrap_or(0))
        .max()
        .unwrap_or(0);
    let title_w = picker.title.width();
    let inner_w = title_w
        .max(max_label_w + if max_meta_w > 0 { max_meta_w + 4 } else { 0 })
        .max(40);
    let popup_w = (inner_w as u16 + 6).min(area.width.saturating_sub(4));
    if popup_w < 30 {
        return;
    }
    let visible_cap = (area.height as usize).saturating_sub(6).clamp(3, 16);
    let visible = picker.options.len().min(visible_cap) as u16;
    let popup_h = 4 + visible + if picker.hint.is_some() { 1 } else { 0 };
    if popup_h > area.height {
        return;
    }
    let popup_x = area.x + (area.width.saturating_sub(popup_w)) / 2;
    let popup_y = area.y + (area.height.saturating_sub(popup_h)) / 2;
    let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);

    draw_box(buf, popup);
    paint_str(
        buf,
        popup.x + 2,
        popup.y + 1,
        &picker.title,
        DS_BRIGHT,
        BG,
        Modifier::BOLD,
    );
    let count_text = format!(
        "{} option{}",
        picker.options.len(),
        if picker.options.len() == 1 { "" } else { "s" }
    );
    let ccol = popup.x + popup.width.saturating_sub(count_text.width() as u16 + 2);
    paint_str(
        buf,
        ccol,
        popup.y + 1,
        &count_text,
        FG3,
        BG,
        Modifier::empty(),
    );

    let chosen = selected.min(picker.options.len().saturating_sub(1));
    let visible_usz = visible as usize;
    let start = if chosen >= visible_usz {
        chosen + 1 - visible_usz
    } else {
        0
    };
    let row_top = popup.y + 3;
    for (i, opt) in picker
        .options
        .iter()
        .enumerate()
        .skip(start)
        .take(visible_usz)
    {
        let row = row_top + (i - start) as u16;
        let is_sel = i == chosen;
        let label_fg = if is_sel { FG } else { DS_BRIGHT };
        let modifier = if is_sel {
            Modifier::BOLD
        } else {
            Modifier::empty()
        };
        if is_sel {
            paint_str(buf, popup.x + 2, row, "▸", DS_BRIGHT, BG, Modifier::BOLD);
        }
        let mut col = popup.x + 4;
        col = paint_str(buf, col, row, &opt.label, label_fg, BG, modifier);
        if let Some(sub) = opt.sublabel.as_deref() {
            col = col.saturating_add(2);
            paint_str(buf, col, row, sub, FG2, BG, Modifier::empty());
        }
        if let Some(meta) = opt.meta.as_deref() {
            let mw = meta.width() as u16;
            let mcol = popup.x + popup.width.saturating_sub(mw + 2);
            paint_str(buf, mcol, row, meta, FG3, BG, Modifier::ITALIC);
        }
    }

    let hint_row = popup.y + popup.height.saturating_sub(2);
    let hint = picker
        .hint
        .as_deref()
        .unwrap_or("↑↓ move  ↵ select  esc cancel");
    paint_str(buf, popup.x + 2, hint_row, hint, FG2, BG, Modifier::empty());
}

fn draw_box(buf: &mut Buffer, area: Rect) {
    let w = area.width;
    if w < 2 {
        return;
    }
    let top = area.y;
    let bot = area.y + area.height - 1;
    let right = area.x + w - 1;
    paint(buf, area.x, top, '╭', DS, BG, Modifier::empty());
    paint(buf, right, top, '╮', DS, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, top, '─', DS, BG, Modifier::empty());
    }
    for y in (top + 1)..bot {
        paint(buf, area.x, y, '│', DS, BG, Modifier::empty());
        paint(buf, right, y, '│', DS, BG, Modifier::empty());
        for x in 1..w - 1 {
            paint(buf, area.x + x, y, ' ', FG, BG, Modifier::empty());
        }
    }
    paint(buf, area.x, bot, '╰', DS, BG, Modifier::empty());
    paint(buf, right, bot, '╯', DS, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, bot, '─', DS, BG, Modifier::empty());
    }
}
