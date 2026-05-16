use chrono::{Local, TimeZone};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::{Color as RColor, Modifier, Style};
use ratatui::text::{Line, Span, Text};
use ratatui::widgets::{Block, Padding, Paragraph, Wrap};
use ratatui::Frame;

use crate::state::{
    EditMode, SceneCard, SceneState, SessionItem, SetupState, SlashMatch, ToolStatus,
};
use crate::theme::{palette, Color, NamedColor};

const MAX_CARD_BODY_LINES: usize = 5;
const MAX_SLASH_ROWS: usize = 6;
const MAX_SESSION_ROWS: usize = 8;
const APPROVAL_PROMPT_MAX: usize = 60;

const LOGO_LINES: [&str; 6] = [
    "██████╗ ███████╗ █████╗ ███████╗ ██████╗ ███╗   ██╗██╗██╗  ██╗",
    "██╔══██╗██╔════╝██╔══██╗██╔════╝██╔═══██╗████╗  ██║██║╚██╗██╔╝",
    "██████╔╝█████╗  ███████║███████╗██║   ██║██╔██╗ ██║██║ ╚███╔╝ ",
    "██╔══██╗██╔══╝  ██╔══██║╚════██║██║   ██║██║╚██╗██║██║ ██╔██╗ ",
    "██║  ██║███████╗██║  ██║███████║╚██████╔╝██║ ╚████║██║██╔╝ ██╗",
    "╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝╚═╝  ╚═╝",
];

pub fn render_trace(state: &SceneState, frame: &mut Frame<'_>) {
    let area = frame.area();
    frame.render_widget(canvas_block(), area);
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Fill(1), Constraint::Length(dock_height(state))])
        .split(area);
    render_scroll(state, frame, chunks[0]);
    render_dock(state, frame, chunks[1]);
}

pub fn render_setup(state: &SetupState, frame: &mut Frame<'_>) {
    let area = frame.area();
    frame.render_widget(canvas_block(), area);
    let mut lines: Vec<Line<'_>> = Vec::new();
    lines.push(Line::from(vec![
        styled(" ● ", palette::ds(), Modifier::BOLD),
        styled("REASONIX", palette::ds_bright(), Modifier::BOLD),
        styled("  welcome", palette::fg2(), Modifier::empty()),
    ]));
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
    let paragraph = Paragraph::new(Text::from(lines)).wrap(Wrap { trim: false });
    frame.render_widget(paragraph, area);
}

fn canvas_block() -> Block<'static> {
    Block::default().style(Style::default().bg(to_rcolor(palette::bg())))
}

fn dock_height(state: &SceneState) -> u16 {
    let mut overlay = 0u16;
    if let Some(list) = state.sessions.as_ref() {
        if !list.is_empty() {
            let shown = list.len().min(MAX_SESSION_ROWS) as u16;
            let header = 1u16;
            let hint = 1u16;
            let overflow = if list.len() > MAX_SESSION_ROWS { 1 } else { 0 };
            overlay = header + shown + overflow + hint;
        }
    } else if let Some(matches) = state.slash_matches.as_ref() {
        if !matches.is_empty() && state.approval_prompt.is_none() {
            let shown = matches.len().min(MAX_SLASH_ROWS) as u16;
            let header = 1u16;
            let overflow = if matches.len() > MAX_SLASH_ROWS { 1 } else { 0 };
            overlay = header + shown + overflow;
        }
    }
    overlay + 3
}

fn render_scroll(state: &SceneState, frame: &mut Frame<'_>, area: Rect) {
    let text = if state.cards.is_empty() {
        Text::from(boot_lines(state))
    } else {
        Text::from(scroll_lines(state, area.height as usize))
    };
    let block = Block::default()
        .padding(Padding::new(2, 2, 1, 1))
        .style(Style::default().bg(to_rcolor(palette::bg())));
    let paragraph = Paragraph::new(text).wrap(Wrap { trim: false }).block(block);
    frame.render_widget(paragraph, area);
}

fn boot_lines(state: &SceneState) -> Vec<Line<'_>> {
    let mut lines: Vec<Line<'_>> = Vec::new();
    lines.push(Line::raw(""));
    for logo in LOGO_LINES.iter() {
        lines.push(Line::from(styled(
            (*logo).to_string(),
            palette::ds(),
            Modifier::BOLD,
        )));
    }
    lines.push(Line::raw(""));
    lines.push(Line::from(vec![
        styled(
            " DeepSeek code agent  ",
            palette::fg(),
            Modifier::empty(),
        ),
        styled(
            "· terminal-native, cache-first ·",
            palette::fg2(),
            Modifier::empty(),
        ),
    ]));
    lines.push(Line::raw(""));
    if let Some(model) = state.model.as_deref() {
        lines.push(boot_field("model", model, palette::ds_bright()));
    }
    if let Some(cwd) = state.cwd.as_deref() {
        lines.push(boot_field("workdir", cwd, palette::fg()));
    }
    if let Some(n) = state.mcp_server_count {
        if n > 0 {
            lines.push(boot_field(
                "mcp",
                &format!("{} server(s) connected", n),
                palette::fg(),
            ));
        }
    }
    lines.push(boot_field(
        "tools",
        "read · write · edit · bash · grep · fetch · todo",
        palette::fg(),
    ));
    lines.push(Line::raw(""));
    lines.push(Line::from(vec![
        Span::raw(" "),
        styled("type to chat  ", palette::fg2(), Modifier::empty()),
        styled("·  ", palette::fg3(), Modifier::empty()),
        styled("/", palette::ds(), Modifier::BOLD),
        styled(" commands  ", palette::fg2(), Modifier::empty()),
        styled("·  ", palette::fg3(), Modifier::empty()),
        styled("@", palette::ds(), Modifier::BOLD),
        styled(" file refs  ", palette::fg2(), Modifier::empty()),
        styled("·  ", palette::fg3(), Modifier::empty()),
        styled("!", palette::ds(), Modifier::BOLD),
        styled(" shell  ", palette::fg2(), Modifier::empty()),
        styled("·  ", palette::fg3(), Modifier::empty()),
        styled("Ctrl+C", palette::ds(), Modifier::BOLD),
        styled(" cancel  ", palette::fg2(), Modifier::empty()),
        styled("·  ", palette::fg3(), Modifier::empty()),
        styled("Ctrl+D", palette::ds(), Modifier::BOLD),
        styled(" exit", palette::fg2(), Modifier::empty()),
    ]));
    lines
}

fn boot_field(key: &str, value: &str, value_color: Color) -> Line<'static> {
    Line::from(vec![
        styled(
            format!(" {:<10}", key),
            palette::fg2(),
            Modifier::empty(),
        ),
        styled(value.to_string(), value_color, Modifier::empty()),
    ])
}

fn scroll_lines(state: &SceneState, available_height: usize) -> Vec<Line<'_>> {
    let mut lines: Vec<Line<'_>> = Vec::new();
    for card in &state.cards {
        append_card_lines(&mut lines, card);
    }
    let inner_height = available_height.saturating_sub(2);
    if lines.len() > inner_height && inner_height > 0 {
        let drop = lines.len() - inner_height;
        lines.drain(0..drop);
    }
    lines
}

fn append_card_lines(out: &mut Vec<Line<'_>>, card: &SceneCard) {
    match card.kind.as_str() {
        "tool" => out.push(tool_card_line(card)),
        "user" | "reasoning" | "streaming" => append_message_card(out, card),
        _ => out.push(generic_card_line(card)),
    }
}

fn append_message_card(out: &mut Vec<Line<'_>>, card: &SceneCard) {
    let color = color_for(&card.kind);
    let label = kind_label(&card.kind).unwrap_or(&card.kind).to_string();
    let mut head_spans: Vec<Span<'_>> = vec![
        styled(glyph_for(&card.kind).to_string(), color.clone(), Modifier::BOLD),
        Span::raw(" "),
        styled(label, color, Modifier::BOLD),
    ];
    let mut right: Vec<Span<'_>> = Vec::new();
    if let Some(meta) = card.meta.as_deref() {
        right.push(styled(meta.to_string(), palette::fg3(), Modifier::empty()));
    }
    if let Some(ts) = card.ts {
        if !right.is_empty() {
            right.push(styled("  ·  ", palette::fg3(), Modifier::empty()));
        }
        right.push(styled(format_ts(ts), palette::fg3(), Modifier::empty()));
    }
    if !right.is_empty() {
        head_spans.push(Span::raw("    "));
        head_spans.extend(right);
    }
    out.push(Line::from(head_spans));
    let body_source = card.body.clone().unwrap_or_else(|| card.summary.clone());
    for body in body_lines(&body_source) {
        let style = if card.kind == "reasoning" {
            Style::default()
                .fg(to_rcolor(palette::fg1()))
                .add_modifier(Modifier::ITALIC)
        } else {
            Style::default().fg(to_rcolor(palette::fg()))
        };
        out.push(Line::from(Span::styled(format!("  {}", body), style)));
    }
    out.push(Line::raw(""));
}

fn body_lines(body: &str) -> Vec<String> {
    let mut out = Vec::new();
    for raw in body.split('\n') {
        let line = raw.trim_end();
        if line.is_empty() {
            continue;
        }
        out.push(line.to_string());
        if out.len() >= MAX_CARD_BODY_LINES {
            break;
        }
    }
    out
}

fn tool_card_line(card: &SceneCard) -> Line<'static> {
    let mut spans: Vec<Span<'_>> = vec![
        styled("▸ ", palette::fg2(), Modifier::empty()),
        styled(
            if card.summary.is_empty() {
                "tool".to_string()
            } else {
                card.summary.clone()
            },
            palette::fg(),
            Modifier::BOLD,
        ),
    ];
    if let Some(args) = card.args.as_deref() {
        spans.push(styled(" (", palette::fg2(), Modifier::empty()));
        spans.push(styled(
            args.to_string(),
            palette::ds_bright(),
            Modifier::empty(),
        ));
        spans.push(styled(")", palette::fg2(), Modifier::empty()));
    }
    spans.push(Span::raw("  "));
    match card.status {
        Some(ToolStatus::Ok) => spans.push(styled("✓", palette::ok(), Modifier::BOLD)),
        Some(ToolStatus::Err) => spans.push(styled("✗", palette::err(), Modifier::BOLD)),
        _ => spans.push(styled("…", palette::warn(), Modifier::empty())),
    }
    if let Some(elapsed) = card.elapsed.as_deref() {
        spans.push(styled(
            format!(" {}", elapsed),
            palette::fg2(),
            Modifier::empty(),
        ));
    }
    if let Some(id) = card.id.as_deref() {
        spans.push(styled(
            format!("  {}", id),
            palette::fg3(),
            Modifier::empty(),
        ));
    }
    Line::from(spans)
}

fn generic_card_line(card: &SceneCard) -> Line<'static> {
    let color = color_for(&card.kind);
    let mut spans: Vec<Span<'_>> = vec![
        styled(glyph_for(&card.kind).to_string(), color.clone(), Modifier::BOLD),
        Span::raw(" "),
    ];
    if let Some(label) = kind_label(&card.kind) {
        spans.push(styled(label.to_string(), color, Modifier::BOLD));
        spans.push(Span::raw("  "));
    }
    let body = if card.summary.is_empty() {
        card.kind.clone()
    } else {
        card.summary.clone()
    };
    spans.push(styled(body, palette::fg(), Modifier::empty()));
    Line::from(spans)
}

fn glyph_for(kind: &str) -> &'static str {
    match kind {
        "user" => "❯",
        "reasoning" | "streaming" | "plan" | "task" => "◆",
        "tool" => "▸",
        "diff" => "Δ",
        "error" => "✗",
        "warn" => "!",
        _ => "·",
    }
}

fn color_for(kind: &str) -> Color {
    match kind {
        "user" => palette::ds(),
        "reasoning" | "diff" | "plan" | "task" => palette::ds_purple(),
        "streaming" => palette::ok(),
        "tool" => palette::fg1(),
        "error" => palette::err(),
        "warn" => palette::warn(),
        _ => palette::fg2(),
    }
}

fn kind_label(kind: &str) -> Option<&'static str> {
    match kind {
        "user" => Some("YOU"),
        "reasoning" => Some("THINKING"),
        "streaming" => Some("reasonix"),
        _ => None,
    }
}

fn render_dock(state: &SceneState, frame: &mut Frame<'_>, area: Rect) {
    let has_sessions = state.sessions.as_ref().is_some_and(|s| !s.is_empty());
    let has_slash = state.slash_matches.as_ref().is_some_and(|s| !s.is_empty());
    let overlay_height = area.height.saturating_sub(3);
    let chunks = if overlay_height > 0 && (has_sessions || (has_slash && state.approval_prompt.is_none())) {
        Layout::default()
            .direction(Direction::Vertical)
            .constraints([
                Constraint::Length(overlay_height),
                Constraint::Length(1),
                Constraint::Length(1),
                Constraint::Length(1),
            ])
            .split(area)
    } else {
        Layout::default()
            .direction(Direction::Vertical)
            .constraints([
                Constraint::Length(0),
                Constraint::Length(1),
                Constraint::Length(1),
                Constraint::Length(1),
            ])
            .split(area)
    };
    if has_sessions {
        render_sessions_picker(state, frame, chunks[0]);
    } else if has_slash && state.approval_prompt.is_none() {
        render_slash_overlay(state, frame, chunks[0]);
    }
    if let Some(prompt) = state.approval_prompt.as_deref() {
        render_approval(state.approval_kind.as_deref(), prompt, frame, chunks[1]);
    } else {
        render_composer(state, frame, chunks[1]);
    }
    render_meta(frame, chunks[2]);
    render_status(state, frame, chunks[3]);
}

fn render_composer(state: &SceneState, frame: &mut Frame<'_>, area: Rect) {
    let bg = Block::default().style(Style::default().bg(to_rcolor(palette::bg2())));
    frame.render_widget(bg, area);
    let mut spans: Vec<Span<'_>> = vec![
        Span::raw(" "),
        styled("❯ ", palette::ds(), Modifier::BOLD),
    ];
    let t = state.composer_text.as_deref().unwrap_or("");
    if t.is_empty() {
        spans.push(styled(
            "type to chat · / for commands · @ for files",
            palette::fg2(),
            Modifier::empty(),
        ));
    } else {
        let total = t.chars().count();
        let cursor = state.composer_cursor.unwrap_or(total).min(total);
        let before: String = t.chars().take(cursor).collect();
        let after: String = t.chars().skip(cursor).collect();
        if !before.is_empty() {
            spans.push(styled(before, palette::fg(), Modifier::empty()));
        }
        spans.push(styled("▮", palette::ds(), Modifier::empty()));
        if !after.is_empty() {
            spans.push(styled(after, palette::fg(), Modifier::empty()));
        }
    }
    let p = Paragraph::new(Line::from(spans));
    frame.render_widget(p, area);
}

fn render_approval(kind: Option<&str>, prompt: &str, frame: &mut Frame<'_>, area: Rect) {
    let bg = Block::default().style(Style::default().bg(to_rcolor(palette::bg2())));
    frame.render_widget(bg, area);
    let clipped: String = if prompt.chars().count() > APPROVAL_PROMPT_MAX {
        let head: String = prompt.chars().take(APPROVAL_PROMPT_MAX - 1).collect();
        format!("{}…", head)
    } else {
        prompt.to_string()
    };
    let mut spans: Vec<Span<'_>> = vec![styled(" ❓ ", palette::warn(), Modifier::BOLD)];
    if let Some(kind) = kind {
        spans.push(styled(
            format!("[{}] ", kind),
            palette::fg2(),
            Modifier::empty(),
        ));
    }
    spans.push(styled(clipped, palette::fg(), Modifier::empty()));
    spans.push(styled("  [y/n]", palette::warn(), Modifier::BOLD));
    let p = Paragraph::new(Line::from(spans));
    frame.render_widget(p, area);
}

fn render_meta(frame: &mut Frame<'_>, area: Rect) {
    let left = Line::from(vec![
        Span::raw(" "),
        styled("↵", palette::ds(), Modifier::empty()),
        styled(" send  ", palette::fg2(), Modifier::empty()),
        styled("⇧↵", palette::ds(), Modifier::empty()),
        styled(" newline  ", palette::fg2(), Modifier::empty()),
        styled("/", palette::ds(), Modifier::empty()),
        styled(" cmd  ", palette::fg2(), Modifier::empty()),
        styled("@", palette::ds(), Modifier::empty()),
        styled(" file  ", palette::fg2(), Modifier::empty()),
        styled("!", palette::ds(), Modifier::empty()),
        styled(" shell", palette::fg2(), Modifier::empty()),
    ]);
    let right = Line::from(vec![
        styled("esc", palette::ds(), Modifier::empty()),
        styled(" cancel  ", palette::fg2(), Modifier::empty()),
        styled("↑", palette::ds(), Modifier::empty()),
        styled(" history ", palette::fg2(), Modifier::empty()),
    ]);
    render_row_split(frame, area, left, right);
}

fn render_status(state: &SceneState, frame: &mut Frame<'_>, area: Rect) {
    let bg = Block::default().style(Style::default().bg(to_rcolor(palette::bg2())));
    frame.render_widget(bg, area);
    let mut left_spans: Vec<Span<'_>> = vec![
        styled(" ●", palette::ok(), Modifier::empty()),
        styled(" reasonix", palette::fg(), Modifier::BOLD),
    ];
    if let Some(model) = state.model.as_deref() {
        left_spans.push(styled("  model ", palette::fg2(), Modifier::empty()));
        left_spans.push(styled(model.to_string(), palette::ds(), Modifier::empty()));
    }
    if let Some(mode) = state.edit_mode.as_ref() {
        let color = match mode {
            EditMode::Yolo => palette::err(),
            EditMode::Auto => palette::warn(),
            EditMode::Review => palette::ds(),
        };
        left_spans.push(styled("  mode ", palette::fg2(), Modifier::empty()));
        left_spans.push(styled(mode.as_str().to_string(), color, Modifier::BOLD));
    }
    left_spans.push(Span::raw("  "));
    left_spans.push(styled(
        if state.busy { "busy" } else { "idle" }.to_string(),
        if state.busy { palette::warn() } else { palette::ok() },
        Modifier::empty(),
    ));
    if let Some(activity) = state.activity.as_deref() {
        left_spans.push(styled(
            format!(" · {}", activity),
            palette::fg2(),
            Modifier::empty(),
        ));
    }

    let mut right_spans: Vec<Span<'_>> = Vec::new();
    if let Some(wallet) = format_wallet(state.wallet_balance, state.wallet_currency.as_deref()) {
        right_spans.push(styled("wallet ", palette::fg2(), Modifier::empty()));
        right_spans.push(styled(
            format!("{} ", wallet),
            palette::ok(),
            Modifier::BOLD,
        ));
    }
    if let Some(cwd) = state.cwd.as_deref() {
        right_spans.push(styled("cwd ", palette::fg2(), Modifier::empty()));
        right_spans.push(styled(
            format!("{} ", trunc_cwd(cwd)),
            palette::fg1(),
            Modifier::empty(),
        ));
    }

    render_row_split(frame, area, Line::from(left_spans), Line::from(right_spans));
}

fn render_row_split(frame: &mut Frame<'_>, area: Rect, left: Line<'_>, right: Line<'_>) {
    let right_width = right.width() as u16;
    let left_width = area.width.saturating_sub(right_width);
    let chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([Constraint::Length(left_width), Constraint::Length(right_width)])
        .split(area);
    frame.render_widget(Paragraph::new(left), chunks[0]);
    frame.render_widget(Paragraph::new(right), chunks[1]);
}

fn render_slash_overlay(state: &SceneState, frame: &mut Frame<'_>, area: Rect) {
    let matches = state.slash_matches.as_ref().unwrap();
    let sel = state
        .slash_selected_index
        .unwrap_or(0)
        .min(matches.len().saturating_sub(1));
    let (start, shown) = list_window(matches, sel, MAX_SLASH_ROWS);
    let mut lines: Vec<Line<'_>> = Vec::new();
    let plural = if matches.len() == 1 { "" } else { "es" };
    lines.push(Line::from(vec![
        Span::raw(" "),
        styled("/", palette::ds(), Modifier::BOLD),
        styled(" commands", palette::fg2(), Modifier::empty()),
        styled(
            format!("  {} match{}", matches.len(), plural),
            palette::fg3(),
            Modifier::empty(),
        ),
    ]));
    for (i, m) in shown.iter().enumerate() {
        lines.push(slash_line(m, start + i == sel));
    }
    let hidden = matches.len() - shown.len();
    if hidden > 0 {
        lines.push(overflow_line(hidden));
    }
    let p = Paragraph::new(Text::from(lines)).wrap(Wrap { trim: false });
    frame.render_widget(p, area);
}

fn slash_line(m: &SlashMatch, selected: bool) -> Line<'static> {
    let prefix = if selected { " ▸ " } else { "   " };
    let mut spans: Vec<Span<'_>> = vec![styled(
        prefix.to_string(),
        if selected { palette::ds() } else { palette::fg3() },
        Modifier::empty(),
    )];
    if selected {
        spans.push(styled(
            m.cmd.clone(),
            palette::ds_bright(),
            Modifier::BOLD,
        ));
    } else {
        spans.push(styled(m.cmd.clone(), palette::fg1(), Modifier::empty()));
    }
    if let Some(args) = m.args_hint.as_deref() {
        spans.push(styled(
            format!(" {}", args),
            palette::fg2(),
            Modifier::empty(),
        ));
    }
    if !m.summary.is_empty() {
        spans.push(Span::raw("  "));
        spans.push(styled(m.summary.clone(), palette::fg2(), Modifier::empty()));
    }
    Line::from(spans)
}

fn render_sessions_picker(state: &SceneState, frame: &mut Frame<'_>, area: Rect) {
    let list = state.sessions.as_ref().unwrap();
    let sel = state
        .sessions_focused_index
        .unwrap_or(0)
        .min(list.len().saturating_sub(1));
    let (start, shown) = list_window(list, sel, MAX_SESSION_ROWS);
    let mut lines: Vec<Line<'_>> = Vec::new();
    lines.push(Line::from(vec![
        Span::raw(" "),
        styled("◇", palette::ds(), Modifier::BOLD),
        styled(" sessions", palette::fg2(), Modifier::empty()),
        styled(
            format!("  {} saved", list.len()),
            palette::fg3(),
            Modifier::empty(),
        ),
    ]));
    for (i, s) in shown.iter().enumerate() {
        lines.push(session_line(s, start + i == sel));
    }
    let hidden = list.len() - shown.len();
    if hidden > 0 {
        lines.push(overflow_line(hidden));
    }
    lines.push(Line::from(vec![
        Span::raw(" "),
        styled("↑↓", palette::ds(), Modifier::empty()),
        styled(" navigate  ", palette::fg2(), Modifier::empty()),
        styled("⏎", palette::ds(), Modifier::empty()),
        styled(" open  ", palette::fg2(), Modifier::empty()),
        styled("n", palette::ds(), Modifier::empty()),
        styled(" new  ", palette::fg2(), Modifier::empty()),
        styled("esc", palette::ds(), Modifier::empty()),
        styled(" cancel", palette::fg2(), Modifier::empty()),
    ]));
    let p = Paragraph::new(Text::from(lines)).wrap(Wrap { trim: false });
    frame.render_widget(p, area);
}

fn session_line(item: &SessionItem, focused: bool) -> Line<'static> {
    let prefix = if focused { " ▸ " } else { "   " };
    let mut spans: Vec<Span<'_>> = vec![styled(
        prefix.to_string(),
        if focused { palette::ds() } else { palette::fg3() },
        Modifier::empty(),
    )];
    if focused {
        spans.push(styled(
            item.title.clone(),
            palette::ds_bright(),
            Modifier::BOLD,
        ));
    } else {
        spans.push(styled(
            item.title.clone(),
            palette::fg1(),
            Modifier::empty(),
        ));
    }
    if let Some(meta) = item.meta.as_deref() {
        spans.push(Span::raw("  "));
        spans.push(styled(meta.to_string(), palette::fg2(), Modifier::empty()));
    }
    Line::from(spans)
}

fn overflow_line(hidden: usize) -> Line<'static> {
    Line::from(styled(
        format!("   …{} more", hidden),
        palette::fg3(),
        Modifier::empty(),
    ))
}

fn list_window<T>(items: &[T], selected: usize, window_size: usize) -> (usize, &[T]) {
    if items.len() <= window_size {
        return (0, items);
    }
    let half = window_size / 2;
    let max_start = items.len() - window_size;
    let start = selected.saturating_sub(half).min(max_start);
    (start, &items[start..start + window_size])
}

fn format_ts(ts: i64) -> String {
    match Local.timestamp_millis_opt(ts) {
        chrono::offset::LocalResult::Single(dt) => dt.format("%H:%M:%S").to_string(),
        _ => String::new(),
    }
}

fn trunc_cwd(cwd: &str) -> String {
    if cwd.chars().count() <= 30 {
        return cwd.to_string();
    }
    let tail: String = cwd
        .chars()
        .rev()
        .take(29)
        .collect::<Vec<_>>()
        .into_iter()
        .rev()
        .collect();
    format!("…{}", tail)
}

fn format_wallet(total: Option<f64>, currency: Option<&str>) -> Option<String> {
    let total = total?;
    if !total.is_finite() {
        return None;
    }
    let symbol = currency_symbol(currency);
    Some(format!("{}{:.2}", symbol, total))
}

fn currency_symbol(currency: Option<&str>) -> String {
    match currency.map(|c| c.to_ascii_uppercase()) {
        Some(ref c) if c == "CNY" || c == "RMB" || c == "JPY" => "¥".to_string(),
        Some(ref c) if c == "USD" => "$".to_string(),
        Some(ref c) if c == "EUR" => "€".to_string(),
        Some(ref c) if c == "GBP" => "£".to_string(),
        Some(c) if !c.is_empty() => format!("{} ", c),
        _ => String::new(),
    }
}

fn styled<S: Into<String>>(text: S, color: Color, modifier: Modifier) -> Span<'static> {
    Span::styled(
        text.into(),
        Style::default().fg(to_rcolor(color)).add_modifier(modifier),
    )
}

pub(crate) fn to_rcolor(c: Color) -> RColor {
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
