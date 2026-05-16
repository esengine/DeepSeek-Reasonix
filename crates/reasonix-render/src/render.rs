use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color as RColor, Modifier, Style};
use unicode_width::UnicodeWidthChar;

use crate::scene::{
    BoxLayout, Color, Dim, FillToken, FlexDirection, NamedColor, SceneFrame, SceneNode, TextRun,
    TextStyle,
};

#[derive(Clone, Copy)]
enum Axis {
    Row,
    Column,
}

pub fn render_frame(frame: &SceneFrame, buf: &mut Buffer, area: Rect) {
    render_node(&frame.root, buf, area);
}

fn render_node(node: &SceneNode, buf: &mut Buffer, area: Rect) {
    if area.width == 0 || area.height == 0 {
        return;
    }
    match node {
        SceneNode::Text { runs, .. } => render_text_runs(runs, buf, area),
        SceneNode::Box { layout, children } => {
            render_box(layout.as_ref(), children, buf, area);
        }
    }
}

fn render_text_runs(runs: &[TextRun], buf: &mut Buffer, area: Rect) {
    let mut x = area.x;
    let max_x = area.x.saturating_add(area.width);
    for run in runs {
        if x >= max_x {
            break;
        }
        let style = ratatui_style(run.style.as_ref());
        for ch in run.text.chars() {
            let w = display_width(ch);
            if w == 0 {
                continue;
            }
            if x.saturating_add(w) > max_x {
                break;
            }
            let cell = &mut buf[(x, area.y)];
            cell.set_char(ch);
            cell.set_style(style);
            x = x.saturating_add(w);
        }
    }
}

fn display_width(ch: char) -> u16 {
    UnicodeWidthChar::width(ch).unwrap_or(0) as u16
}

fn render_box(layout: Option<&BoxLayout>, children: &[SceneNode], buf: &mut Buffer, area: Rect) {
    let inner = apply_padding(layout, area);
    if inner.width == 0 || inner.height == 0 {
        return;
    }
    let direction = layout
        .and_then(|l| l.direction)
        .unwrap_or(FlexDirection::Column);
    let gap = layout
        .and_then(|l| l.gap)
        .filter(|g| *g >= 0)
        .map(|g| g as u16)
        .unwrap_or(0);
    match direction {
        FlexDirection::Column => render_column(children, gap, buf, inner),
        FlexDirection::Row => render_row(children, gap, buf, inner),
    }
}

fn render_column(children: &[SceneNode], gap: u16, buf: &mut Buffer, area: Rect) {
    let sizes = compute_axis_sizes(children, gap, area.height, Axis::Column);
    let mut y = area.y;
    let max_y = area.y.saturating_add(area.height);
    let last = children.len().saturating_sub(1);
    for (i, child) in children.iter().enumerate() {
        if y >= max_y {
            break;
        }
        let h = sizes[i].min(max_y - y);
        let child_area = Rect {
            x: area.x,
            y,
            width: area.width,
            height: h,
        };
        render_node(child, buf, child_area);
        y = y.saturating_add(h);
        if i < last && gap > 0 {
            y = y.saturating_add(gap);
        }
    }
}

fn render_row(children: &[SceneNode], gap: u16, buf: &mut Buffer, area: Rect) {
    let sizes = compute_axis_sizes(children, gap, area.width, Axis::Row);
    let mut x = area.x;
    let max_x = area.x.saturating_add(area.width);
    let last = children.len().saturating_sub(1);
    for (i, child) in children.iter().enumerate() {
        if x >= max_x {
            break;
        }
        let w = sizes[i].min(max_x - x);
        let child_area = Rect {
            x,
            y: area.y,
            width: w,
            height: area.height,
        };
        render_node(child, buf, child_area);
        x = x.saturating_add(w);
        if i < last && gap > 0 {
            x = x.saturating_add(gap);
        }
    }
}

fn compute_axis_sizes(children: &[SceneNode], gap: u16, total: u16, axis: Axis) -> Vec<u16> {
    let n = children.len();
    if n == 0 {
        return vec![];
    }
    let gap_total = gap.saturating_mul((n.saturating_sub(1)) as u16);
    let mut available = total.saturating_sub(gap_total);
    let mut sizes = vec![0u16; n];
    let mut fill_indices: Vec<usize> = Vec::new();

    for (i, child) in children.iter().enumerate() {
        match axis_dim(child, axis) {
            Some(Dim::Cells(c)) => {
                let want = c.max(0) as u16;
                let take = want.min(available);
                sizes[i] = take;
                available = available.saturating_sub(take);
            }
            Some(Dim::Fill(FillToken::Fill)) => {
                fill_indices.push(i);
            }
            None => {
                let want = match axis {
                    Axis::Row => intrinsic_width(child),
                    Axis::Column => intrinsic_height(child),
                };
                let take = want.min(available);
                sizes[i] = take;
                available = available.saturating_sub(take);
            }
        }
    }

    if !fill_indices.is_empty() && available > 0 {
        let count = fill_indices.len() as u16;
        let per = available / count;
        let remainder = available % count;
        for (j, &i) in fill_indices.iter().enumerate() {
            sizes[i] = per + if (j as u16) < remainder { 1 } else { 0 };
        }
    }

    sizes
}

fn axis_dim(node: &SceneNode, axis: Axis) -> Option<Dim> {
    let layout = match node {
        SceneNode::Box { layout, .. } => layout.as_ref()?,
        SceneNode::Text { .. } => return None,
    };
    let dim = match axis {
        Axis::Row => layout.width.as_ref(),
        Axis::Column => layout.height.as_ref(),
    };
    dim.cloned()
}

fn apply_padding(layout: Option<&BoxLayout>, area: Rect) -> Rect {
    let px = layout
        .and_then(|l| l.padding_x)
        .filter(|p| *p >= 0)
        .map(|p| p as u16)
        .unwrap_or(0);
    let py = layout
        .and_then(|l| l.padding_y)
        .filter(|p| *p >= 0)
        .map(|p| p as u16)
        .unwrap_or(0);
    let shrink_w = px.saturating_mul(2);
    let shrink_h = py.saturating_mul(2);
    Rect {
        x: area.x.saturating_add(px),
        y: area.y.saturating_add(py),
        width: area.width.saturating_sub(shrink_w),
        height: area.height.saturating_sub(shrink_h),
    }
}

fn intrinsic_height(node: &SceneNode) -> u16 {
    match node {
        SceneNode::Text { .. } => 1,
        SceneNode::Box {
            layout, children, ..
        } => {
            let py = layout
                .as_ref()
                .and_then(|l| l.padding_y)
                .filter(|p| *p >= 0)
                .map(|p| p as u16)
                .unwrap_or(0);
            let gap = layout
                .as_ref()
                .and_then(|l| l.gap)
                .filter(|g| *g >= 0)
                .map(|g| g as u16)
                .unwrap_or(0);
            let direction = layout
                .as_ref()
                .and_then(|l| l.direction)
                .unwrap_or(FlexDirection::Column);
            let body = match direction {
                FlexDirection::Column => {
                    let mut total: u16 = 0;
                    for (i, c) in children.iter().enumerate() {
                        total = total.saturating_add(intrinsic_height(c));
                        if i + 1 < children.len() {
                            total = total.saturating_add(gap);
                        }
                    }
                    total
                }
                FlexDirection::Row => children.iter().map(intrinsic_height).max().unwrap_or(0),
            };
            body.saturating_add(py.saturating_mul(2))
        }
    }
}

fn intrinsic_width(node: &SceneNode) -> u16 {
    match node {
        SceneNode::Text { runs, .. } => {
            let cells: usize = runs
                .iter()
                .flat_map(|r| r.text.chars())
                .map(|c| display_width(c) as usize)
                .sum();
            cells.min(u16::MAX as usize) as u16
        }
        SceneNode::Box {
            layout, children, ..
        } => {
            let px = layout
                .as_ref()
                .and_then(|l| l.padding_x)
                .filter(|p| *p >= 0)
                .map(|p| p as u16)
                .unwrap_or(0);
            let gap = layout
                .as_ref()
                .and_then(|l| l.gap)
                .filter(|g| *g >= 0)
                .map(|g| g as u16)
                .unwrap_or(0);
            let direction = layout
                .as_ref()
                .and_then(|l| l.direction)
                .unwrap_or(FlexDirection::Column);
            let body = match direction {
                FlexDirection::Row => {
                    let mut total: u16 = 0;
                    for (i, c) in children.iter().enumerate() {
                        total = total.saturating_add(intrinsic_width(c));
                        if i + 1 < children.len() {
                            total = total.saturating_add(gap);
                        }
                    }
                    total
                }
                FlexDirection::Column => children.iter().map(intrinsic_width).max().unwrap_or(0),
            };
            body.saturating_add(px.saturating_mul(2))
        }
    }
}

fn ratatui_style(style: Option<&TextStyle>) -> Style {
    let Some(s) = style else {
        return Style::default();
    };
    let mut out = Style::default();
    if let Some(c) = s.color.as_ref() {
        out = out.fg(color_to_ratatui(c));
    }
    if let Some(c) = s.bg.as_ref() {
        out = out.bg(color_to_ratatui(c));
    }
    if s.bold == Some(true) {
        out = out.add_modifier(Modifier::BOLD);
    }
    if s.dim == Some(true) {
        out = out.add_modifier(Modifier::DIM);
    }
    if s.italic == Some(true) {
        out = out.add_modifier(Modifier::ITALIC);
    }
    if s.underline == Some(true) {
        out = out.add_modifier(Modifier::UNDERLINED);
    }
    if s.inverse == Some(true) {
        out = out.add_modifier(Modifier::REVERSED);
    }
    if s.strikethrough == Some(true) {
        out = out.add_modifier(Modifier::CROSSED_OUT);
    }
    out
}

fn color_to_ratatui(c: &Color) -> RColor {
    match c {
        Color::Named(n) => match n {
            NamedColor::Default => RColor::Reset,
            NamedColor::Black => RColor::Black,
            NamedColor::Red => RColor::Red,
            NamedColor::Green => RColor::Green,
            NamedColor::Yellow => RColor::Yellow,
            NamedColor::Blue => RColor::Blue,
            NamedColor::Magenta => RColor::Magenta,
            NamedColor::Cyan => RColor::Cyan,
            NamedColor::White => RColor::White,
            NamedColor::Gray => RColor::Gray,
        },
        Color::Hex { hex } => parse_hex(hex).unwrap_or(RColor::Reset),
    }
}

fn parse_hex(hex: &str) -> Option<RColor> {
    let s = hex.strip_prefix('#')?;
    if s.len() != 6 {
        return None;
    }
    let r = u8::from_str_radix(&s[0..2], 16).ok()?;
    let g = u8::from_str_radix(&s[2..4], 16).ok()?;
    let b = u8::from_str_radix(&s[4..6], 16).ok()?;
    Some(RColor::Rgb(r, g, b))
}
