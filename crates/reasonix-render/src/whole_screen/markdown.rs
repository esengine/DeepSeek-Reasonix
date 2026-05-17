use pulldown_cmark::{Alignment, CodeBlockKind, Event, HeadingLevel, Options, Parser, Tag, TagEnd};

#[derive(Clone, Debug, Default)]
pub struct InlineStyle {
    pub bold: bool,
    pub italic: bool,
    pub code: bool,
    pub strike: bool,
    pub link: bool,
}

#[derive(Clone, Debug)]
pub struct InlineSpan {
    pub text: String,
    pub style: InlineStyle,
}

#[derive(Clone, Copy, Debug)]
pub enum CellAlign {
    Left,
    Center,
    Right,
}

#[derive(Clone, Debug)]
pub enum MdBlock {
    Paragraph(Vec<InlineSpan>),
    Heading {
        level: u8,
        spans: Vec<InlineSpan>,
    },
    Code {
        #[allow(dead_code)]
        lang: String,
        text: String,
    },
    ListItem {
        ordered: bool,
        index: u32,
        depth: u8,
        spans: Vec<InlineSpan>,
    },
    BlockQuote(Vec<InlineSpan>),
    Hr,
    Table {
        aligns: Vec<CellAlign>,
        head: Vec<Vec<InlineSpan>>,
        rows: Vec<Vec<Vec<InlineSpan>>>,
    },
}

pub fn parse(text: &str) -> Vec<MdBlock> {
    let mut opts = Options::empty();
    opts.insert(Options::ENABLE_STRIKETHROUGH);
    opts.insert(Options::ENABLE_TASKLISTS);
    opts.insert(Options::ENABLE_TABLES);
    let parser = Parser::new_ext(text, opts);

    let mut out: Vec<MdBlock> = Vec::new();
    let mut style = InlineStyle::default();
    let mut current_spans: Vec<InlineSpan> = Vec::new();
    let mut current_kind: Option<TagKind> = None;
    let mut list_stack: Vec<ListFrame> = Vec::new();
    let mut code_lang = String::new();
    let mut code_buf = String::new();
    let mut in_code = false;
    let mut table_aligns: Vec<CellAlign> = Vec::new();
    let mut table_head: Vec<Vec<InlineSpan>> = Vec::new();
    let mut table_rows: Vec<Vec<Vec<InlineSpan>>> = Vec::new();
    let mut current_row: Vec<Vec<InlineSpan>> = Vec::new();

    for ev in parser {
        match ev {
            Event::Start(Tag::Paragraph) => {
                start_block(
                    &mut out,
                    &mut current_spans,
                    &mut current_kind,
                    TagKind::Paragraph,
                );
            }
            Event::End(TagEnd::Paragraph) => {
                end_block(
                    &mut out,
                    &mut current_spans,
                    &mut current_kind,
                    list_stack.last(),
                );
            }
            Event::Start(Tag::Heading { level, .. }) => {
                start_block(
                    &mut out,
                    &mut current_spans,
                    &mut current_kind,
                    TagKind::Heading(heading_level(level)),
                );
            }
            Event::End(TagEnd::Heading(_)) => {
                end_block(
                    &mut out,
                    &mut current_spans,
                    &mut current_kind,
                    list_stack.last(),
                );
            }
            Event::Start(Tag::CodeBlock(kind)) => {
                in_code = true;
                code_lang = match kind {
                    CodeBlockKind::Fenced(s) => {
                        s.split_whitespace().next().unwrap_or("").to_string()
                    }
                    CodeBlockKind::Indented => String::new(),
                };
                code_buf.clear();
            }
            Event::End(TagEnd::CodeBlock) => {
                let text = code_buf.trim_end_matches('\n').to_string();
                out.push(MdBlock::Code {
                    lang: std::mem::take(&mut code_lang),
                    text,
                });
                in_code = false;
                code_buf.clear();
            }
            Event::Start(Tag::BlockQuote(_)) => {
                start_block(
                    &mut out,
                    &mut current_spans,
                    &mut current_kind,
                    TagKind::BlockQuote,
                );
            }
            Event::End(TagEnd::BlockQuote(_)) => {
                end_block(
                    &mut out,
                    &mut current_spans,
                    &mut current_kind,
                    list_stack.last(),
                );
            }
            Event::Start(Tag::List(start)) => {
                list_stack.push(ListFrame {
                    ordered: start.is_some(),
                    next_index: start.unwrap_or(1) as u32,
                    depth: list_stack.len() as u8,
                });
            }
            Event::End(TagEnd::List(_)) => {
                list_stack.pop();
            }
            Event::Start(Tag::Item) => {
                let frame = list_stack.last().copied();
                start_block(
                    &mut out,
                    &mut current_spans,
                    &mut current_kind,
                    TagKind::ListItem,
                );
                if let Some(f) = frame {
                    current_kind = Some(TagKind::ListItemFor(f));
                }
            }
            Event::End(TagEnd::Item) => {
                end_block(
                    &mut out,
                    &mut current_spans,
                    &mut current_kind,
                    list_stack.last(),
                );
                if let Some(frame) = list_stack.last_mut() {
                    if frame.ordered {
                        frame.next_index += 1;
                    }
                }
            }
            Event::Rule => {
                out.push(MdBlock::Hr);
            }
            Event::Start(Tag::Table(aligns)) => {
                table_aligns = aligns.into_iter().map(map_align).collect();
                table_head.clear();
                table_rows.clear();
            }
            Event::End(TagEnd::Table) => {
                out.push(MdBlock::Table {
                    aligns: std::mem::take(&mut table_aligns),
                    head: std::mem::take(&mut table_head),
                    rows: std::mem::take(&mut table_rows),
                });
            }
            Event::Start(Tag::TableHead) => {
                current_row = Vec::new();
            }
            Event::End(TagEnd::TableHead) => {
                table_head = std::mem::take(&mut current_row);
            }
            Event::Start(Tag::TableRow) => {
                current_row = Vec::new();
            }
            Event::End(TagEnd::TableRow) => {
                table_rows.push(std::mem::take(&mut current_row));
            }
            Event::Start(Tag::TableCell) => {
                current_spans.clear();
            }
            Event::End(TagEnd::TableCell) => {
                current_row.push(std::mem::take(&mut current_spans));
            }
            Event::Start(Tag::Strong) => {
                style.bold = true;
            }
            Event::End(TagEnd::Strong) => {
                style.bold = false;
            }
            Event::Start(Tag::Emphasis) => {
                style.italic = true;
            }
            Event::End(TagEnd::Emphasis) => {
                style.italic = false;
            }
            Event::Start(Tag::Strikethrough) => {
                style.strike = true;
            }
            Event::End(TagEnd::Strikethrough) => {
                style.strike = false;
            }
            Event::Start(Tag::Link { .. }) => {
                style.link = true;
            }
            Event::End(TagEnd::Link) => {
                style.link = false;
            }
            Event::Code(s) => {
                let mut code_style = style.clone();
                code_style.code = true;
                current_spans.push(InlineSpan {
                    text: s.into_string(),
                    style: code_style,
                });
            }
            Event::Text(s) => {
                if in_code {
                    code_buf.push_str(&s);
                } else {
                    current_spans.push(InlineSpan {
                        text: s.into_string(),
                        style: style.clone(),
                    });
                }
            }
            Event::SoftBreak | Event::HardBreak if !in_code => {
                current_spans.push(InlineSpan {
                    text: " ".to_string(),
                    style: style.clone(),
                });
            }
            _ => {}
        }
    }
    end_block(
        &mut out,
        &mut current_spans,
        &mut current_kind,
        list_stack.last(),
    );
    out
}

fn map_align(a: Alignment) -> CellAlign {
    match a {
        Alignment::Left | Alignment::None => CellAlign::Left,
        Alignment::Center => CellAlign::Center,
        Alignment::Right => CellAlign::Right,
    }
}

fn heading_level(level: HeadingLevel) -> u8 {
    match level {
        HeadingLevel::H1 => 1,
        HeadingLevel::H2 => 2,
        HeadingLevel::H3 => 3,
        HeadingLevel::H4 => 4,
        HeadingLevel::H5 => 5,
        HeadingLevel::H6 => 6,
    }
}

#[derive(Clone, Copy, Debug)]
struct ListFrame {
    ordered: bool,
    next_index: u32,
    depth: u8,
}

#[derive(Clone, Copy, Debug)]
enum TagKind {
    Paragraph,
    Heading(u8),
    BlockQuote,
    ListItem,
    ListItemFor(ListFrame),
}

fn start_block(
    out: &mut Vec<MdBlock>,
    spans: &mut Vec<InlineSpan>,
    kind: &mut Option<TagKind>,
    new_kind: TagKind,
) {
    if !spans.is_empty() {
        flush(out, std::mem::take(spans), *kind, None);
    }
    *kind = Some(new_kind);
}

fn end_block(
    out: &mut Vec<MdBlock>,
    spans: &mut Vec<InlineSpan>,
    kind: &mut Option<TagKind>,
    active_frame: Option<&ListFrame>,
) {
    let taken = std::mem::take(spans);
    let k = kind.take();
    if !taken.is_empty() || matches!(k, Some(TagKind::ListItem) | Some(TagKind::ListItemFor(_))) {
        flush(out, taken, k, active_frame);
    }
}

fn flush(
    out: &mut Vec<MdBlock>,
    spans: Vec<InlineSpan>,
    kind: Option<TagKind>,
    active_frame: Option<&ListFrame>,
) {
    match kind {
        Some(TagKind::Heading(level)) => out.push(MdBlock::Heading { level, spans }),
        Some(TagKind::BlockQuote) => out.push(MdBlock::BlockQuote(spans)),
        Some(TagKind::ListItem) => {
            let frame = active_frame.copied().unwrap_or(ListFrame {
                ordered: false,
                next_index: 1,
                depth: 0,
            });
            out.push(MdBlock::ListItem {
                ordered: frame.ordered,
                index: frame.next_index,
                depth: frame.depth,
                spans,
            });
        }
        Some(TagKind::ListItemFor(frame)) => out.push(MdBlock::ListItem {
            ordered: frame.ordered,
            index: frame.next_index,
            depth: frame.depth,
            spans,
        }),
        Some(TagKind::Paragraph) | None => {
            if !spans.is_empty() {
                out.push(MdBlock::Paragraph(spans));
            }
        }
    }
}
