use ratatui::backend::TestBackend;
use ratatui::Terminal;

use reasonix_render::state::{
    EditMode, SceneCard, SceneState, SessionItem, SetupState, SlashMatch, ToolStatus,
};
use reasonix_render::view::{render_setup, render_trace};

fn draw_trace(state: &SceneState, cols: u16, rows: u16) -> String {
    let backend = TestBackend::new(cols, rows);
    let mut terminal = Terminal::new(backend).unwrap();
    terminal.draw(|f| render_trace(state, f)).unwrap();
    let buffer = terminal.backend().buffer().clone();
    let mut out = String::new();
    for y in 0..buffer.area.height {
        for x in 0..buffer.area.width {
            out.push_str(buffer[(x, y)].symbol());
        }
        out.push('\n');
    }
    out
}

fn draw_setup(state: &SetupState, cols: u16, rows: u16) -> String {
    let backend = TestBackend::new(cols, rows);
    let mut terminal = Terminal::new(backend).unwrap();
    terminal.draw(|f| render_setup(state, f)).unwrap();
    let buffer = terminal.backend().buffer().clone();
    let mut out = String::new();
    for y in 0..buffer.area.height {
        for x in 0..buffer.area.width {
            out.push_str(buffer[(x, y)].symbol());
        }
        out.push('\n');
    }
    out
}

#[test]
fn empty_state_shows_reasonix_logo_and_boot_fields() {
    let state = SceneState {
        model: Some("deepseek-chat".to_string()),
        cwd: Some("/work/reasonix".to_string()),
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("██████╗"), "missing logo");
    assert!(rendered.contains("DeepSeek code agent"));
    assert!(rendered.contains("model"));
    assert!(rendered.contains("deepseek-chat"));
    assert!(rendered.contains("workdir"));
    assert!(rendered.contains("/work/reasonix"));
    assert!(rendered.contains("tools"));
}

#[test]
fn cards_replace_boot_block_in_scroll_area() {
    let state = SceneState {
        cards: vec![
            SceneCard {
                kind: "user".to_string(),
                summary: "hello".to_string(),
                body: Some("hello".to_string()),
                ts: Some(1_700_000_000_000),
                ..Default::default()
            },
            SceneCard {
                kind: "streaming".to_string(),
                summary: "hi back".to_string(),
                body: Some("hi back".to_string()),
                ..Default::default()
            },
        ],
        card_count: 2,
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("YOU"));
    assert!(rendered.contains("hello"));
    assert!(rendered.contains("reasonix"));
    assert!(rendered.contains("hi back"));
    assert!(!rendered.contains("██████╗"), "boot block should be hidden");
}

#[test]
fn composer_renders_with_cursor_block_at_offset() {
    let state = SceneState {
        composer_text: Some("hello".to_string()),
        composer_cursor: Some(2),
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("he▮llo"));
}

#[test]
fn composer_placeholder_shown_when_empty() {
    let state = SceneState::default();
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("type to chat"));
    assert!(rendered.contains("for commands"));
}

#[test]
fn meta_row_shows_kbd_shortcuts() {
    let state = SceneState::default();
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("send"));
    assert!(rendered.contains("newline"));
    assert!(rendered.contains("cmd"));
    assert!(rendered.contains("file"));
    assert!(rendered.contains("shell"));
    assert!(rendered.contains("esc"));
    assert!(rendered.contains("cancel"));
    assert!(rendered.contains("history"));
}

#[test]
fn status_bar_carries_brand_model_wallet_cwd() {
    let state = SceneState {
        model: Some("deepseek-chat".to_string()),
        wallet_balance: Some(184.2),
        wallet_currency: Some("CNY".to_string()),
        cwd: Some("/workspace/reasonix-core".to_string()),
        ..Default::default()
    };
    let rendered = draw_trace(&state, 120, 30);
    assert!(rendered.contains("reasonix"));
    assert!(rendered.contains("deepseek-chat"));
    assert!(rendered.contains("¥184.20"));
    assert!(rendered.contains("reasonix-core"));
}

#[test]
fn status_bar_busy_idle_with_activity() {
    let state = SceneState {
        busy: true,
        activity: Some("awaiting tools".to_string()),
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("busy"));
    assert!(rendered.contains("awaiting tools"));
}

#[test]
fn edit_mode_shows_in_status_bar() {
    let state = SceneState {
        edit_mode: Some(EditMode::Yolo),
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("mode"));
    assert!(rendered.contains("yolo"));
}

#[test]
fn approval_replaces_composer_row_with_y_n_stub() {
    let state = SceneState {
        approval_kind: Some("shell".to_string()),
        approval_prompt: Some("rm -rf /tmp/x".to_string()),
        composer_text: Some("typing…".to_string()),
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("❓"));
    assert!(rendered.contains("[shell]"));
    assert!(rendered.contains("rm -rf /tmp/x"));
    assert!(rendered.contains("[y/n]"));
    assert!(!rendered.contains("typing…"));
}

#[test]
fn slash_overlay_appears_above_composer_with_selection_marker() {
    let state = SceneState {
        slash_matches: Some(vec![
            SlashMatch {
                cmd: "/help".to_string(),
                summary: "show help".to_string(),
                args_hint: None,
            },
            SlashMatch {
                cmd: "/model".to_string(),
                summary: "switch model".to_string(),
                args_hint: Some("<name>".to_string()),
            },
        ]),
        slash_selected_index: Some(1),
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("/help"));
    assert!(rendered.contains("/model"));
    assert!(rendered.contains("<name>"));
    assert!(rendered.contains("switch model"));
    assert!(rendered.contains("▸"));
}

#[test]
fn sessions_picker_shows_header_rows_and_hint() {
    let state = SceneState {
        sessions: Some(vec![
            SessionItem {
                title: "feat-foo".to_string(),
                meta: Some("main · 12 turns".to_string()),
            },
            SessionItem {
                title: "spike-bar".to_string(),
                meta: Some("release/4.5".to_string()),
            },
        ]),
        sessions_focused_index: Some(0),
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("sessions"));
    assert!(rendered.contains("2 saved"));
    assert!(rendered.contains("feat-foo"));
    assert!(rendered.contains("spike-bar"));
    assert!(rendered.contains("navigate"));
}

#[test]
fn tool_card_uses_rich_arrow_args_status_format() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "tool".to_string(),
            summary: "Read".to_string(),
            args: Some("src/parser.ts".to_string()),
            status: Some(ToolStatus::Ok),
            elapsed: Some("120ms".to_string()),
            id: Some("#a4f1".to_string()),
            ..Default::default()
        }],
        card_count: 1,
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("▸"));
    assert!(rendered.contains("Read"));
    assert!(rendered.contains("(src/parser.ts)"));
    assert!(rendered.contains("✓"));
    assert!(rendered.contains("120ms"));
    assert!(rendered.contains("#a4f1"));
}

#[test]
fn tool_card_err_status_shows_x_glyph() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "tool".to_string(),
            summary: "Bash".to_string(),
            args: Some("false".to_string()),
            status: Some(ToolStatus::Err),
            ..Default::default()
        }],
        card_count: 1,
        ..Default::default()
    };
    let rendered = draw_trace(&state, 100, 30);
    assert!(rendered.contains("✗"));
}

#[test]
fn setup_renders_welcome_and_masked_dots() {
    let state = SetupState {
        buffer_length: 4,
        error: None,
    };
    let rendered = draw_setup(&state, 80, 24);
    assert!(rendered.contains("REASONIX"));
    assert!(rendered.contains("welcome"));
    assert!(rendered.contains("API key"));
    assert!(rendered.contains("••••"));
    assert!(rendered.contains("▮"));
    assert!(rendered.contains("Ctrl+C"));
}

#[test]
fn setup_with_error_renders_error_row() {
    let state = SetupState {
        buffer_length: 0,
        error: Some("key malformed".to_string()),
    };
    let rendered = draw_setup(&state, 80, 24);
    assert!(rendered.contains("✗"));
    assert!(rendered.contains("key malformed"));
}
