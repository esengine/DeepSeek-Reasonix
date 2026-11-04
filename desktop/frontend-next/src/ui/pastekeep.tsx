import type { Dispatch, SetStateAction } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, KeptText } from "../port/port";

// A long paste held beside the box. saved is set once a person has had the host
// keep the text as a file; what the message carries from then on is its pointer.
export type PasteChip = {
  k: "paste";
  id: string;
  body: string;
  lines: number;
  name?: string;
  saved?: KeptText;
  saving?: boolean;
  error?: string;
};

type Held = { id: string; k: string };

// Saving is a choice made per paste; nothing is moved on its own. A failure
// leaves the text in the chip, so the paste can still be sent as it is.
export function keepPasteAsFile<T extends Held>(port: AgentPort, chip: PasteChip, set: Dispatch<SetStateAction<T[]>>) {
  const change = (patch: Partial<PasteChip>) => set((all) => all.map((x) => (x.id === chip.id && x.k === "paste" ? ({ ...x, ...patch } as T) : x)));
  change({ saving: true, error: "" });
  port.keepText(chip.body).then(
    (saved) => change({ saving: false, saved }),
    (e: unknown) => change({ saving: false, error: reason(e) }),
  );
}

export function PasteKeep({ chip, onKeep }: { chip: PasteChip; onKeep: (chip: PasteChip) => void }) {
  return (
    <>
      {chip.saved ? (
        <span className="sz" title={chip.saved.path}>{t("composer.saved_as_a_file_the")}</span>
      ) : (
        <button className="save" data-action="paste.save" disabled={chip.saving} onClick={() => onKeep(chip)}>
          {chip.saving ? t("common.saving_2") : t("composer.save_as_file")}
        </button>
      )}
      {chip.error && <span className="why" role="alert" title={chip.error}>{chip.error}</span>}
    </>
  );
}
