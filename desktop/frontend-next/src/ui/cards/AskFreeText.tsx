import type { KeyboardEvent, Ref } from "react";
import type { AgentPort } from "../../port/port";
import type { useIme } from "../ime";
import { usePromptRefine } from "../PromptRefine";

interface Props {
  value: string;
  label: string;
  placeholder: string;
  readOnly: boolean;
  inputRef: Ref<HTMLInputElement>;
  onChange: (text: string) => void;
  onFocus?: () => void;
  onEnter: () => void;
  isIme: (e: { isComposing?: boolean; keyCode?: number }) => boolean;
  imeHandlers: ReturnType<typeof useIme>["handlers"];
  refine?: AgentPort["refinePrompt"];
}

// The free-text answer of one question. A rewrite is offered the way the
// composer offers it, by the same hook, so the keys and the card behave alike;
// the box is one line, so an adopted rewrite is joined onto one.
export function AskFreeText({ value, label, placeholder, readOnly, inputRef, onChange, onFocus, onEnter, isIme, imeHandlers, refine: ask }: Props) {
  const refine = usePromptRefine({ refinePrompt: ask ?? NO_REFINE }, value, (next) => onChange(next.replace(/\s*\n+\s*/g, " ").trim()));
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (isIme(e.nativeEvent)) return;
    if (ask && !readOnly && refine.onKey(e)) return;
    if (e.key !== "Enter" || readOnly) return;
    e.preventDefault();
    onEnter();
  };
  return (
    <>
      <div className="other-field">
        <input
          ref={inputRef}
          data-action-keydown="ask.answer"
          aria-label={label}
          value={value}
          readOnly={readOnly}
          placeholder={placeholder}
          onChange={(e) => onChange(e.target.value)}
          onFocus={onFocus}
          onKeyDown={onKeyDown}
          {...imeHandlers}
        />
        {ask && !readOnly && refine.button}
      </div>
      {ask && !readOnly && refine.card}
    </>
  );
}

const NO_REFINE = async (draft: string) => draft;
