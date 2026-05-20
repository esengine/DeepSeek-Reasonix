import { useEffect } from "react";

const TEXT_INPUT_SELECTOR =
  'input, textarea, [contenteditable=""], [contenteditable="true"], [contenteditable="plaintext-only"]';

function getTextAssistAttrs() {
  return {
    autoCorrect: "off" as const,
    autoCapitalize: "off" as const,
    spellCheck: false as const,
  };
}

function isMacPlatform() {
  if (typeof document === "undefined") return false;
  return document.documentElement.dataset.platform === "macos";
}

function applyTextAssistAttrs(element: Element) {
  if (!(element instanceof HTMLElement)) return;
  if (
    !(element instanceof HTMLInputElement) &&
    !(element instanceof HTMLTextAreaElement) &&
    !element.isContentEditable
  ) {
    return;
  }

  const attrs = getTextAssistAttrs();
  element.setAttribute("autocorrect", attrs.autoCorrect);
  element.setAttribute("autocapitalize", attrs.autoCapitalize);
  element.setAttribute("spellcheck", String(attrs.spellCheck));
  element.spellcheck = attrs.spellCheck;
}

function applyWithin(root: ParentNode) {
  if (root instanceof Element) {
    applyTextAssistAttrs(root);
  }
  root.querySelectorAll(TEXT_INPUT_SELECTOR).forEach(applyTextAssistAttrs);
}

export function useDisableTextAssist() {
  useEffect(() => {
    if (typeof document === "undefined" || !isMacPlatform()) return;

    const root = document.body;
    if (!root) return;

    applyWithin(root);

    const handleFocusIn = (event: FocusEvent) => {
      if (event.target instanceof Element) {
        applyTextAssistAttrs(event.target);
      }
    };

    const observer = new MutationObserver((mutations) => {
      for (const mutation of mutations) {
        mutation.addedNodes.forEach((node) => {
          if (node instanceof Element) {
            applyWithin(node);
          }
        });
      }
    });

    document.addEventListener("focusin", handleFocusIn, true);
    observer.observe(root, { childList: true, subtree: true });

    return () => {
      document.removeEventListener("focusin", handleFocusIn, true);
      observer.disconnect();
    };
  }, []);
}
