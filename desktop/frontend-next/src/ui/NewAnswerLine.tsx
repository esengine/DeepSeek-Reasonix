import { createContext, useContext } from "react";
import { t } from "../i18n";
import "./NewAnswerLine.css";

export const NewAnswerAnchor = createContext<string | null>(null);

export function useNewAnswerAnchor(): string | null {
  return useContext(NewAnswerAnchor);
}

export function NewAnswerLine() {
  return <div className="new-answer-line" role="separator" data-new-answer=""><span>{t("newAnswerLine.newAnswersStartHere")}</span></div>;
}
