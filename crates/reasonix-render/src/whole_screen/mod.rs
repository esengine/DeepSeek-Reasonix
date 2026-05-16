mod boot;
mod cards;
mod demo;
mod dock;
mod overlay;
mod overlay_at;
mod paint;
mod selection;
mod sidebar;
mod theme;

use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::widgets::Widget;

use crate::state::SceneState;

pub use demo::demo_state;
pub use overlay::{slash_completion, slash_match_count};
pub use overlay_at::{at_completion, at_match_count};
pub use paint::{paint, paint_str};
pub use selection::{cards_layout, extract_text, CardsLayout, Selection};

use boot::render_scroll;
use dock::render_dock;
use overlay::render_slash_overlay;
use overlay_at::render_at_overlay;
use paint::fill_bg;
use selection::apply_highlight;
use sidebar::render_sidebar;
use theme::{BG, DOCK_HEIGHT, SIDEBAR_WIDTH};

pub struct WholeScreen<'a> {
    state: &'a SceneState,
    scroll_offset: u16,
    selection: Option<Selection>,
    slash_idx: usize,
    at_idx: usize,
    tick: u32,
}

impl<'a> WholeScreen<'a> {
    pub fn new(state: &'a SceneState) -> Self {
        Self {
            state,
            scroll_offset: 0,
            selection: None,
            slash_idx: 0,
            at_idx: 0,
            tick: 0,
        }
    }

    pub fn with_tick(mut self, tick: u32) -> Self {
        self.tick = tick;
        self
    }

    pub fn with_scroll(mut self, offset: u16) -> Self {
        self.scroll_offset = offset;
        self
    }

    pub fn with_selection(mut self, sel: Option<Selection>) -> Self {
        self.selection = sel;
        self
    }

    pub fn with_slash_index(mut self, idx: usize) -> Self {
        self.slash_idx = idx;
        self
    }

    pub fn with_at_index(mut self, idx: usize) -> Self {
        self.at_idx = idx;
        self
    }
}

impl Widget for WholeScreen<'_> {
    fn render(self, area: Rect, buf: &mut Buffer) {
        fill_bg(buf, area, BG);
        let (main, side) = split_main_sidebar(area);
        render_main(
            buf,
            main,
            self.state,
            self.scroll_offset,
            self.slash_idx,
            self.at_idx,
            self.tick,
        );
        if side.width > 0 {
            render_sidebar(buf, side, self.state);
        }
        if let Some(sel) = self.selection {
            let layout = cards_layout(area, self.state, self.scroll_offset);
            apply_highlight(buf, &layout, sel);
        }
    }
}

fn split_main_sidebar(area: Rect) -> (Rect, Rect) {
    if area.width <= SIDEBAR_WIDTH + 30 {
        return (area, Rect::new(area.x, area.y, 0, area.height));
    }
    let main_w = area.width - SIDEBAR_WIDTH;
    let main = Rect::new(area.x, area.y, main_w, area.height);
    let side = Rect::new(area.x + main_w, area.y, SIDEBAR_WIDTH, area.height);
    (main, side)
}

fn render_main(
    buf: &mut Buffer,
    area: Rect,
    state: &SceneState,
    scroll_offset: u16,
    slash_idx: usize,
    at_idx: usize,
    tick: u32,
) {
    let dock_h = DOCK_HEIGHT.min(area.height);
    let scroll_h = area.height.saturating_sub(dock_h);
    let scroll = Rect::new(area.x, area.y, area.width, scroll_h);
    let dock = Rect::new(area.x, area.y + scroll_h, area.width, dock_h);
    render_scroll(buf, scroll, state, scroll_offset, tick);
    render_dock(buf, dock, state, tick);
    render_slash_overlay(buf, dock, state, slash_idx);
    render_at_overlay(buf, dock, state, at_idx);
}
