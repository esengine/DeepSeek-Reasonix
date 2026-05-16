use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color as RColor, Modifier};

use reasonix_render::render::render_frame;
use reasonix_render::scene::{
    BorderStyle, BoxLayout, Color, Dim, FillToken, FlexDirection, NamedColor, SceneFrame,
    SceneNode, TextRun, TextStyle,
};

fn box_with_width(text: &str, width: Dim) -> SceneNode {
    SceneNode::Box {
        layout: Some(BoxLayout {
            width: Some(width),
            ..Default::default()
        }),
        children: vec![SceneNode::Text {
            runs: vec![TextRun {
                text: text.to_string(),
                style: None,
            }],
            wrap: None,
        }],
    }
}

fn box_with_height(text: &str, height: Dim) -> SceneNode {
    SceneNode::Box {
        layout: Some(BoxLayout {
            height: Some(height),
            ..Default::default()
        }),
        children: vec![SceneNode::Text {
            runs: vec![TextRun {
                text: text.to_string(),
                style: None,
            }],
            wrap: None,
        }],
    }
}

fn frame_of(root: SceneNode) -> SceneFrame {
    SceneFrame {
        schema_version: 1,
        cols: 80,
        rows: 24,
        root,
    }
}

fn collect_row(buf: &Buffer, y: u16, width: u16) -> String {
    let mut out = String::new();
    for x in 0..width {
        out.push_str(buf[(x, y)].symbol());
    }
    out.trim_end().to_string()
}

#[test]
fn cjk_chars_advance_by_their_display_width_so_neighbors_dont_overlap() {
    // Smoke test for the wide-char bug seen on 2026-05-15: typing 测试123
    // showed up as "测 23" because every char advanced x by 1 instead of by
    // the char's display width.
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "测试123".to_string(),
            style: None,
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 20, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    // Cell 0 holds 测 (width 2); cell 1 is the continuation. Cell 2 holds 试
    // (width 2); cell 3 continuation. Cells 4/5/6 hold 1/2/3.
    assert_eq!(buf[(0, 0)].symbol(), "测");
    assert_eq!(buf[(2, 0)].symbol(), "试");
    assert_eq!(buf[(4, 0)].symbol(), "1");
    assert_eq!(buf[(5, 0)].symbol(), "2");
    assert_eq!(buf[(6, 0)].symbol(), "3");
}

#[test]
fn renders_a_plain_text_frame_at_row_zero() {
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "hello".to_string(),
            style: None,
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 10, 3);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 10), "hello");
}

#[test]
fn applies_color_and_bold_to_text() {
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "ok".to_string(),
            style: Some(TextStyle {
                color: Some(Color::Named(NamedColor::Green)),
                bold: Some(true),
                ..Default::default()
            }),
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 5, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    let cell = &buf[(0, 0)];
    assert_eq!(cell.symbol(), "o");
    assert_eq!(cell.style().fg, Some(RColor::Green));
    assert!(cell.style().add_modifier.contains(Modifier::BOLD));
}

#[test]
fn stacks_column_children_vertically_with_gap() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Column),
            gap: Some(1),
            ..Default::default()
        }),
        children: vec![
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "first".to_string(),
                    style: None,
                }],
                wrap: None,
            },
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "second".to_string(),
                    style: None,
                }],
                wrap: None,
            },
        ],
    });
    let area = Rect::new(0, 0, 10, 5);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 10), "first");
    assert_eq!(collect_row(&buf, 1, 10), "");
    assert_eq!(collect_row(&buf, 2, 10), "second");
}

#[test]
fn lays_out_row_children_horizontally() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        }),
        children: vec![
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "ab".to_string(),
                    style: None,
                }],
                wrap: None,
            },
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "cd".to_string(),
                    style: None,
                }],
                wrap: None,
            },
        ],
    });
    let area = Rect::new(0, 0, 10, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 10), "abcd");
}

#[test]
fn padding_shifts_children_in_and_shrinks_area() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            padding_x: Some(2),
            padding_y: Some(1),
            ..Default::default()
        }),
        children: vec![SceneNode::Text {
            runs: vec![TextRun {
                text: "x".to_string(),
                style: None,
            }],
            wrap: None,
        }],
    });
    let area = Rect::new(0, 0, 10, 5);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(2, 1)].symbol(), "x");
    assert_eq!(buf[(0, 0)].symbol(), " ");
}

#[test]
fn truncates_text_overflowing_its_area() {
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "abcdefghij".to_string(),
            style: None,
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 4, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 4), "abcd");
}

#[test]
fn row_with_fixed_cell_widths_reserves_those_widths_first() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        }),
        children: vec![
            box_with_width("L", Dim::Cells(4)),
            box_with_width("R", Dim::Cells(3)),
        ],
    });
    let area = Rect::new(0, 0, 20, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "L");
    assert_eq!(buf[(4, 0)].symbol(), "R");
}

#[test]
fn row_with_one_fill_child_takes_all_remaining_space() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        }),
        children: vec![
            box_with_width("L", Dim::Cells(3)),
            box_with_width("M", Dim::Fill(FillToken::Fill)),
            box_with_width("R", Dim::Cells(3)),
        ],
    });
    let area = Rect::new(0, 0, 20, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "L");
    assert_eq!(buf[(3, 0)].symbol(), "M");
    assert_eq!(buf[(17, 0)].symbol(), "R");
}

#[test]
fn row_with_multiple_fill_children_splits_remainder_evenly() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        }),
        children: vec![
            box_with_width("A", Dim::Fill(FillToken::Fill)),
            box_with_width("B", Dim::Fill(FillToken::Fill)),
            box_with_width("C", Dim::Fill(FillToken::Fill)),
        ],
    });
    let area = Rect::new(0, 0, 12, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "A");
    assert_eq!(buf[(4, 0)].symbol(), "B");
    assert_eq!(buf[(8, 0)].symbol(), "C");
}

#[test]
fn row_distributes_uneven_remainder_to_leading_fill_children() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        }),
        children: vec![
            box_with_width("A", Dim::Fill(FillToken::Fill)),
            box_with_width("B", Dim::Fill(FillToken::Fill)),
        ],
    });
    let area = Rect::new(0, 0, 11, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "A");
    assert_eq!(buf[(6, 0)].symbol(), "B");
}

#[test]
fn row_caps_a_fixed_width_at_the_remaining_space() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        }),
        children: vec![
            box_with_width("ABCDE", Dim::Cells(50)),
            box_with_width("X", Dim::Cells(3)),
        ],
    });
    let area = Rect::new(0, 0, 8, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "A");
    assert_eq!(buf[(7, 0)].symbol(), " ");
}

#[test]
fn row_unspecified_children_keep_their_intrinsic_widths() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            ..Default::default()
        }),
        children: vec![
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "ab".to_string(),
                    style: None,
                }],
                wrap: None,
            },
            SceneNode::Text {
                runs: vec![TextRun {
                    text: "cd".to_string(),
                    style: None,
                }],
                wrap: None,
            },
        ],
    });
    let area = Rect::new(0, 0, 10, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(collect_row(&buf, 0, 10), "abcd");
}

#[test]
fn row_gap_is_subtracted_from_available_before_distributing() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Row),
            gap: Some(1),
            ..Default::default()
        }),
        children: vec![
            box_with_width("A", Dim::Fill(FillToken::Fill)),
            box_with_width("B", Dim::Fill(FillToken::Fill)),
        ],
    });
    let area = Rect::new(0, 0, 9, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "A");
    assert_eq!(buf[(5, 0)].symbol(), "B");
}

#[test]
fn column_with_fixed_cell_heights_reserves_those_heights_first() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Column),
            ..Default::default()
        }),
        children: vec![
            box_with_height("T", Dim::Cells(2)),
            box_with_height("B", Dim::Cells(1)),
        ],
    });
    let area = Rect::new(0, 0, 5, 10);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "T");
    assert_eq!(buf[(0, 2)].symbol(), "B");
}

#[test]
fn column_fill_child_consumes_remaining_height() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Column),
            ..Default::default()
        }),
        children: vec![
            box_with_height("T", Dim::Cells(1)),
            box_with_height("M", Dim::Fill(FillToken::Fill)),
            box_with_height("B", Dim::Cells(1)),
        ],
    });
    let area = Rect::new(0, 0, 5, 10);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "T");
    assert_eq!(buf[(0, 1)].symbol(), "M");
    assert_eq!(buf[(0, 9)].symbol(), "B");
}

#[test]
fn box_with_single_border_draws_box_drawing_glyphs_at_the_perimeter() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            border_style: Some(BorderStyle::Single),
            ..Default::default()
        }),
        children: vec![SceneNode::Text {
            runs: vec![TextRun {
                text: "x".to_string(),
                style: None,
            }],
            wrap: None,
        }],
    });
    let area = Rect::new(0, 0, 5, 3);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "┌");
    assert_eq!(buf[(4, 0)].symbol(), "┐");
    assert_eq!(buf[(0, 2)].symbol(), "└");
    assert_eq!(buf[(4, 2)].symbol(), "┘");
    assert_eq!(buf[(1, 0)].symbol(), "─");
    assert_eq!(buf[(0, 1)].symbol(), "│");
    assert_eq!(buf[(1, 1)].symbol(), "x");
}

#[test]
fn rounded_border_uses_curved_corner_glyphs() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            border_style: Some(BorderStyle::Round),
            ..Default::default()
        }),
        children: vec![],
    });
    let area = Rect::new(0, 0, 4, 3);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "╭");
    assert_eq!(buf[(3, 0)].symbol(), "╮");
    assert_eq!(buf[(0, 2)].symbol(), "╰");
    assert_eq!(buf[(3, 2)].symbol(), "╯");
}

#[test]
fn double_border_uses_double_line_glyphs() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            border_style: Some(BorderStyle::Double),
            ..Default::default()
        }),
        children: vec![],
    });
    let area = Rect::new(0, 0, 4, 3);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].symbol(), "╔");
    assert_eq!(buf[(3, 0)].symbol(), "╗");
}

#[test]
fn border_color_is_applied_to_perimeter_cells() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            border_style: Some(BorderStyle::Single),
            border_color: Some(Color::Named(NamedColor::Cyan)),
            ..Default::default()
        }),
        children: vec![],
    });
    let area = Rect::new(0, 0, 4, 3);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].style().fg, Some(RColor::Cyan));
    assert_eq!(buf[(1, 0)].style().fg, Some(RColor::Cyan));
}

#[test]
fn background_fills_every_cell_inside_the_box() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            background: Some(Color::Hex {
                hex: "#112233".to_string(),
            }),
            ..Default::default()
        }),
        children: vec![SceneNode::Text {
            runs: vec![TextRun {
                text: "y".to_string(),
                style: None,
            }],
            wrap: None,
        }],
    });
    let area = Rect::new(0, 0, 3, 2);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    let want = RColor::Rgb(0x11, 0x22, 0x33);
    assert_eq!(buf[(0, 0)].style().bg, Some(want));
    assert_eq!(buf[(2, 1)].style().bg, Some(want));
    assert_eq!(buf[(0, 0)].symbol(), "y");
}

#[test]
fn border_shrinks_the_inner_area_by_one_cell_on_each_side() {
    let frame = frame_of(SceneNode::Box {
        layout: Some(BoxLayout {
            direction: Some(FlexDirection::Column),
            border_style: Some(BorderStyle::Single),
            ..Default::default()
        }),
        children: vec![SceneNode::Text {
            runs: vec![TextRun {
                text: "abc".to_string(),
                style: None,
            }],
            wrap: None,
        }],
    });
    let area = Rect::new(0, 0, 5, 3);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(1, 1)].symbol(), "a");
    assert_eq!(buf[(2, 1)].symbol(), "b");
    assert_eq!(buf[(3, 1)].symbol(), "c");
}

#[test]
fn renders_hex_color_as_truecolor() {
    let frame = frame_of(SceneNode::Text {
        runs: vec![TextRun {
            text: "z".to_string(),
            style: Some(TextStyle {
                color: Some(Color::Hex {
                    hex: "#aabbcc".to_string(),
                }),
                ..Default::default()
            }),
        }],
        wrap: None,
    });
    let area = Rect::new(0, 0, 3, 1);
    let mut buf = Buffer::empty(area);
    render_frame(&frame, &mut buf, area);
    assert_eq!(buf[(0, 0)].style().fg, Some(RColor::Rgb(0xaa, 0xbb, 0xcc)));
}
