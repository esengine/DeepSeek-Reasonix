import { Fragment } from "react";
import {
  formatShortcutCombo,
  formatShortcutComboParts,
  type ShortcutCombo,
  type ShortcutPlatform,
} from "../lib/keyboardShortcuts";

export function ShortcutComboDisplay({
  combo,
  platform,
  as = "span",
  className,
}: {
  combo: ShortcutCombo;
  platform: ShortcutPlatform;
  as?: "span" | "kbd";
  className?: string;
}) {
  const Tag = as;
  const label = formatShortcutCombo(combo, platform);
  const parts = formatShortcutComboParts(combo, platform);
  const separator = platform === "darwin" ? null : "+";
  return (
    <Tag className={`shortcut-combo${className ? ` ${className}` : ""}`} aria-label={label}>
      {parts.map((part, index) => (
        <Fragment key={`${part}-${index}`}>
          {index > 0 && separator && (
            <span className="shortcut-combo__separator" aria-hidden="true">
              {separator}
            </span>
          )}
          <span className="shortcut-combo__part">{part}</span>
        </Fragment>
      ))}
    </Tag>
  );
}
