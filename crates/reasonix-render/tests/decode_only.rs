use reasonix_render::decode_only::run_decode_only;

#[test]
fn counts_and_emits_one_line_per_frame() {
    let input = concat!(
        r#"{"schemaVersion":1,"cols":80,"rows":24,"root":{"kind":"text","runs":[{"text":"a"}]}}"#,
        "\n",
        r#"{"schemaVersion":1,"cols":80,"rows":24,"root":{"kind":"text","runs":[{"text":"b"}]}}"#,
        "\n",
    );
    let mut out = Vec::<u8>::new();
    let count = run_decode_only(input.as_bytes(), &mut out).expect("decode");
    assert_eq!(count, 2);
    let s = String::from_utf8(out).expect("utf8");
    assert!(s.contains("frame 1"), "{s}");
    assert!(s.contains("frame 2"), "{s}");
}

#[test]
fn skips_blank_lines_and_keeps_counting() {
    let input = concat!(
        "\n",
        r#"{"schemaVersion":1,"cols":80,"rows":24,"root":{"kind":"text","runs":[{"text":"a"}]}}"#,
        "\n",
        "   \n",
        r#"{"schemaVersion":1,"cols":80,"rows":24,"root":{"kind":"text","runs":[{"text":"b"}]}}"#,
        "\n",
    );
    let mut out = Vec::<u8>::new();
    let count = run_decode_only(input.as_bytes(), &mut out).expect("decode");
    assert_eq!(count, 2);
}

#[test]
fn surfaces_decode_errors_with_line_context() {
    let input = concat!(
        r#"{"schemaVersion":1,"cols":80,"rows":24,"root":{"kind":"text","runs":[{"text":"a"}]}}"#,
        "\n",
        r#"{not valid json"#,
        "\n",
    );
    let mut out = Vec::<u8>::new();
    let err = run_decode_only(input.as_bytes(), &mut out).unwrap_err();
    assert!(err.to_string().contains("line 2"), "{err}");
}
