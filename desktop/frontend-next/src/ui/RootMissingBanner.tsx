import { t } from "../i18n";
import { say } from "../i18n/kernel";
import type { Item } from "../state/session";

const ROOT_MISSING = "tool.root_missing";

// The newest tool call decides: once a later call runs, the folder is back and the
// banner goes. It keys on the host's refusal code, never on the wording.
export function rootMissing(items: Item[]): boolean {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item.t === "tool") return item.tool.refusalCode === ROOT_MISSING;
  }
  return false;
}

export function RootMissingBanner({ items, onPick }: { items: Item[]; onPick: () => void }) {
  if (!rootMissing(items)) return null;
  return (
    <div className="cmpalert">
      <div className="errbar" data-kind="note" data-cause="root-missing" role="alert">
        <span>{say({ code: ROOT_MISSING }, "")}</span>
        <button data-action="root-missing.reselect" onClick={onPick}>{t("app.choose_the_folder_again")}</button>
      </div>
    </div>
  );
}
