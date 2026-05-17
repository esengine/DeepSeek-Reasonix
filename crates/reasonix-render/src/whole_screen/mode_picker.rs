use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use unicode_width::UnicodeWidthStr;

use super::paint::{paint, paint_str};
use super::theme::{BG, DS_BRIGHT, DS_PURPLE, ERR, FG, FG1, FG2, FG3, INFO, OK, WARN};

pub struct PickerOption {
    pub marker: &'static str,
    pub marker_color: Color,
    pub title: &'static str,
    pub summary: &'static str,
    pub hot: &'static str,
}

pub fn mode_picker_options() -> [PickerOption; 3] {
    [
        PickerOption {
            marker: "○",
            marker_color: INFO,
            title: "review",
            summary: "每个工具调用都要确认 · 最安全",
            hot: "shift+tab",
        },
        PickerOption {
            marker: "◆",
            marker_color: DS_PURPLE,
            title: "auto",
            summary: "只在编辑 / 删除 / shell 时确认 · 默认",
            hot: "shift+tab",
        },
        PickerOption {
            marker: "⚡",
            marker_color: ERR,
            title: "yolo",
            summary: "完全自主 · 任何操作都不问 · 危险",
            hot: "shift+tab",
        },
    ]
}

pub fn preset_picker_options() -> [PickerOption; 3] {
    [
        PickerOption {
            marker: "◇",
            marker_color: DS_BRIGHT,
            title: "auto",
            summary: "flash → pro on hard turns · default",
            hot: "click",
        },
        PickerOption {
            marker: "◇",
            marker_color: OK,
            title: "flash",
            summary: "v4-flash always · cheapest, predictable",
            hot: "click",
        },
        PickerOption {
            marker: "◇",
            marker_color: DS_BRIGHT,
            title: "pro",
            summary: "v4-pro always · ~3× flash, hard multi-turn work",
            hot: "click",
        },
    ]
}

pub fn render_picker(
    buf: &mut Buffer,
    dock_area: Rect,
    title: &str,
    subtitle: &str,
    accent: Color,
    options: &[PickerOption],
    selected_idx: usize,
) {
    let row_h: u16 = 2;
    let inner_rows = (options.len() as u16) * row_h;
    let popup_h = 3 + inner_rows;
    let popup_w = dock_area.width.saturating_sub(4).min(110);
    if popup_w < 40 || popup_h > dock_area.y {
        return;
    }
    let popup_x = dock_area.x + 2;
    let popup_y = dock_area.y.saturating_sub(popup_h);
    let popup = Rect::new(popup_x, popup_y, popup_w, popup_h);

    draw_box(buf, popup, accent);
    draw_title(buf, popup, title, subtitle, accent);

    let body_top = popup.y + 2;
    for (i, opt) in options.iter().enumerate() {
        let row = body_top + (i as u16) * row_h;
        let selected = i == selected_idx;
        let title_color = if selected { opt.marker_color } else { FG };
        let title_mod = if selected {
            Modifier::BOLD
        } else {
            Modifier::empty()
        };

        if selected {
            paint_str(
                buf,
                popup.x + 2,
                row,
                "▸",
                opt.marker_color,
                BG,
                Modifier::BOLD,
            );
        }
        paint_str(
            buf,
            popup.x + 4,
            row,
            opt.marker,
            opt.marker_color,
            BG,
            Modifier::BOLD,
        );
        let mut col = popup.x + 6;
        col = paint_str(buf, col, row, opt.title, title_color, BG, title_mod);
        let _ = col;

        let hot_label = opt.hot;
        let right_w = hot_label.width() as u16;
        let hot_col = popup.x + popup.width.saturating_sub(right_w + 2);
        paint_str(buf, hot_col, row, hot_label, FG3, BG, Modifier::empty());

        if row + 1 < popup.y + popup.height - 1 {
            paint_str(
                buf,
                popup.x + 6,
                row + 1,
                opt.summary,
                FG2,
                BG,
                Modifier::empty(),
            );
        }
    }
    let _ = (FG1, WARN);
}

fn draw_title(buf: &mut Buffer, area: Rect, title: &str, subtitle: &str, accent: Color) {
    let row = area.y;
    let mut col = area.x + 2;
    col = paint_str(buf, col, row, "▢", accent, BG, Modifier::BOLD);
    col = col.saturating_add(1);
    col = paint_str(buf, col, row, title, accent, BG, Modifier::BOLD);
    if !subtitle.is_empty() {
        col = col.saturating_add(2);
        paint_str(buf, col, row, subtitle, FG2, BG, Modifier::empty());
    }
    let hint = "esc close";
    let hint_col = area.x + area.width.saturating_sub(hint.width() as u16 + 2);
    paint_str(buf, hint_col, row, hint, FG3, BG, Modifier::empty());
}

fn draw_box(buf: &mut Buffer, area: Rect, accent: Color) {
    let w = area.width;
    if w < 2 {
        return;
    }
    let top = area.y;
    let bot = area.y + area.height - 1;
    let right = area.x + w - 1;

    paint(buf, area.x, top, '╭', accent, BG, Modifier::empty());
    paint(buf, right, top, '╮', accent, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, top, '─', accent, BG, Modifier::empty());
    }
    for y in (top + 1)..bot {
        paint(buf, area.x, y, '│', accent, BG, Modifier::empty());
        paint(buf, right, y, '│', accent, BG, Modifier::empty());
        for x in 1..w - 1 {
            paint(buf, area.x + x, y, ' ', FG, BG, Modifier::empty());
        }
    }
    paint(buf, area.x, bot, '╰', accent, BG, Modifier::empty());
    paint(buf, right, bot, '╯', accent, BG, Modifier::empty());
    for x in 1..w - 1 {
        paint(buf, area.x + x, bot, '─', accent, BG, Modifier::empty());
    }
}
