import { formatTerminalOutputForComposer, stripTerminalControlSequences } from "../lib/terminalOutput";

function eq<T>(actual: T, expected: T, message: string): void {
  if (actual !== expected) throw new Error(`${message}: got ${String(actual)}, want ${String(expected)}`);
}

const ansi = "\u001b[31mred\u001b[0m \u001b]0;terminal title\u0007ok";
eq(stripTerminalControlSequences(ansi), "red ok", "ANSI and OSC sequences are removed");
eq(stripTerminalControlSequences("a\u009b31mb"), "ab", "C1 CSI sequences are removed");
eq(stripTerminalControlSequences("a\u009d0;terminal title\u0007b"), "ab", "C1 OSC sequences terminated by BEL are removed");
eq(stripTerminalControlSequences("a\u0090private payload\u009cb"), "ab", "C1 string sequences terminated by ST are removed");
eq(stripTerminalControlSequences("a\u001bPprivate payload\u001b\\b"), "ab", "ESC string sequences terminated by ST are removed");
eq(stripTerminalControlSequences("a\u001b(Bb"), "ab", "multi-byte ESC character-set sequences are removed");
eq(stripTerminalControlSequences("a\u0000b\u0007c\tline\r\nnext"), "abc\tline\nnext", "unsafe controls are removed while layout whitespace remains");

const fenced = formatTerminalOutputForComposer("before ``` inside\nlast");
eq(fenced.startsWith("````console\n"), true, "the outer fence grows beyond embedded backticks");
eq(fenced.endsWith("\n````"), true, "the matching closing fence is emitted");
eq(fenced.includes("before ``` inside"), true, "terminal text remains intact inside the fenced block");

const clipped = formatTerminalOutputForComposer("0123456789", 4);
eq(clipped, "```console\n6789\n```", "large output keeps the newest bounded tail");
eq(formatTerminalOutputForComposer("\u001b[2J\u001b[H\u0000"), "", "output with no printable content is not inserted");

console.log("terminal-output: ok");
