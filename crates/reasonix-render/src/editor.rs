pub fn insert_char_at(buffer: &mut String, char_idx: usize, ch: char) {
    let byte_idx = char_to_byte(buffer, char_idx);
    buffer.insert(byte_idx, ch);
}

pub fn remove_char_at(buffer: &mut String, char_idx: usize) {
    let Some((byte_idx, _)) = buffer.char_indices().nth(char_idx) else {
        return;
    };
    buffer.remove(byte_idx);
}

pub fn char_to_byte(buffer: &str, char_idx: usize) -> usize {
    buffer
        .char_indices()
        .nth(char_idx)
        .map(|(b, _)| b)
        .unwrap_or(buffer.len())
}

pub fn prev_word_boundary(buffer: &str, from: usize) -> usize {
    let chars: Vec<char> = buffer.chars().collect();
    let mut i = from.min(chars.len());
    while i > 0 && chars[i - 1].is_whitespace() {
        i -= 1;
    }
    while i > 0 && !chars[i - 1].is_whitespace() {
        i -= 1;
    }
    i
}

pub fn next_word_boundary(buffer: &str, from: usize) -> usize {
    let chars: Vec<char> = buffer.chars().collect();
    let mut i = from.min(chars.len());
    let n = chars.len();
    while i < n && !chars[i].is_whitespace() {
        i += 1;
    }
    while i < n && chars[i].is_whitespace() {
        i += 1;
    }
    i
}

pub fn locate_cursor_in(buffer: &str, cursor: usize) -> (usize, usize) {
    let mut line = 0usize;
    let mut col = 0usize;
    for (idx, ch) in buffer.chars().enumerate() {
        if idx == cursor {
            return (line, col);
        }
        if ch == '\n' {
            line += 1;
            col = 0;
        } else {
            col += 1;
        }
    }
    (line, col)
}

pub fn line_col_to_cursor(buffer: &str, target_line: usize, target_col: usize) -> usize {
    let mut line = 0usize;
    let mut col = 0usize;
    let mut idx = 0usize;
    for ch in buffer.chars() {
        if line == target_line && col == target_col {
            return idx;
        }
        if ch == '\n' {
            if line == target_line {
                return idx;
            }
            line += 1;
            col = 0;
        } else {
            col += 1;
        }
        idx += 1;
    }
    idx
}

pub fn move_cursor_line(buffer: &str, cursor: usize, delta: isize) -> usize {
    let (line, col) = locate_cursor_in(buffer, cursor);
    let new_line = if delta < 0 {
        line.saturating_sub((-delta) as usize)
    } else {
        line + delta as usize
    };
    let max_line = buffer.chars().filter(|c| *c == '\n').count();
    if new_line > max_line {
        return buffer.chars().count();
    }
    if new_line == line {
        return cursor;
    }
    line_col_to_cursor(buffer, new_line, col)
}

pub fn cursor_on_first_line(buffer: &str, cursor: usize) -> bool {
    locate_cursor_in(buffer, cursor).0 == 0
}

pub fn cursor_on_last_line(buffer: &str, cursor: usize) -> bool {
    let (line, _) = locate_cursor_in(buffer, cursor);
    let last_line = buffer.chars().filter(|c| *c == '\n').count();
    line >= last_line
}
