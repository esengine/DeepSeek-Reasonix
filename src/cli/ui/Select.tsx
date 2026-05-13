/** Arrow-key list components for Ink — single-select and multi-select. */

import { Box, Text } from "ink";
import React, { useState } from "react";
import { useKeystroke } from "./keystroke-context.js";
import type { KeyEvent } from "./stdin-reader.js";
import { type UiColor, useColor } from "./theme.js";

export interface SelectItem<V extends string = string> {
  value: V;
  label: string;
  /** Optional descriptive text rendered dimmed. */
  hint?: string;
  /** Disabled rows render dimmed and are skipped on nav. */
  disabled?: boolean;
}

export interface SingleSelectProps<V extends string> {
  items: SelectItem<V>[];
  initialValue?: V;
  onSubmit: (value: V) => void;
  onCancel?: () => void;
  /** Fired when Tab is pressed on the currently highlighted item. */
  onTab?: (value: V) => void;
  /** Optional dim footer beneath the list. */
  footer?: string;
  /** Render item hints on the same row as the label instead of a second row. */
  inlineHints?: boolean;
  /** Ignore matching keystrokes so an enclosing component can own them. */
  ignoreKey?: (ev: KeyEvent) => boolean;
}

export function SingleSelect<V extends string>({
  items,
  initialValue,
  onSubmit,
  onTab,
  onCancel,
  footer,
  inlineHints = false,
  ignoreKey,
}: SingleSelectProps<V>) {
  const color = useColor();
  const initialIndex = Math.max(
    0,
    items.findIndex((i) => i.value === initialValue && !i.disabled),
  );
  const [index, setIndex] = useState(initialIndex === -1 ? 0 : initialIndex);

  useKeystroke((ev) => {
    if (ev.paste || ignoreKey?.(ev)) return;
    if (ev.upArrow) {
      setIndex((i) => findNextEnabled(items, i, -1));
    } else if (ev.downArrow) {
      setIndex((i) => findNextEnabled(items, i, +1));
    } else if (ev.return) {
      const chosen = items[index];
      if (chosen && !chosen.disabled) onSubmit(chosen.value);
    } else if (ev.tab) {
      const chosen = items[index];
      if (chosen && !chosen.disabled) onTab?.(chosen.value);
    } else if (ev.escape && onCancel) {
      onCancel();
    }
  });

  return (
    <Box flexDirection="column">
      {items.map((item, i) => (
        <SelectRow
          key={item.value}
          item={item}
          active={i === index}
          marker={i === index ? "▸" : " "}
          color={color}
          inlineHint={inlineHints}
        />
      ))}
      {footer ? (
        <Box marginTop={1}>
          <Text dimColor>{footer}</Text>
        </Box>
      ) : null}
    </Box>
  );
}

export interface MultiSelectProps<V extends string> {
  items: SelectItem<V>[];
  initialSelected?: V[];
  onSubmit: (values: V[]) => void;
  onCancel?: () => void;
  /** Footer hint under the list — e.g. "[Space] toggle · [Enter] confirm". */
  footer?: string;
  /** Render item hints on the same row as the label instead of a second row. */
  inlineHints?: boolean;
  /** Ignore matching keystrokes so an enclosing component can own them. */
  ignoreKey?: (ev: KeyEvent) => boolean;
}

export function MultiSelect<V extends string>({
  items,
  initialSelected = [],
  onSubmit,
  onCancel,
  footer,
  inlineHints = false,
  ignoreKey,
}: MultiSelectProps<V>) {
  const color = useColor();
  const [index, setIndex] = useState(() => {
    const first = items.findIndex((i) => !i.disabled);
    return first === -1 ? 0 : first;
  });
  const [selected, setSelected] = useState<Set<V>>(new Set(initialSelected));

  useKeystroke((ev) => {
    if (ev.paste || ignoreKey?.(ev)) return;
    if (ev.upArrow) {
      setIndex((i) => findNextEnabled(items, i, -1));
    } else if (ev.downArrow) {
      setIndex((i) => findNextEnabled(items, i, +1));
    } else if (ev.input === " ") {
      const item = items[index];
      if (!item || item.disabled) return;
      setSelected((prev) => {
        const next = new Set(prev);
        if (next.has(item.value)) next.delete(item.value);
        else next.add(item.value);
        return next;
      });
    } else if (ev.return) {
      const ordered = items.filter((i) => selected.has(i.value)).map((i) => i.value);
      onSubmit(ordered);
    } else if (ev.escape && onCancel) {
      onCancel();
    }
  });

  return (
    <Box flexDirection="column">
      {items.map((item, i) => {
        const checked = selected.has(item.value);
        const marker = checked ? "[x]" : "[ ]";
        return (
          <SelectRow
            key={item.value}
            item={item}
            active={i === index}
            marker={`${i === index ? "▸" : " "} ${marker}`}
            color={color}
            inlineHint={inlineHints}
          />
        );
      })}
      {footer ? (
        <Box marginTop={1}>
          <Text dimColor>{footer}</Text>
        </Box>
      ) : null}
    </Box>
  );
}

function SelectRow<V extends string>({
  item,
  active,
  marker,
  color,
  inlineHint = false,
}: {
  item: SelectItem<V>;
  active: boolean;
  marker: string;
  color: UiColor;
  inlineHint?: boolean;
}) {
  const rowColor = item.disabled ? color.info : active ? color.primary : undefined;
  const labelText = `${marker} ${item.label}`;
  if (inlineHint) {
    return (
      <Box flexDirection="row" flexWrap="nowrap" minHeight={1}>
        <Text color={rowColor} bold={active} dimColor={item.disabled} wrap="truncate">
          {labelText}
        </Text>
        {item.hint ? <Text dimColor wrap="truncate">{`  ${item.hint}`}</Text> : null}
      </Box>
    );
  }
  return (
    <Box flexDirection="column">
      <Box>
        <Text color={rowColor} bold={active} dimColor={item.disabled}>
          {labelText}
        </Text>
      </Box>
      {item.hint ? (
        <Box paddingLeft={marker.length + 1}>
          <Text dimColor>{item.hint}</Text>
        </Box>
      ) : null}
    </Box>
  );
}

function findNextEnabled<V extends string>(
  items: SelectItem<V>[],
  from: number,
  step: -1 | 1,
): number {
  if (items.length === 0) return 0;
  let i = from;
  for (let tries = 0; tries < items.length; tries++) {
    i = (i + step + items.length) % items.length;
    if (!items[i]?.disabled) return i;
  }
  return from;
}
