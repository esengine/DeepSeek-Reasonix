use ratatui::backend::TestBackend;
use ratatui::Terminal;
use reasonix_render::state::SetupState;
use reasonix_render::view::render_setup;

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
