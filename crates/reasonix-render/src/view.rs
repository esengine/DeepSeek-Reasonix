use ratatui::style::{Color as RColor, Modifier, Style};
use ratatui::text::{Line, Span, Text};
use ratatui::widgets::{Block, Paragraph, Wrap};
use ratatui::Frame;

use crate::state::SetupState;
use crate::theme::{palette, Color, NamedColor};

pub fn render_setup(state: &SetupState, frame: &mut Frame<'_>) {
    let area = frame.area();
    frame.render_widget(canvas_block(), area);
    let mut lines: Vec<Line<'_>> = vec![Line::from(vec![
        styled(" ● ", palette::ds(), Modifier::BOLD),
        styled("REASONIX", palette::ds_bright(), Modifier::BOLD),
        styled("  welcome", palette::fg2(), Modifier::empty()),
    ])];
    lines.push(Line::raw(""));
    lines.push(Line::from(styled(
        " Enter your DeepSeek API key:",
        palette::ds(),
        Modifier::empty(),
    )));
    lines.push(Line::from(styled(
        "   get one at https://platform.deepseek.com",
        palette::fg2(),
        Modifier::empty(),
    )));
    let mut masked: Vec<Span<'_>> = vec![styled(" ❯ ", palette::ds(), Modifier::BOLD)];
    if state.buffer_length == 0 {
        masked.push(styled(
            "(start typing your key)",
            palette::fg2(),
            Modifier::empty(),
        ));
    } else {
        let dots = "•".repeat(state.buffer_length);
        masked.push(styled(dots, palette::fg(), Modifier::empty()));
        masked.push(styled("▮", palette::ds(), Modifier::empty()));
    }
    lines.push(Line::from(masked));
    if let Some(err) = state.error.as_deref() {
        lines.push(Line::from(vec![
            styled(" ✗ ", palette::err(), Modifier::BOLD),
            styled(err.to_string(), palette::err(), Modifier::empty()),
        ]));
    }
    lines.push(Line::raw(""));
    lines.push(Line::from(styled(
        " Ctrl+C to exit · /exit to quit",
        palette::fg2(),
        Modifier::empty(),
    )));
    let paragraph = Paragraph::new(Text::from(lines))
        .wrap(Wrap { trim: false })
        .style(Style::default().bg(to_rcolor(palette::bg())));
    frame.render_widget(paragraph, area);
}

fn canvas_block() -> Block<'static> {
    Block::default().style(Style::default().bg(to_rcolor(palette::bg())))
}

fn styled<S: Into<String>>(text: S, color: Color, modifier: Modifier) -> Span<'static> {
    Span::styled(
        text.into(),
        Style::default().fg(to_rcolor(color)).add_modifier(modifier),
    )
}

fn to_rcolor(c: Color) -> RColor {
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
        Color::Hex { hex } => parse_hex(&hex).unwrap_or(RColor::Reset),
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
