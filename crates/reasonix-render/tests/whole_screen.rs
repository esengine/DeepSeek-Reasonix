use ratatui::backend::TestBackend;
use ratatui::Terminal;
use reasonix_render::state::{SceneCard, SceneState};
use reasonix_render::whole_screen::{
    at_completion, at_match_count, cards_layout, demo_state, extract_text, slash_arg_completion,
    slash_arg_match_count, slash_completion, slash_is_exact, slash_match_count, Selection,
    WholeScreen,
};
use unicode_width::UnicodeWidthStr;

fn draw(state: &SceneState, cols: u16, rows: u16) -> Vec<String> {
    let backend = TestBackend::new(cols, rows);
    let mut terminal = Terminal::new(backend).unwrap();
    terminal
        .draw(|f| {
            let area = f.area();
            f.render_widget(WholeScreen::new(state), area);
        })
        .unwrap();
    let buf = terminal.backend().buffer().clone();
    let mut out = Vec::with_capacity(buf.area.height as usize);
    for y in 0..buf.area.height {
        let mut line = String::new();
        let mut x = 0u16;
        while x < buf.area.width {
            let sym = buf[(x, y)].symbol();
            let w = UnicodeWidthStr::width(sym).max(1) as u16;
            line.push_str(sym);
            x = x.saturating_add(w);
        }
        out.push(line.trim_end().to_string());
    }
    out
}

fn joined(rows: &[String]) -> String {
    rows.join("\n")
}

#[test]
fn homepage_renders_logo_meta_dock_and_sidebar() {
    let rows = draw(&demo_state(), 120, 36);
    let all = joined(&rows);
    assert!(all.contains("██████╗"), "logo missing");
    assert!(all.contains("deepseek-v3.2-coder"), "model missing");
    assert!(all.contains("~/work/reasonix-core"), "workdir missing");
    assert!(all.contains("128k"), "context cap missing");
    assert!(all.contains("type to chat"), "hint missing");
    assert!(all.contains("MISSION CONTROL"), "sidebar header missing");
    assert!(all.contains("PLAN"), "sidebar PLAN missing");
    assert!(all.contains("JOBS"), "sidebar JOBS missing");
    assert!(all.contains("CHANGES"), "sidebar CHANGES missing");
    assert!(all.contains("SESSION"), "sidebar SESSION missing");
}

#[test]
fn homepage_input_box_drawn_with_corners() {
    let rows = draw(&demo_state(), 120, 36);
    let all = joined(&rows);
    assert!(all.contains("╭"), "input top-left corner missing");
    assert!(all.contains("╮"), "input top-right corner missing");
    assert!(all.contains("╰"), "input bottom-left corner missing");
    assert!(all.contains("╯"), "input bottom-right corner missing");
    assert!(all.contains("❯ "), "composer prompt missing");
    assert!(all.contains("type to chat"), "placeholder missing");
}

#[test]
fn homepage_status_bar_has_segments_and_ctx_meter() {
    let rows = draw(&demo_state(), 120, 36);
    // Status row carries "ctx" + a context bar + cost — find by ctx
    // since the brand cell now swaps to an activity label when busy.
    let last = rows
        .iter()
        .rev()
        .find(|r| r.contains("ctx") && r.contains("cache"))
        .expect("status row");
    assert!(last.contains("●"), "brand dot missing");
    assert!(last.contains("19.2k/128k"));
    assert!(last.contains("87%"));
    assert!(last.contains("$0.043"));
    assert!(last.contains("▰") && last.contains("▱"), "ctx bar segments");
}

#[test]
fn narrow_terminal_hides_sidebar_but_keeps_main_column() {
    let rows = draw(&demo_state(), 50, 20);
    let all = joined(&rows);
    assert!(all.contains("██████╗"), "logo still drawn");
    assert!(all.contains("❯ "), "composer still drawn");
    assert!(
        !all.contains("MISSION CONTROL"),
        "sidebar must collapse on narrow term"
    );
}

#[test]
fn diff_card_still_renders_inside_scroll_area() {
    let state = SceneState {
        model: Some("deepseek-v3.2-coder".to_string()),
        cwd: Some("~/work/reasonix-core".to_string()),
        cards: vec![SceneCard {
            kind: "diff".to_string(),
            summary: "src/utils/format.ts".to_string(),
            meta: Some("+5 -2".to_string()),
            body: Some(
                "@@ -16,4 +16,7 @@ formatToken\n\
                  export function formatToken(t: Token) {\n\
                 -  if (kind === \"text\") return value;\n\
                 +  switch (kind) {"
                    .to_string(),
            ),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 36);
    let all = joined(&rows);
    assert!(all.contains("± src/utils/format.ts"), "diff header missing");
    assert!(all.contains("+5"));
    assert!(all.contains("-2"));
    assert!(all.contains("@@ -16,4 +16,7 @@"));
    assert!(all.contains("if (kind"));
    assert!(all.contains("switch (kind)"));
}

#[test]
fn empty_narrow_area_does_not_panic() {
    let _ = draw(&SceneState::default(), 10, 6);
}

#[test]
fn demo_state_renders_user_thinking_tools_streaming() {
    let rows = draw(&demo_state(), 140, 80);
    let all = joined(&rows);
    assert!(all.contains("YOU"), "user header missing");
    assert!(all.contains("parser.ts"), "user body missing");
    assert!(all.contains("THINKING"), "reasoning header missing");
    assert!(all.contains("8 steps · 2.1s"), "reasoning meta missing");
    assert!(all.contains("PLAN"), "todo header missing");
    assert!(all.contains("[x]"), "done todo marker missing");
    assert!(all.contains("[~]"), "active todo marker missing");
    assert!(all.contains("[ ]"), "pending todo marker missing");
    assert!(all.contains("▸ Read"), "first tool missing");
    assert!(all.contains("#a4f1"), "tool id missing");
    assert!(all.contains("✓"), "tool ok glyph missing");
    assert!(all.contains("REASONIX"), "assistant header missing");
    assert!(all.contains("streaming…"), "streaming meta missing");
    assert!(all.contains("▊"), "streaming caret missing");
}

#[test]
fn sidebar_plan_populates_from_todo_card() {
    let rows = draw(&demo_state(), 140, 50);
    let all = joined(&rows);
    assert!(all.contains("2/5"), "plan progress count missing");
    assert!(all.contains("阅读 parser.ts"), "done item missing");
    assert!(all.contains("改写为 AsyncIterator"), "active item missing");
}

#[test]
fn sidebar_jobs_populates_from_tool_history() {
    let rows = draw(&demo_state(), 140, 50);
    let all = joined(&rows);
    assert!(all.contains("recent"), "jobs count missing");
    let jobs_row = rows
        .iter()
        .find(|r| r.contains("Grep"))
        .expect("grep job row");
    assert!(jobs_row.contains("✓"), "job glyph missing");
}

#[test]
fn slash_overlay_renders_when_composer_starts_with_slash() {
    let mut state = demo_state();
    state.composer_text = Some("/c".to_string());
    let rows = draw(&state, 140, 50);
    let all = joined(&rows);
    assert!(all.contains("SLASH COMMANDS"), "popup header missing");
    assert!(all.contains("/clear"), "/clear match missing");
    assert!(all.contains("/compact"), "/compact match missing");
    assert!(all.contains("/commit"), "/commit match missing");
    assert!(!all.contains("/undo"), "/undo should be filtered out");
}

#[test]
fn slash_overlay_hidden_when_no_slash() {
    let state = demo_state();
    let rows = draw(&state, 140, 50);
    let all = joined(&rows);
    assert!(
        !all.contains("SLASH COMMANDS"),
        "popup should not show without slash"
    );
}

fn draw_with_scroll(state: &SceneState, cols: u16, rows: u16, scroll: u16) -> Vec<String> {
    let backend = TestBackend::new(cols, rows);
    let mut terminal = Terminal::new(backend).unwrap();
    terminal
        .draw(|f| {
            let area = f.area();
            f.render_widget(WholeScreen::new(state).with_scroll(scroll), area);
        })
        .unwrap();
    let buf = terminal.backend().buffer().clone();
    let mut out = Vec::with_capacity(buf.area.height as usize);
    for y in 0..buf.area.height {
        let mut line = String::new();
        let mut x = 0u16;
        while x < buf.area.width {
            let sym = buf[(x, y)].symbol();
            let w = UnicodeWidthStr::width(sym).max(1) as u16;
            line.push_str(sym);
            x = x.saturating_add(w);
        }
        out.push(line.trim_end().to_string());
    }
    out
}

#[test]
fn scrolling_up_reveals_older_cards() {
    let state = demo_state();
    let bottom_view = draw_with_scroll(&state, 140, 30, 0);
    let scrolled = draw_with_scroll(&state, 140, 30, u16::MAX);
    assert_ne!(
        joined(&bottom_view),
        joined(&scrolled),
        "scrolled view must differ from default"
    );
    assert!(
        joined(&scrolled).contains("YOU"),
        "older user card should appear after scroll-to-top"
    );
}

#[test]
fn scrollbar_thumb_appears_when_content_overflows() {
    let rows = draw_with_scroll(&demo_state(), 140, 18, 0);
    let all = joined(&rows);
    assert!(
        all.contains('█') || all.contains('▊'),
        "thumb glyph missing"
    );
}

#[test]
fn selection_contains_handles_single_line_and_multi_line() {
    let mut sel = Selection::new(5, 10);
    sel.extend(20, 10);
    assert!(sel.contains_virt(5, 10));
    assert!(sel.contains_virt(20, 10));
    assert!(sel.contains_virt(12, 10));
    assert!(!sel.contains_virt(4, 10));
    assert!(!sel.contains_virt(21, 10));
    assert!(!sel.contains_virt(5, 11));

    let mut multi = Selection::new(5, 10);
    multi.extend(3, 12);
    assert!(multi.contains_virt(20, 10), "first row right of anchor");
    assert!(multi.contains_virt(0, 11), "middle row anywhere");
    assert!(multi.contains_virt(3, 12), "last row up to head");
    assert!(!multi.contains_virt(4, 12), "past head on last row");
}

#[test]
fn extract_text_returns_selected_card_symbols() {
    use ratatui::layout::Rect;
    let state = demo_state();
    let term = Rect::new(0, 0, 140, 80);
    let layout = cards_layout(term, &state, 0, true);
    assert!(layout.total > 0, "demo state has cards");
    let mut sel = Selection::new(layout.screen_rect.x + 2, layout.view_top);
    sel.extend(
        layout.screen_rect.right().saturating_sub(2),
        layout.view_top + layout.view_h.saturating_sub(1),
    );
    let text = extract_text(&state, 0, term, sel, true);
    assert!(!text.is_empty(), "extracted text empty");
    assert!(
        !text.contains("MISSION CONTROL"),
        "sidebar leaked: {text:?}"
    );
    assert!(!text.contains("19.2k/128k"), "dock leaked: {text:?}");
}

fn catalog_state() -> reasonix_render::state::SceneState {
    use reasonix_render::state::{SceneState, SlashMatch};
    let entries = ["clear", "compact", "commit", "undo", "diff", "help"];
    SceneState {
        slash_catalog: Some(
            entries
                .iter()
                .map(|cmd| SlashMatch {
                    cmd: (*cmd).to_string(),
                    summary: String::new(),
                    group: None,
                    args_hint: None,
                    aliases: Vec::new(),
                    arg_completer: None,
                })
                .collect(),
        ),
        ..Default::default()
    }
}

#[test]
fn idle_banner_includes_starter_command_hints() {
    let rows = draw(&SceneState::default(), 120, 30);
    let all = joined(&rows);
    assert!(all.contains("● idle"), "idle marker");
    assert!(all.contains("/help"), "starter /help");
    assert!(all.contains("/cost"), "starter /cost");
    assert!(all.contains("/init"), "starter /init");
    assert!(all.contains("/sessions"), "starter /sessions");
}

#[test]
fn status_bar_shows_activity_phase_when_busy() {
    let state = SceneState {
        busy: true,
        activity: Some("reasoning".to_string()),
        ..Default::default()
    };
    let rows = draw(&state, 140, 36);
    let last = rows
        .iter()
        .rev()
        .find(|r| r.contains("reasoning"))
        .expect("status row with phase label");
    assert!(last.contains("●"), "brand dot visible: {last}");
}

#[test]
fn list_picker_modal_renders_when_scene_carries_one() {
    use reasonix_render::state::{ListPicker, ListPickerOption, SceneState};
    let state = SceneState {
        list_picker: Some(ListPicker {
            id: "pick-1".to_string(),
            title: "switch session".to_string(),
            hint: Some("↑↓ move  ↵ open".to_string()),
            options: vec![
                ListPickerOption {
                    key: "s-2026-05-17".to_string(),
                    label: "s-2026-05-17".to_string(),
                    sublabel: Some("branch main".to_string()),
                    meta: Some("2h ago".to_string()),
                },
                ListPickerOption {
                    key: "s-2026-05-16".to_string(),
                    label: "s-2026-05-16".to_string(),
                    sublabel: None,
                    meta: Some("1d ago".to_string()),
                },
            ],
        }),
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("switch session"), "picker title missing");
    assert!(all.contains("s-2026-05-17"), "first option missing");
    assert!(all.contains("s-2026-05-16"), "second option missing");
    assert!(all.contains("branch main"), "sublabel missing");
    assert!(all.contains("2h ago"), "meta missing");
    assert!(all.contains("↵"), "hint visible");
}

#[test]
fn renders_doctor_memory_ctx_plan_card_task_kinds() {
    let cards = [
        ("doctor", "/doctor", "✓ git\n✕ docker"),
        (
            "memory",
            "memory · 2",
            "· [user] ds engineer\n· [project] rust port",
        ),
        (
            "ctx",
            "context breakdown",
            "8.0k / 128k used (6%)\nsystem 1.2k",
        ),
        ("plan-card", "rewrite renderer", "○ scaffold\n◆ port cards"),
        ("task", "build · 1/3", "✓ install\n◆ compile"),
    ];
    for (kind, summary, body) in cards {
        let state = SceneState {
            cards: vec![SceneCard {
                kind: kind.to_string(),
                summary: summary.to_string(),
                body: Some(body.to_string()),
                ..Default::default()
            }],
            ..Default::default()
        };
        let rows = draw(&state, 140, 30);
        let all = joined(&rows);
        assert!(all.contains(summary), "{kind} summary missing: {summary}");
        let first_body_line = body.lines().next().unwrap();
        assert!(
            all.contains(first_body_line),
            "{kind} body missing: {first_body_line}"
        );
    }
}

#[test]
fn dashboard_url_renders_in_boot_block() {
    let state = SceneState {
        model: Some("deepseek-v3.2-coder".to_string()),
        cwd: Some("~/work/reasonix-core".to_string()),
        dashboard_url: Some("http://localhost:7777".to_string()),
        ..Default::default()
    };
    let rows = draw(&state, 120, 36);
    let all = joined(&rows);
    assert!(all.contains("dashboard"), "dashboard label missing");
    assert!(
        all.contains("http://localhost:7777"),
        "dashboard URL missing"
    );
}

#[test]
fn dashboard_line_hidden_when_url_absent() {
    let state = SceneState {
        model: Some("deepseek-v3.2-coder".to_string()),
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(!all.contains("dashboard"), "no dashboard line without URL");
}

#[test]
fn double_slash_does_not_match_anything() {
    let state = catalog_state();
    // Bare "/" is browse mode — every catalog entry surfaces.
    assert_eq!(slash_match_count("/", &state), 6, "bare slash shows all");
    // "//" is two literal slashes, not "bare slash with prefix" — the
    // user is most likely typing a chat message starting with //, so the
    // overlay should NOT explode into the whole catalog.
    assert_eq!(
        slash_match_count("//", &state),
        0,
        "double slash matches nothing"
    );
    assert_eq!(
        slash_match_count("///c", &state),
        0,
        "triple slash also matches nothing"
    );
}

#[test]
fn slash_arg_picker_static_completer() {
    use reasonix_render::state::{SceneState, SlashMatch};
    let state = SceneState {
        slash_catalog: Some(vec![
            SlashMatch {
                cmd: "preset".to_string(),
                summary: String::new(),
                group: Some("setup".to_string()),
                args_hint: Some("<auto|flash|pro>".to_string()),
                aliases: Vec::new(),
                arg_completer: Some(vec![
                    "auto".to_string(),
                    "flash".to_string(),
                    "pro".to_string(),
                ]),
            },
            SlashMatch {
                cmd: "language".to_string(),
                summary: String::new(),
                group: Some("setup".to_string()),
                args_hint: Some("<EN|zh-CN>".to_string()),
                aliases: Vec::new(),
                arg_completer: Some(vec!["EN".to_string(), "zh-CN".to_string()]),
            },
            SlashMatch {
                cmd: "help".to_string(),
                summary: String::new(),
                group: Some("chat".to_string()),
                args_hint: None,
                aliases: Vec::new(),
                arg_completer: None,
            },
        ]),
        ..Default::default()
    };
    assert_eq!(
        slash_arg_match_count("/preset ", &state),
        3,
        "bare arg shows all"
    );
    assert_eq!(
        slash_arg_match_count("/preset fl", &state),
        1,
        "prefix filters"
    );
    assert_eq!(
        slash_arg_match_count("/preset flash", &state),
        0,
        "exact dismisses"
    );
    assert_eq!(slash_arg_match_count("/help ", &state), 0, "no completer");
    assert_eq!(
        slash_arg_match_count("/preset auto x", &state),
        0,
        "past first arg"
    );
    assert_eq!(slash_arg_match_count("/zzz ", &state), 0, "unknown cmd");
    assert_eq!(
        slash_arg_completion("/preset fl", 0, &state).as_deref(),
        Some("/preset flash"),
    );
    assert_eq!(
        slash_arg_completion("/language ", 1, &state).as_deref(),
        Some("/language zh-CN"),
    );
}

#[test]
fn slash_arg_state_overrides_catalog_for_dynamic_completers() {
    use reasonix_render::state::{SceneState, SlashArgState, SlashMatch};
    let state = SceneState {
        slash_catalog: Some(vec![SlashMatch {
            cmd: "model".to_string(),
            summary: String::new(),
            group: Some("setup".to_string()),
            args_hint: Some("<id>".to_string()),
            aliases: Vec::new(),
            arg_completer: None,
        }]),
        slash_arg_state: Some(SlashArgState {
            cmd: "model".to_string(),
            partial: "ds".to_string(),
            matches: vec!["deepseek-v3.2-coder".to_string(), "deepseek-r1".to_string()],
        }),
        ..Default::default()
    };
    assert_eq!(
        slash_arg_match_count("/model ds", &state),
        2,
        "scene picks up dynamic matches"
    );
    assert_eq!(
        slash_arg_completion("/model ds", 0, &state).as_deref(),
        Some("/model deepseek-v3.2-coder"),
    );
    let no_match = SceneState {
        slash_catalog: state.slash_catalog.clone(),
        slash_arg_state: Some(SlashArgState {
            cmd: "model".to_string(),
            partial: "old".to_string(),
            matches: vec!["irrelevant".to_string()],
        }),
        ..Default::default()
    };
    assert_eq!(
        slash_arg_match_count("/model ds", &no_match),
        0,
        "stale scene partial does not leak in",
    );
}

#[test]
fn slash_is_exact_matches_full_command_only() {
    let state = catalog_state();
    assert!(slash_is_exact("/clear", &state), "full name matches");
    assert!(slash_is_exact("/CLEAR", &state), "case-insensitive");
    assert!(!slash_is_exact("/cl", &state), "prefix is not exact");
    assert!(
        !slash_is_exact("/clear ", &state),
        "trailing space is not exact"
    );
    assert!(
        !slash_is_exact("/clear foo", &state),
        "with args is not exact"
    );
    assert!(!slash_is_exact("clear", &state), "no leading slash");
    assert!(!slash_is_exact("/zzz", &state), "unknown command");
    assert!(!slash_is_exact("/", &state), "empty cmd");
}

#[test]
fn slash_match_count_filters_by_prefix() {
    let state = catalog_state();
    assert_eq!(slash_match_count("/c", &state), 3);
    assert_eq!(slash_match_count("/u", &state), 1);
    assert_eq!(slash_match_count("/", &state), 6);
    assert_eq!(slash_match_count("", &state), 0);
    assert_eq!(slash_match_count("/zzz", &state), 0);
}

#[test]
fn slash_completion_appends_trailing_space() {
    let state = catalog_state();
    assert_eq!(
        slash_completion("/c", 0, &state).as_deref(),
        Some("/clear ")
    );
    assert_eq!(
        slash_completion("/c", 1, &state).as_deref(),
        Some("/compact ")
    );
    assert_eq!(
        slash_completion("/c", 2, &state).as_deref(),
        Some("/commit ")
    );
    assert_eq!(slash_completion("/c", 3, &state), None);
    assert_eq!(slash_completion("abc", 0, &state), None);
}

#[test]
fn slash_overlay_highlights_selected_index() {
    fn draw_at_idx(idx: usize) -> Vec<String> {
        let mut state = demo_state();
        state.composer_text = Some("/c".to_string());
        let backend = TestBackend::new(140, 50);
        let mut terminal = Terminal::new(backend).unwrap();
        terminal
            .draw(|f| {
                let area = f.area();
                f.render_widget(WholeScreen::new(&state).with_slash_index(idx), area);
            })
            .unwrap();
        let buf = terminal.backend().buffer().clone();
        let mut out = Vec::with_capacity(buf.area.height as usize);
        for y in 0..buf.area.height {
            let mut line = String::new();
            let mut x = 0u16;
            while x < buf.area.width {
                let sym = buf[(x, y)].symbol();
                let w = UnicodeWidthStr::width(sym).max(1) as u16;
                line.push_str(sym);
                x = x.saturating_add(w);
            }
            out.push(line.trim_end().to_string());
        }
        out
    }

    let rows_zero = draw_at_idx(0);
    let clear_row = rows_zero
        .iter()
        .find(|r| r.contains("/clear"))
        .expect("/clear row");
    assert!(
        clear_row.contains("▸"),
        "first row should be marked: {clear_row:?}"
    );

    let rows_two = draw_at_idx(2);
    let commit_row = rows_two
        .iter()
        .find(|r| r.contains("/commit"))
        .expect("/commit row");
    assert!(
        commit_row.contains("▸"),
        "third row should be marked: {commit_row:?}"
    );
    let clear_after = rows_two
        .iter()
        .find(|r| r.contains("/clear"))
        .expect("/clear row");
    assert!(
        !clear_after.contains("▸"),
        "/clear should not be marked when idx=2"
    );
}

fn at_state_state() -> reasonix_render::state::SceneState {
    use reasonix_render::state::{AtPickerEntry, AtState, SceneState};
    SceneState {
        at_state: Some(AtState::Browse {
            base_dir: String::new(),
            entries: vec![
                AtPickerEntry {
                    label: "src/parser.ts".to_string(),
                    insert_path: "src/parser.ts".to_string(),
                    dir_suffix: String::new(),
                    is_dir: false,
                },
                AtPickerEntry {
                    label: "tests/parser.test.ts".to_string(),
                    insert_path: "tests/parser.test.ts".to_string(),
                    dir_suffix: String::new(),
                    is_dir: false,
                },
            ],
            loading: false,
        }),
        ..Default::default()
    }
}

#[test]
fn at_match_count_requires_at_at_end() {
    let state = at_state_state();
    assert_eq!(at_match_count("@", &state), 2);
    assert_eq!(at_match_count("@par", &state), 2);
    assert_eq!(at_match_count("check @par", &state), 2);
    assert_eq!(at_match_count("@par this", &state), 0);
    assert_eq!(at_match_count("nopath", &state), 0);
    assert_eq!(at_match_count("foo@bar", &state), 0);
}

#[test]
fn at_completion_replaces_only_the_at_token() {
    let state = at_state_state();
    assert_eq!(
        at_completion("@par", 0, &state).as_deref(),
        Some("@src/parser.ts ")
    );
    assert_eq!(
        at_completion("check @par", 0, &state).as_deref(),
        Some("check @src/parser.ts ")
    );
    assert_eq!(at_completion("no at", 0, &state), None);
}

#[test]
fn at_overlay_renders_when_composer_has_at_token() {
    let mut state = demo_state();
    state.composer_text = Some("check @par".to_string());
    let at = at_state_state();
    state.at_state = at.at_state;
    let backend = TestBackend::new(140, 50);
    let mut terminal = Terminal::new(backend).unwrap();
    terminal
        .draw(|f| f.render_widget(WholeScreen::new(&state), f.area()))
        .unwrap();
    let buf = terminal.backend().buffer().clone();
    let mut all = String::new();
    for y in 0..buf.area.height {
        let mut x = 0u16;
        while x < buf.area.width {
            let sym = buf[(x, y)].symbol();
            let w = UnicodeWidthStr::width(sym).max(1) as u16;
            all.push_str(sym);
            x = x.saturating_add(w);
        }
        all.push('\n');
    }
    assert!(all.contains("src/parser.ts"), "parser.ts match missing");
}

#[test]
fn cmd_card_renders_header_and_body() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "cmd".to_string(),
            summary: "pnpm test parser".to_string(),
            meta: Some("exit 0 · 2.1s".to_string()),
            body: Some(" RUN  v1.6.0\n  ✓ tokens".to_string()),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("$ pnpm test parser"));
    assert!(all.contains("exit 0"));
    assert!(all.contains("RUN  v1.6.0"));
    assert!(all.contains("✓ tokens"));
}

#[test]
fn fileview_card_renders_line_numbers_and_code() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "fileview".to_string(),
            summary: "src/foo.ts".to_string(),
            meta: Some("12 more lines".to_string()),
            body: Some("14:import { escape }\n15:export function foo() {}".to_string()),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("src/foo.ts"));
    assert!(all.contains("12 more lines"));
    assert!(all.contains("14"));
    assert!(all.contains("import { escape }"));
    assert!(all.contains("export function foo()"));
}

#[test]
fn running_tool_card_animates_through_spinner_frames() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "tool".to_string(),
            summary: "Edit".to_string(),
            args: Some("src/foo.ts".to_string()),
            status: Some(reasonix_render::state::ToolStatus::Running),
            ..Default::default()
        }],
        ..Default::default()
    };
    let backend_a = TestBackend::new(120, 40);
    let mut term_a = Terminal::new(backend_a).unwrap();
    term_a
        .draw(|f| {
            f.render_widget(WholeScreen::new(&state).with_tick(0), f.area());
        })
        .unwrap();
    let backend_b = TestBackend::new(120, 40);
    let mut term_b = Terminal::new(backend_b).unwrap();
    term_b
        .draw(|f| {
            f.render_widget(WholeScreen::new(&state).with_tick(3), f.area());
        })
        .unwrap();
    let a = buffer_string(term_a.backend().buffer());
    let b = buffer_string(term_b.backend().buffer());
    assert!(a.contains('⠋'), "tick=0 frame missing: {a:?}");
    assert!(b.contains('⠸'), "tick=3 frame missing: {b:?}");
}

#[test]
fn streaming_card_body_reveals_progressively() {
    let state = SceneState {
        busy: true,
        cards: vec![SceneCard {
            kind: "streaming".to_string(),
            body: Some("hello world from streaming reveal".to_string()),
            ..Default::default()
        }],
        ..Default::default()
    };
    let backend_early = TestBackend::new(120, 30);
    let mut term_early = Terminal::new(backend_early).unwrap();
    term_early
        .draw(|f| f.render_widget(WholeScreen::new(&state).with_tick(2), f.area()))
        .unwrap();
    let backend_mid = TestBackend::new(120, 30);
    let mut term_mid = Terminal::new(backend_mid).unwrap();
    term_mid
        .draw(|f| f.render_widget(WholeScreen::new(&state).with_tick(20), f.area()))
        .unwrap();
    let backend_late = TestBackend::new(120, 30);
    let mut term_late = Terminal::new(backend_late).unwrap();
    term_late
        .draw(|f| f.render_widget(WholeScreen::new(&state).with_tick(200), f.area()))
        .unwrap();
    let early = buffer_string(term_early.backend().buffer());
    let mid = buffer_string(term_mid.backend().buffer());
    let late = buffer_string(term_late.backend().buffer());
    assert!(
        !early.contains("streaming reveal"),
        "tick=2 should not have full body"
    );
    assert!(mid.contains("hello"), "tick=20 should have some chars");
    assert!(
        late.contains("streaming reveal"),
        "tick=200 should have full body"
    );
}

#[test]
fn composer_scrolls_to_keep_cursor_visible() {
    let mut state = SceneState::default();
    let body = (1..=8)
        .map(|n| format!("line {n}"))
        .collect::<Vec<_>>()
        .join("\n");
    let cursor_at_end = body.chars().count();
    state.composer_text = Some(body);
    state.composer_cursor = Some(cursor_at_end);
    let backend = TestBackend::new(120, 40);
    let mut term = Terminal::new(backend).unwrap();
    term.draw(|f| f.render_widget(WholeScreen::new(&state).with_tick(0), f.area()))
        .unwrap();
    let buf = term.backend().buffer().clone();
    let mut all = String::new();
    for y in 0..buf.area.height {
        let mut x = 0u16;
        while x < buf.area.width {
            let sym = buf[(x, y)].symbol();
            let w = UnicodeWidthStr::width(sym).max(1) as u16;
            all.push_str(sym);
            x = x.saturating_add(w);
        }
        all.push('\n');
    }
    assert!(
        all.contains("line 8"),
        "last line (cursor row) must be visible"
    );
    assert!(all.contains("line 4"), "line 4 should still fit (cap 5)");
    assert!(
        !all.contains("line 1"),
        "earliest line should be scrolled out"
    );
    assert!(all.contains('↑'), "scroll indicator up missing");
}

#[test]
fn composer_box_grows_for_multi_line_text() {
    let state = SceneState {
        composer_text: Some("line one\nline two\nline three".to_string()),
        composer_cursor: Some(0),
        ..Default::default()
    };
    let backend = TestBackend::new(120, 40);
    let mut term = Terminal::new(backend).unwrap();
    term.draw(|f| f.render_widget(WholeScreen::new(&state).with_tick(0), f.area()))
        .unwrap();
    let buf = term.backend().buffer().clone();
    let mut lines_found = vec![false; 3];
    for y in 0..buf.area.height {
        let mut line = String::new();
        let mut x = 0u16;
        while x < buf.area.width {
            let sym = buf[(x, y)].symbol();
            let w = UnicodeWidthStr::width(sym).max(1) as u16;
            line.push_str(sym);
            x = x.saturating_add(w);
        }
        if line.contains("line one") {
            lines_found[0] = true;
        }
        if line.contains("line two") {
            lines_found[1] = true;
        }
        if line.contains("line three") {
            lines_found[2] = true;
        }
    }
    assert!(
        lines_found[0] && lines_found[1] && lines_found[2],
        "all 3 composer lines should render: {lines_found:?}"
    );
}

#[test]
fn shell_prefix_does_not_break_overlay_or_composer() {
    let mut state = demo_state();
    state.composer_text = Some("!ls -la".to_string());
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("❯"), "prompt missing");
    assert!(all.contains("!ls -la"), "shell text missing");
    assert!(
        !all.contains("SLASH COMMANDS"),
        "slash overlay should not show for !"
    );
    assert!(
        !all.contains("@ ATTACH FILE"),
        "at overlay should not show for !"
    );
}

#[test]
fn empty_cards_state_shows_idle_banner() {
    let state = SceneState {
        model: Some("deepseek-v3.2-coder".to_string()),
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("● idle"), "idle glyph + label missing");
    assert!(all.contains("ready for next task"));
    assert!(all.contains("type below"));
    assert!(all.contains("commands"));
    assert!(all.contains("file refs"));
    assert!(all.contains("shell"));
}

#[test]
fn composer_caret_renders_at_cursor_index() {
    let state = SceneState {
        composer_text: Some("hello world".to_string()),
        composer_cursor: Some(5),
        ..Default::default()
    };
    let backend = TestBackend::new(120, 30);
    let mut term = Terminal::new(backend).unwrap();
    term.draw(|f| f.render_widget(WholeScreen::new(&state).with_tick(0), f.area()))
        .unwrap();
    let buf = term.backend().buffer().clone();
    let mut composer_row = None;
    for y in 0..buf.area.height {
        let mut line = String::new();
        let mut x = 0u16;
        while x < buf.area.width {
            let sym = buf[(x, y)].symbol();
            let w = UnicodeWidthStr::width(sym).max(1) as u16;
            line.push_str(sym);
            x = x.saturating_add(w);
        }
        if line.contains("hello") && line.contains("world") {
            composer_row = Some(line);
            break;
        }
    }
    let row = composer_row.expect("composer row missing");
    assert!(
        row.contains("hello▮ world") || row.contains("hello▮world"),
        "caret should sit between hello and world: {row:?}"
    );
}

#[test]
fn composer_caret_blinks_with_tick() {
    let state = demo_state();
    let backend_on = TestBackend::new(120, 30);
    let mut term_on = Terminal::new(backend_on).unwrap();
    term_on
        .draw(|f| f.render_widget(WholeScreen::new(&state).with_tick(0), f.area()))
        .unwrap();
    let backend_off = TestBackend::new(120, 30);
    let mut term_off = Terminal::new(backend_off).unwrap();
    term_off
        .draw(|f| f.render_widget(WholeScreen::new(&state).with_tick(6), f.area()))
        .unwrap();
    let on = buffer_string(term_on.backend().buffer());
    let off = buffer_string(term_off.backend().buffer());
    let on_count = on.matches('▮').count();
    let off_count = off.matches('▮').count();
    assert!(
        on_count > off_count,
        "caret should be visible at tick 0 (on={on_count}, off={off_count})"
    );
}

fn buffer_string(buf: &ratatui::buffer::Buffer) -> String {
    let mut s = String::new();
    for y in 0..buf.area.height {
        let mut x = 0u16;
        while x < buf.area.width {
            let sym = buf[(x, y)].symbol();
            let w = UnicodeWidthStr::width(sym).max(1) as u16;
            s.push_str(sym);
            x = x.saturating_add(w);
        }
        s.push('\n');
    }
    s
}

#[test]
fn subagent_card_renders_task_and_steps() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "subagent".to_string(),
            summary: "subagent: reviewer".to_string(),
            meta: Some("3 steps · 1.4s".to_string()),
            body: Some("review parser.ts\nscanned call sites\nall callers compatible".to_string()),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("⧪"));
    assert!(all.contains("subagent: reviewer"));
    assert!(all.contains("3 steps · 1.4s"));
    assert!(all.contains("review parser.ts"));
    assert!(all.contains("→"), "step glyph missing");
    assert!(all.contains("scanned call sites"));
}

#[test]
fn confirm_card_renders_title_question_and_options() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "confirm".to_string(),
            summary: "edit src/parser.ts".to_string(),
            body: Some(
                "modify parser.ts (+34 -18, affects 7 call sites)?\n[y]apply [n]skip [d]diff [a]always"
                    .to_string(),
            ),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("⚠"));
    assert!(all.contains("permission required"));
    assert!(all.contains("edit src/parser.ts"));
    assert!(all.contains("[y] apply"));
    assert!(all.contains("[n] skip"));
    assert!(all.contains("[d] diff"));
    assert!(all.contains("[a] always"));
}

#[test]
fn await_card_renders_question_and_keyed_options() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "await".to_string(),
            body: Some(
                "wrapCode needs lang inference now?\ny) yes — add lang\nn) no — keep current\ns) show wrapCode first"
                    .to_string(),
            ),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("awaiting input"));
    assert!(all.contains("wrapCode needs"));
    assert!(all.contains("y)"));
    assert!(all.contains("yes — add lang"));
    assert!(all.contains("s)"));
}

#[test]
fn error_card_renders_title_and_traceback() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "error".to_string(),
            summary: "typecheck failed".to_string(),
            body: Some(
                "TS2554: Expected 1 arguments, but got 2.\n  createConnection(url, { ssl: true });\nat src/db/pool.ts:42:8"
                    .to_string(),
            ),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("✕"));
    assert!(all.contains("typecheck failed"));
    assert!(all.contains("TS2554"));
    assert!(all.contains("createConnection"));
    assert!(all.contains("at src/db/pool.ts:42:8"));
}

#[test]
fn search_card_renders_file_line_and_match() {
    let state = SceneState {
        cards: vec![SceneCard {
            kind: "search".to_string(),
            summary: "grep \"foo\" in src/".to_string(),
            meta: Some("2 matches".to_string()),
            body: Some("src/a.ts:42:foo(bar)\nsrc/b.ts:88:return foo;".to_string()),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(all.contains("grep \"foo\" in src/"));
    assert!(all.contains("2 matches"));
    assert!(all.contains("src/a.ts"));
    assert!(all.contains("foo(bar)"));
    assert!(all.contains("src/b.ts"));
}

#[test]
fn selection_follows_scroll() {
    use ratatui::layout::Rect;
    let state = demo_state();
    let term = Rect::new(0, 0, 140, 30);
    let layout_a = cards_layout(term, &state, 0, true);
    let layout_b = cards_layout(term, &state, 5, true);
    assert!(
        layout_b.view_top < layout_a.view_top || layout_a.view_top == layout_b.view_top,
        "scrolling up moves view_top earlier"
    );
    let mut sel = Selection::new(layout_a.screen_rect.x + 4, layout_a.view_top + 1);
    sel.extend(layout_a.screen_rect.x + 30, layout_a.view_top + 1);
    let a = extract_text(&state, 0, term, sel, true);
    let b = extract_text(&state, 5, term, sel, true);
    assert_eq!(a, b, "selection text invariant under scroll");
}

#[test]
fn sidebar_hidden_when_toggled_off() {
    let backend = TestBackend::new(120, 30);
    let mut terminal = Terminal::new(backend).unwrap();
    terminal
        .draw(|f| {
            let area = f.area();
            f.render_widget(
                WholeScreen::new(&demo_state()).with_sidebar_visible(false),
                area,
            );
        })
        .unwrap();
    let buf = terminal.backend().buffer().clone();
    let mut all = String::new();
    for y in 0..buf.area.height {
        for x in 0..buf.area.width {
            all.push_str(buf[(x, y)].symbol());
        }
        all.push('\n');
    }
    assert!(
        !all.contains("MISSION CONTROL"),
        "sidebar must be hidden when toggled off"
    );
    assert!(!all.contains("JOBS"), "sidebar JOBS section must be hidden");
}

#[test]
fn sidebar_long_label_wraps_within_sidebar() {
    let state = SceneState {
        model: Some("ds".to_string()),
        cards: vec![
            SceneCard {
                kind: "todo".to_string(),
                body: Some(
                    "[~] 这是一个非常非常非常非常非常非常非常长的中文计划描述用来测试换行"
                        .to_string(),
                ),
                ..Default::default()
            },
            SceneCard {
                kind: "tool".to_string(),
                summary: "Grep".to_string(),
                args: Some(
                    "\"a_pattern_that_is_long_enough_to_need_wrapping_in_the_sidebar\"".to_string(),
                ),
                status: Some(reasonix_render::state::ToolStatus::Ok),
                ..Default::default()
            },
        ],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let all = joined(&rows);
    assert!(
        all.contains("中文计划描述"),
        "long plan label continuation segment missing"
    );
    assert!(
        all.contains("enough_to_need_wrapping"),
        "long job label continuation segment missing"
    );
}

#[test]
fn main_panel_content_does_not_leak_into_sidebar_columns() {
    let state = SceneState {
        model: Some("ds".to_string()),
        cwd: Some(
            "F:/some/very/deeply/nested/working/directory/that/keeps/going/and/going".to_string(),
        ),
        cards: vec![SceneCard {
            kind: "tool".to_string(),
            summary: "Grep".to_string(),
            args: Some("short".to_string()),
            status: Some(reasonix_render::state::ToolStatus::Ok),
            ..Default::default()
        }],
        ..Default::default()
    };
    let rows = draw(&state, 120, 30);
    let main_w = 120 - 34;
    for (i, line) in rows.iter().enumerate() {
        let cells: Vec<&str> = line.split_terminator("").skip(1).collect();
        for j in main_w..cells.len().min(main_w + 2) {
            let cell = cells.get(j).copied().unwrap_or("");
            assert!(
                cell == "│" || cell == " " || cell.is_empty(),
                "row {i} col {j}: main-panel content leaked into sidebar: {cell:?} (line: {line:?})"
            );
        }
    }
}
