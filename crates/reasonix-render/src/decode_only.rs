use std::io::{BufRead, Write};

use anyhow::{Context, Result};

pub fn run_decode_only<R: BufRead, W: Write>(input: R, mut output: W) -> Result<u64> {
    let mut count = 0u64;
    for (lineno, line) in input.lines().enumerate() {
        let line = line.with_context(|| format!("read line {}", lineno + 1))?;
        if line.trim().is_empty() {
            continue;
        }
        let _value: serde_json::Value =
            serde_json::from_str(&line).with_context(|| format!("decode line {}", lineno + 1))?;
        count += 1;
        writeln!(output, "frame {count}").ok();
    }
    Ok(count)
}
