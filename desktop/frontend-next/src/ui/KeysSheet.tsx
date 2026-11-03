import { t } from "../i18n";
import { chord } from "./keys";
import { Group } from "./Group";

/** One row: the chord as this platform writes it, and what it does. */
const KEYS: Array<[string, string]> = [
  [chord("F"), "keysSheet.findInTheConversation"],
  [chord("K"), "keysSheet.searchConversations"],
  [chord("1"), "common.showConversation"],
  [chord("2"), "common.showRunAnalysis"],
  [chord("3"), "common.showWorkbench"],
  [`${chord("↑")} / ${chord("↓")}`, "keysSheet.moveBetweenYourQuestions"],
  [chord("Shift+E"), "common.refinePrompt"],
  [chord("Shift+F"), "keysSheet.workbenchFullScreen"],
  [`${chord("-")} / ${chord("+")} / ${chord("0")}`, "keysSheet.makeTheInterfaceSmallerLarger"],
  [chord("Enter"), "keysSheet.sendWhenTheSendKey"],
  ["Enter / Shift+Enter", "keysSheet.sendOrANewLine"],
  ["Esc", "keysSheet.closeTheOpenPanel"],
  ["← / → · Home", "keysSheet.widenADividerOnceFocused"],
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
      title={t("common.keyboardShortcuts")}
      hint={t("keysSheet.theseKeysWorkInsideThe")}
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
