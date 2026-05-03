import { describe, expect, it } from "vitest";
import { parseKeystrokes } from "../../../src/renderer/input/keystroke.js";

describe("parseKeystrokes — printable characters", () => {
  it("plain ASCII letter", () => {
    const [k] = parseKeystrokes("a");
    expect(k?.input).toBe("a");
    expect(k?.ctrl).toBe(false);
    expect(k?.meta).toBe(false);
  });

  it("digits and punctuation", () => {
    const keys = parseKeystrokes("1!");
    expect(keys.map((k) => k.input)).toEqual(["1", "!"]);
  });
});

describe("parseKeystrokes — control characters", () => {
  it("\\r is return", () => {
    expect(parseKeystrokes("\r")[0]!.return).toBe(true);
  });
  it("\\n is also return", () => {
    expect(parseKeystrokes("\n")[0]!.return).toBe(true);
  });
  it("\\t is tab", () => {
    expect(parseKeystrokes("\t")[0]!.tab).toBe(true);
  });
  it("0x7f and 0x08 are backspace", () => {
    expect(parseKeystrokes("\x7f")[0]!.backspace).toBe(true);
    expect(parseKeystrokes("\x08")[0]!.backspace).toBe(true);
  });
  it("Ctrl+A maps to ctrl=true, input='a'", () => {
    const k = parseKeystrokes("\x01")[0]!;
    expect(k.ctrl).toBe(true);
    expect(k.input).toBe("a");
  });
  it("Ctrl+Z maps to ctrl=true, input='z'", () => {
    const k = parseKeystrokes("\x1a")[0]!;
    expect(k.ctrl).toBe(true);
    expect(k.input).toBe("z");
  });
});

describe("parseKeystrokes — escape and meta", () => {
  it("standalone ESC is escape:true", () => {
    expect(parseKeystrokes("\x1b")[0]!.escape).toBe(true);
  });

  it("ESC followed by a printable char is meta+char", () => {
    const k = parseKeystrokes("\x1ba")[0]!;
    expect(k.meta).toBe(true);
    expect(k.input).toBe("a");
  });
});

describe("parseKeystrokes — CSI arrows", () => {
  it("up arrow", () => {
    expect(parseKeystrokes("\x1b[A")[0]!.upArrow).toBe(true);
  });
  it("down arrow", () => {
    expect(parseKeystrokes("\x1b[B")[0]!.downArrow).toBe(true);
  });
  it("right arrow", () => {
    expect(parseKeystrokes("\x1b[C")[0]!.rightArrow).toBe(true);
  });
  it("left arrow", () => {
    expect(parseKeystrokes("\x1b[D")[0]!.leftArrow).toBe(true);
  });
});

describe("parseKeystrokes — CSI named keys", () => {
  it("Home via CSI H", () => {
    expect(parseKeystrokes("\x1b[H")[0]!.home).toBe(true);
  });
  it("End via CSI F", () => {
    expect(parseKeystrokes("\x1b[F")[0]!.end).toBe(true);
  });
  it("Page Up via CSI 5 ~", () => {
    expect(parseKeystrokes("\x1b[5~")[0]!.pageUp).toBe(true);
  });
  it("Page Down via CSI 6 ~", () => {
    expect(parseKeystrokes("\x1b[6~")[0]!.pageDown).toBe(true);
  });
  it("Delete via CSI 3 ~", () => {
    expect(parseKeystrokes("\x1b[3~")[0]!.delete).toBe(true);
  });
});

describe("parseKeystrokes — modifier-encoded CSI", () => {
  it("Shift+ArrowUp via CSI 1;2 A", () => {
    const k = parseKeystrokes("\x1b[1;2A")[0]!;
    expect(k.upArrow).toBe(true);
    expect(k.shift).toBe(true);
  });

  it("Ctrl+ArrowRight via CSI 1;5 C", () => {
    const k = parseKeystrokes("\x1b[1;5C")[0]!;
    expect(k.rightArrow).toBe(true);
    expect(k.ctrl).toBe(true);
  });

  it("Alt+Shift+ArrowDown via CSI 1;4 B", () => {
    const k = parseKeystrokes("\x1b[1;4B")[0]!;
    expect(k.downArrow).toBe(true);
    expect(k.shift).toBe(true);
    expect(k.meta).toBe(true);
  });
});

describe("parseKeystrokes — SGR mouse wheel", () => {
  it("button 64 → wheelUp", () => {
    const k = parseKeystrokes("\x1b[<64;10;5M")[0]!;
    expect(k.wheelUp).toBe(true);
    expect(k.wheelDown).toBe(false);
  });
  it("button 65 → wheelDown", () => {
    const k = parseKeystrokes("\x1b[<65;10;5M")[0]!;
    expect(k.wheelDown).toBe(true);
    expect(k.wheelUp).toBe(false);
  });
  it("'m' release variant works the same way", () => {
    const k = parseKeystrokes("\x1b[<64;10;5m")[0]!;
    expect(k.wheelUp).toBe(true);
  });
});

describe("parseKeystrokes — sequences in one chunk", () => {
  it("paste of two characters yields two events", () => {
    const keys = parseKeystrokes("ab");
    expect(keys.map((k) => k.input)).toEqual(["a", "b"]);
  });

  it("ctrl-c followed by enter", () => {
    const keys = parseKeystrokes("\x03\r");
    expect(keys[0]!.ctrl).toBe(true);
    expect(keys[0]!.input).toBe("c");
    expect(keys[1]!.return).toBe(true);
  });

  it("up-arrow then 'q'", () => {
    const keys = parseKeystrokes("\x1b[Aq");
    expect(keys[0]!.upArrow).toBe(true);
    expect(keys[1]!.input).toBe("q");
  });
});
