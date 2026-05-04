import { Text, useInput } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useRef } from "react";
import { FG } from "./theme/tokens.js";

export interface MaskedInputProps {
  value: string;
  onChange: (next: string) => void;
  onSubmit: (final: string) => void;
  mask?: string;
  placeholder?: string;
}

/** Windows ConPTY splits bracketed-paste wrappers across stdin chunks; Ink's parser sees them as printable `[`, `2`, `0`, `0`, `~` and they leak into the buffer. Strip them at the input boundary and again at submit. */
function stripPasteMarkers(s: string): string {
  // biome-ignore lint/suspicious/noControlCharactersInRegex: ESC (0x1b) is exactly what we're stripping — bracketed-paste wrappers and stray escape bytes leaked from Ink's parser.
  return s.replace(/\u001b?\[20[01]~/g, "").replace(/\u001b/g, "");
}

export function MaskedInput({
  value,
  onChange,
  onSubmit,
  mask = "•",
  placeholder = "",
}: MaskedInputProps): React.ReactElement {
  const valueRef = useRef(value);
  valueRef.current = value;

  useInput((input, key) => {
    if (key.return) {
      onSubmit(stripPasteMarkers(valueRef.current));
      return;
    }
    if (key.backspace || key.delete) {
      if (valueRef.current.length === 0) return;
      const next = valueRef.current.slice(0, -1);
      valueRef.current = next;
      onChange(next);
      return;
    }
    if (input && !key.ctrl && !key.meta && !key.escape) {
      const cleaned = stripPasteMarkers(input);
      if (cleaned.length === 0) return;
      const next = stripPasteMarkers(valueRef.current + cleaned);
      valueRef.current = next;
      onChange(next);
    }
  });

  if (value.length === 0) {
    if (placeholder.length === 0) {
      return <Text inverse> </Text>;
    }
    return (
      <>
        <Text inverse>{placeholder[0]}</Text>
        <Text color={FG.faint}>{placeholder.slice(1)}</Text>
      </>
    );
  }

  return (
    <>
      <Text>{mask.repeat(value.length)}</Text>
      <Text inverse> </Text>
    </>
  );
}
