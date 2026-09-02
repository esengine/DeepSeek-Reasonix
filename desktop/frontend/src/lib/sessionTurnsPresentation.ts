import type { Translator } from "./i18n";

type SessionTurns = {
  turns?: number;
  turnsState?: string;
};

export function sessionTurnsLabel(session: SessionTurns, t: Translator): string {
  if (session.turnsState === "unknown") return t("history.indexing");
  if (typeof session.turns === "number") return t("composer.sessionTurns", { n: session.turns });
  return "";
}
