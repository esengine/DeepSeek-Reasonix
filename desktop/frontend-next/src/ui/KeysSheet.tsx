import { t } from "../i18n";
import { chord } from "./keys";
import { Group } from "./Group";

/** One row: the chord as this platform writes it, and what it does. */
const KEYS: Array<[string, string]> = [
  [chord("F"), "settings.find_in_the_conversation"],
  [chord("K"), "settings.search_conversations"],
  [chord("B"), "session.collapse_sidebar"],
  [chord("1"), "session.show_conversation"],
  [chord("2"), "session.show_run_analysis"],
  [chord("3"), "session.show_workbench"],
  [chord("`"), "terminal.open"],
  [`${chord("↑")} / ${chord("↓")}`, "settings.move_between_your_questions"],
  [chord("Shift+E"), "composer.refine_prompt"],
  [chord("Shift+F"), "settings.workbench_full_screen"],
  [`${chord("-")} / ${chord("+")} / ${chord("0")}`, "settings.make_the_interface_smaller_larger"],
  [chord("Enter"), "settings.send_when_the_send_key"],
  ["Enter / Shift+Enter", "settings.send_or_a_new_line"],
  ["Esc", "settings.close_the_open_panel"],
  ["← / → · Home", "settings.widen_a_divider_once_focused"],
];

/** Every chord the window answers to, on one screen.
 *
 *  Nothing here sets anything: it is a reference, so it carries no scope and no
 *  apply semantics. It lives beside the appearance settings because that is
 *  where someone goes when the window does not behave the way they expect. */
export function KeysSheet() {
  return (
    <Group
      id="keys"
      title={t("settings.keyboard_shortcuts")}
      hint={t("settings.all_shortcuts_incl_global_and_esc")}
    >
      {KEYS.map(([keys, what]) => (
        <div className="lrow keys-row" key={keys + what}>
          <span className="ds"><kbd>{keys}</kbd></span>
          <span className="tx"><span className="lb">{t(what)}</span></span>
        </div>
      ))}
    </Group>
  );
}
