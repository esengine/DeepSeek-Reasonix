import chalk from 'chalk';
import React, { useEffect, useState } from 'react';
import type { Key } from '../events/input-event.js';
import useInput from '../hooks/use-input.js';
import Text from './Text.js';

export type TextInputProps = {
  /** The current text. Controlled — caller owns the state. */
  readonly value: string;
  /** Fired with the next text on every keystroke that mutates `value`. */
  readonly onChange: (value: string) => void;
  /** Fired on Enter with the value at submit time. */
  readonly onSubmit?: (value: string) => void;
  /** Shown in place of the value while it's empty. */
  readonly placeholder?: string;
  /** When `true`, `useInput` is active and the cursor is drawn. */
  readonly focus?: boolean;
  /** Character to render in place of every value character. */
  readonly mask?: string;
  /** Whether to draw a visible cursor block. */
  readonly showCursor?: boolean;
  /** Highlight the range that the most recent input chunk inserted. */
  readonly highlightPastedText?: boolean;
};

/** Single-line controlled text input. */
export default function TextInput({
  value: originalValue,
  onChange,
  onSubmit,
  placeholder = '',
  focus = true,
  mask,
  showCursor = true,
  highlightPastedText = false,
}: TextInputProps): React.ReactElement {
  const [state, setState] = useState({
    cursorOffset: (originalValue || '').length,
    cursorWidth: 0,
  });
  const { cursorOffset, cursorWidth } = state;

  useEffect(() => {
    setState((prev) => {
      if (!focus || !showCursor) return prev;
      const nextValue = originalValue || '';
      // Parent shrank `value` past where our cursor was sitting — snap
      // the cursor to the new end so we don't render an empty inverse
      // cell past the visible characters.
      if (prev.cursorOffset > nextValue.length - 1) {
        return { cursorOffset: nextValue.length, cursorWidth: 0 };
      }
      return prev;
    });
  }, [originalValue, focus, showCursor]);

  const cursorActualWidth = highlightPastedText ? cursorWidth : 0;
  const value = mask ? mask.repeat(originalValue.length) : originalValue;

  let renderedValue = value;
  let renderedPlaceholder = placeholder ? chalk.grey(placeholder) : undefined;

  if (showCursor && focus) {
    renderedPlaceholder =
      placeholder.length > 0
        ? chalk.inverse(placeholder[0]) + chalk.grey(placeholder.slice(1))
        : chalk.inverse(' ');
    renderedValue = value.length > 0 ? '' : chalk.inverse(' ');
    let i = 0;
    for (const char of value) {
      renderedValue +=
        i >= cursorOffset - cursorActualWidth && i <= cursorOffset
          ? chalk.inverse(char)
          : char;
      i++;
    }
    if (value.length > 0 && cursorOffset === value.length) {
      renderedValue += chalk.inverse(' ');
    }
  }

  useInput(
    (input: string, key: Key) => {
      // Pass through navigation and exit keys so the surrounding app
      // can still respond to them while a text field is focused.
      if (
        key.upArrow ||
        key.downArrow ||
        (key.ctrl && input === 'c') ||
        key.tab ||
        (key.shift && key.tab)
      ) {
        return;
      }
      if (key.return) {
        onSubmit?.(originalValue);
        return;
      }

      let nextCursorOffset = cursorOffset;
      let nextValue = originalValue;
      let nextCursorWidth = 0;

      if (key.leftArrow) {
        if (showCursor) nextCursorOffset--;
      } else if (key.rightArrow) {
        if (showCursor) nextCursorOffset++;
      } else if (key.backspace || key.delete) {
        if (cursorOffset > 0) {
          nextValue =
            originalValue.slice(0, cursorOffset - 1) +
            originalValue.slice(cursorOffset, originalValue.length);
          nextCursorOffset--;
        }
      } else {
        nextValue =
          originalValue.slice(0, cursorOffset) +
          input +
          originalValue.slice(cursorOffset, originalValue.length);
        nextCursorOffset += input.length;
        // Multi-character chunks are almost always pastes; remember the
        // span so `highlightPastedText` can render the inserted range
        // inverse on the next paint.
        if (input.length > 1) nextCursorWidth = input.length;
      }

      if (nextCursorOffset < 0) nextCursorOffset = 0;
      if (nextCursorOffset > nextValue.length) nextCursorOffset = nextValue.length;

      setState({ cursorOffset: nextCursorOffset, cursorWidth: nextCursorWidth });

      if (nextValue !== originalValue) onChange(nextValue);
    },
    { isActive: focus },
  );

  return (
    <Text>{placeholder ? value.length > 0 ? renderedValue : renderedPlaceholder : renderedValue}</Text>
  );
}
