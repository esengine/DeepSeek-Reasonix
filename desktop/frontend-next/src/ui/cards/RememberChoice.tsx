import { t } from "../../i18n";

export type Remembered = "session" | "always";

const LABEL: Record<Remembered, string> = {
  session: "rememberChoice.allowThisKindForThis",
  always: "rememberChoice.alwaysAllowThisKindStop",
};

/** The quieter half of a prompt's footer: the choices that answer this question
 *  and every later one of its kind. Which of them is drawn is the kernel's call
 *  (`options` names what it will honour here), and the line under them says where
 *  each one is kept, so remembering is never a surprise. One component so every
 *  card that offers it words and places it the same way. */
export function RememberChoice({ options, target, busy, disabled, onChoose }: {
  options: readonly Remembered[];
  target: string;
  busy: string;
  disabled: boolean;
  onChoose: (choice: Remembered) => void;
}) {
  if (options.length === 0) return null;
  return (
    <>
      {options.map((choice) => (
        <button key={choice} className="btn" data-weak="" data-action="decision.tool" data-target={target} data-value={choice}
          aria-describedby={`remember-where-${target}`} disabled={disabled} onClick={() => onChoose(choice)}>
          {busy === choice ? t("common.submitting") : t(LABEL[choice])}
        </button>
      ))}
    </>
  );
}

const SESSION_KEPT = "rememberChoice.allowForThisSessionLasts";

// Where an always answer is kept, for the questions that do not write a
// permission rule. Keyed on the tool id the kernel puts on the card.
const ALWAYS_KEPT_AT: Record<string, string> = {
  paid_search: "rememberChoice.alwaysAllowAddsThisPaid",
  network_egress: "rememberChoice.alwaysAllowAddsThisDomain",
  mcp_signin: "rememberChoice.alwaysAllowRecordsThisSign",
};

const ALWAYS_KEPT_AS_RULE = "rememberChoice.alwaysAllowIsSavedAs";

/** Where the remembered choices live, as one sentence for the footer's note.
 *  "Always" is a permission rule for an ordinary tool and something else for the
 *  questions in ALWAYS_KEPT_AT, so the sentence follows the tool. */
export function RememberWhere({ options, target, tool }: { options: readonly Remembered[]; target: string; tool?: string }) {
  if (options.length === 0) return null;
  const always = (tool && ALWAYS_KEPT_AT[tool]) || ALWAYS_KEPT_AS_RULE;
  const text = [options.includes("session") && SESSION_KEPT, options.includes("always") && always].filter((x): x is string => !!x).map((x) => t(x)).join(" ");
  return <div className="apv-note" id={`remember-where-${target}`}>{text}</div>;
}
