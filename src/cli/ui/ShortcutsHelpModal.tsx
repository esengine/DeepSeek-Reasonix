import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../i18n/index.js";
import { FG, SURFACE, TONE } from "./theme/tokens.js";

interface ShortcutEntry {
  keys: string;
  descKey: string;
}

interface ShortcutGroup {
  titleKey: string;
  items: ShortcutEntry[];
}

const SHORTCUT_GROUPS: ShortcutGroup[] = [
  {
    titleKey: "shortcutsHelp.groupInput",
    items: [
      { keys: "Enter", descKey: "shortcutsHelp.descEnter" },
      { keys: "Shift+Enter", descKey: "shortcutsHelp.descShiftEnter" },
      { keys: "Ctrl+U", descKey: "shortcutsHelp.descCtrlU" },
      { keys: "Ctrl+W", descKey: "shortcutsHelp.descCtrlW" },
      { keys: "Ctrl+P", descKey: "shortcutsHelp.descCtrlP" },
      { keys: "Ctrl+X", descKey: "shortcutsHelp.descCtrlX" },
    ],
  },
  {
    titleKey: "shortcutsHelp.groupNavigation",
    items: [
      { keys: "\u2191/\u2193", descKey: "shortcutsHelp.descArrows" },
      { keys: "PgUp/PgDn", descKey: "shortcutsHelp.descPgUpDown" },
      { keys: "Ctrl+L", descKey: "shortcutsHelp.descCtrlL" },
      { keys: "Ctrl+B", descKey: "shortcutsHelp.descCtrlB" },
    ],
  },
  {
    titleKey: "shortcutsHelp.groupSession",
    items: [
      { keys: "/new", descKey: "shortcutsHelp.descNewSession" },
      { keys: "/sessions", descKey: "shortcutsHelp.descListSessions" },
      { keys: "/model", descKey: "shortcutsHelp.descSwitchModel" },
      { keys: "/preset", descKey: "shortcutsHelp.descSwitchPreset" },
      { keys: "/theme", descKey: "shortcutsHelp.descSwitchTheme" },
    ],
  },
  {
    titleKey: "shortcutsHelp.groupSystem",
    items: [
      { keys: "Ctrl+C", descKey: "shortcutsHelp.descCtrlC" },
      { keys: "Esc", descKey: "shortcutsHelp.descEsc" },
      { keys: "Ctrl+R", descKey: "shortcutsHelp.descCtrlR" },
      { keys: "Ctrl+O", descKey: "shortcutsHelp.descCtrlO" },
      { keys: "/help", descKey: "shortcutsHelp.descHelp" },
      { keys: "Shift+Tab", descKey: "shortcutsHelp.descShiftTab" },
    ],
  },
];

export function ShortcutsHelpModal(): React.ReactElement {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="#649ed2"
      paddingX={1}
      paddingY={0}
      backgroundColor={SURFACE.bg}
    >
      <Box justifyContent="center">
        <Text bold color="#649ed2">
          {` ${t("shortcutsHelp.title")} `}
        </Text>
      </Box>
      {SHORTCUT_GROUPS.map((group, groupIdx) => (
        <Box
          key={group.titleKey}
          flexDirection="column"
          marginBottom={groupIdx < SHORTCUT_GROUPS.length - 1 ? 0 : 0}
        >
          <Text color={TONE.accent}>{`  ${t(group.titleKey)}`}</Text>
          {group.items.map((item) => (
            <Box key={item.keys}>
              <Text bold color="#649ed2">
                {`    ${item.keys}`}
              </Text>
              <Text color={FG.faint}>{`  ${t(item.descKey)}`}</Text>
            </Box>
          ))}
        </Box>
      ))}
    </Box>
  );
}
