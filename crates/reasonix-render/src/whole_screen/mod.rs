mod approval;
mod boot;
mod cards;
mod demo;
mod dock;
mod list_picker;
mod markdown;
mod md_render;
mod mode_picker;
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
pub use overlay::{
    slash_arg_completion, slash_arg_match_count, slash_completion, slash_is_exact,
    slash_match_count,
};
pub use overlay_at::{at_completion, at_match_count};
pub use paint::{paint, paint_str};
pub use selection::{cards_layout, extract_text, CardsLayout, ScrollbarGeom, Selection};

use approval::render_approval;
use boot::render_scroll;
use dock::render_dock;
use list_picker::render_list_picker;
use mode_picker::{mode_picker_options, preset_picker_options, render_picker};
use overlay::{render_slash_arg_overlay, render_slash_overlay};
use overlay_at::render_at_overlay;
use paint::fill_bg;
use selection::apply_highlight;
use sidebar::render_sidebar;
use theme::{BG, DOCK_HEIGHT, MAX_COMPOSER_ROWS, SIDEBAR_WIDTH};
use theme::{DS_BRIGHT, DS_PURPLE};

pub struct WholeScreen<'a> {
    state: &'a SceneState,
    scroll_offset: u16,
    selection: Option<Selection>,
    slash_idx: usize,
    slash_arg_idx: usize,
    at_idx: usize,
    approval_idx: usize,
    mode_picker_idx: Option<usize>,
    preset_picker_idx: Option<usize>,
    list_picker_idx: usize,
    sidebar_visible: bool,
    tick: u32,
}

impl<'a> WholeScreen<'a> {
    pub fn new(state: &'a SceneState) -> Self {
        Self {
            state,
            scroll_offset: 0,
            selection: None,
            slash_idx: 0,
            slash_arg_idx: 0,
            at_idx: 0,
            approval_idx: 0,
            mode_picker_idx: None,
            preset_picker_idx: None,
            list_picker_idx: 0,
            sidebar_visible: true,
            tick: 0,
        }
    }

    pub fn with_sidebar_visible(mut self, visible: bool) -> Self {
        self.sidebar_visible = visible;
        self
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

    pub fn with_slash_arg_index(mut self, idx: usize) -> Self {
        self.slash_arg_idx = idx;
        self
    }

    pub fn with_approval_index(mut self, idx: usize) -> Self {
        self.approval_idx = idx;
        self
    }

    pub fn with_mode_picker(mut self, idx: Option<usize>) -> Self {
        self.mode_picker_idx = idx;
        self
    }

    pub fn with_preset_picker(mut self, idx: Option<usize>) -> Self {
        self.preset_picker_idx = idx;
        self
    }

    pub fn with_list_picker_index(mut self, idx: usize) -> Self {
        self.list_picker_idx = idx;
        self
    }
}

impl Widget for WholeScreen<'_> {
    fn render(self, area: Rect, buf: &mut Buffer) {
        fill_bg(buf, area, BG);
        let (main, side) = split_main_sidebar(area, self.sidebar_visible);
        render_main(buf, main, self.state, self.scroll_offset, self.tick);
        if side.width > 0 {
            render_sidebar(buf, side, self.state);
        }
        if let Some(sel) = self.selection {
            let layout = cards_layout(area, self.state, self.scroll_offset, self.sidebar_visible);
            apply_highlight(buf, &layout, sel);
        }
        let dock_h = dock_height_for(self.state).min(area.height);
        let full_dock = Rect::new(area.x, area.y + area.height - dock_h, area.width, dock_h);
        render_slash_overlay(buf, full_dock, self.state, self.slash_idx);
        render_slash_arg_overlay(buf, full_dock, self.state, self.slash_arg_idx);
        render_at_overlay(buf, full_dock, self.state, self.at_idx);
        if let Some(approval) = self.state.approval.as_ref() {
            render_approval(buf, area, approval, self.approval_idx, dock_h);
        }
        if let Some(idx) = self.mode_picker_idx {
            let opts = mode_picker_options();
            render_picker(
                buf,
                full_dock,
                "AUTONOMY MODE",
                "controls when the agent asks before acting",
                DS_PURPLE,
                &opts,
                idx,
            );
        } else if let Some(idx) = self.preset_picker_idx {
            let opts = preset_picker_options();
            render_picker(
                buf,
                full_dock,
                "MODEL PRESET",
                "picks the default DeepSeek model bundle",
                DS_BRIGHT,
                &opts,
                idx,
            );
        }
        if let Some(picker) = self.state.list_picker.as_ref() {
            render_list_picker(buf, area, picker, self.list_picker_idx);
        }
    }
}

fn split_main_sidebar(area: Rect, sidebar_visible: bool) -> (Rect, Rect) {
    if !sidebar_visible || area.width <= SIDEBAR_WIDTH + 30 {
        return (area, Rect::new(area.x, area.y, 0, area.height));
    }
    let main_w = area.width - SIDEBAR_WIDTH;
    let main = Rect::new(area.x, area.y, main_w, area.height);
    let side = Rect::new(area.x + main_w, area.y, SIDEBAR_WIDTH, area.height);
    (main, side)
}

fn render_main(buf: &mut Buffer, area: Rect, state: &SceneState, scroll_offset: u16, tick: u32) {
    let dock_h = dock_height_for(state).min(area.height);
    let scroll_h = area.height.saturating_sub(dock_h);
    let scroll = Rect::new(area.x, area.y, area.width, scroll_h);
    let dock = Rect::new(area.x, area.y + scroll_h, area.width, dock_h);
    render_scroll(buf, scroll, state, scroll_offset, tick);
    render_dock(buf, dock, state, tick);
}

pub(super) fn dock_height_for(state: &SceneState) -> u16 {
    let text = state.composer_text.as_deref().unwrap_or("");
    let lines = if text.is_empty() {
        1u16
    } else {
        text.split('\n')
            .count()
            .clamp(1, MAX_COMPOSER_ROWS as usize) as u16
    };
    DOCK_HEIGHT + lines.saturating_sub(1)
}
