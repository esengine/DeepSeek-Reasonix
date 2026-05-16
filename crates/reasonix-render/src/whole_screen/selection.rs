use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Modifier;
use unicode_width::UnicodeWidthStr;

use crate::state::SceneState;

use super::cards::{render_card_to, total_cards_height};
use super::theme::{DOCK_HEIGHT, SIDEBAR_WIDTH};

const BOOT_HEIGHT: u16 = 15;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Selection {
    pub anchor: (u16, u16),
    pub head: (u16, u16),
}

impl Selection {
    pub fn new(col: u16, virt_y: u16) -> Self {
        Self {
            anchor: (col, virt_y),
            head: (col, virt_y),
        }
    }

    pub fn extend(&mut self, col: u16, virt_y: u16) {
        self.head = (col, virt_y);
    }

    pub fn is_empty(&self) -> bool {
        self.anchor == self.head
    }

    pub fn normalized(&self) -> ((u16, u16), (u16, u16)) {
        let (ac, ay) = self.anchor;
        let (hc, hy) = self.head;
        if (ay, ac) <= (hy, hc) {
            (self.anchor, self.head)
        } else {
            (self.head, self.anchor)
        }
    }

    pub fn contains_virt(&self, col: u16, virt_y: u16) -> bool {
        let ((sc, sy), (ec, ey)) = self.normalized();
        if virt_y < sy || virt_y > ey {
            return false;
        }
        if sy == ey {
            col >= sc && col <= ec
        } else if virt_y == sy {
            col >= sc
        } else if virt_y == ey {
            col <= ec
        } else {
            true
        }
    }
}

#[derive(Clone, Copy, Debug)]
pub struct CardsLayout {
    pub screen_rect: Rect,
    pub view_top: u16,
    pub view_h: u16,
    pub total: u16,
}

impl CardsLayout {
    pub fn contains_screen(&self, x: u16, y: u16) -> bool {
        x >= self.screen_rect.x
            && x < self.screen_rect.right()
            && y >= self.screen_rect.y
            && y < self.screen_rect.bottom()
    }

    pub fn project_clamped(&self, x: u16, y: u16) -> (u16, u16) {
        let min_x = self.screen_rect.x;
        let max_x = self
            .screen_rect
            .right()
            .saturating_sub(2)
            .max(min_x);
        let min_y = self.screen_rect.y;
        let max_y = self
            .screen_rect
            .bottom()
            .saturating_sub(1)
            .max(min_y);
        let col = x.clamp(min_x, max_x);
        let row = y.clamp(min_y, max_y);
        let virt_y = self.view_top.saturating_add(row - min_y);
        (col, virt_y)
    }
}

pub fn cards_layout(terminal: Rect, state: &SceneState, scroll_offset: u16) -> CardsLayout {
    let main_w = if terminal.width > SIDEBAR_WIDTH + 30 {
        terminal.width - SIDEBAR_WIDTH
    } else {
        terminal.width
    };
    let dock_h = DOCK_HEIGHT.min(terminal.height);
    let scroll_h = terminal.height.saturating_sub(dock_h);
    let boot = BOOT_HEIGHT.min(scroll_h);
    let cards_y = terminal.y + boot;
    let cards_h = scroll_h.saturating_sub(boot);
    let screen_rect = Rect::new(terminal.x, cards_y, main_w, cards_h);

    let total = total_cards_height(state);
    let avail = cards_h;
    let max_offset = total.saturating_sub(avail);
    let offset = scroll_offset.min(max_offset);
    let view_h = avail.min(total);
    let view_top = total.saturating_sub(view_h).saturating_sub(offset);

    CardsLayout {
        screen_rect,
        view_top,
        view_h,
        total,
    }
}

pub fn apply_highlight(buf: &mut Buffer, layout: &CardsLayout, sel: Selection) {
    if sel.is_empty() || layout.view_h == 0 {
        return;
    }
    let top = layout.screen_rect.y;
    let bottom = layout.screen_rect.bottom();
    let left = layout.screen_rect.x;
    let right = layout.screen_rect.right().saturating_sub(1);
    for y in top..bottom {
        let virt_y = layout.view_top + (y - top);
        for x in left..right {
            if sel.contains_virt(x, virt_y) {
                let style = buf[(x, y)].style().add_modifier(Modifier::REVERSED);
                buf[(x, y)].set_style(style);
            }
        }
    }
}

pub fn extract_text(
    state: &SceneState,
    scroll_offset: u16,
    terminal: Rect,
    sel: Selection,
) -> String {
    if sel.is_empty() {
        return String::new();
    }
    let layout = cards_layout(terminal, state, scroll_offset);
    if layout.total == 0 {
        return String::new();
    }
    let scroll_w = layout.screen_rect.width.saturating_sub(1);
    if scroll_w == 0 {
        return String::new();
    }
    let virt_rect = Rect::new(0, 0, scroll_w, layout.total);
    let mut virt = Buffer::empty(virt_rect);
    let mut row = 0u16;
    let last_idx = state.cards.len().saturating_sub(1);
    for (i, card) in state.cards.iter().enumerate() {
        let streaming = state.busy && i == last_idx;
        row = render_card_to(&mut virt, virt_rect, row, card, streaming);
    }

    let ((sc, sy), (ec, ey)) = sel.normalized();
    let offset_x = layout.screen_rect.x;
    let start_x = sc.saturating_sub(offset_x);
    let end_x = ec.saturating_sub(offset_x);
    let mut out = String::new();
    for y in sy..=ey {
        if y >= layout.total {
            break;
        }
        let row_start = if y == sy { start_x } else { 0 };
        let row_end = if y == ey {
            end_x
        } else {
            scroll_w.saturating_sub(1)
        };
        let mut x = row_start;
        let mut line = String::new();
        while x <= row_end && x < scroll_w {
            let sym = virt[(x, y)].symbol();
            line.push_str(sym);
            let cw = UnicodeWidthStr::width(sym).max(1) as u16;
            x = x.saturating_add(cw);
        }
        out.push_str(line.trim_end());
        if y < ey {
            out.push('\n');
        }
    }
    out
}
