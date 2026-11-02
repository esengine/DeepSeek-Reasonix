import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { TooltipButton } from "./TooltipButton";
import { ReplyMenu } from "./cards/ReplyMenu";
import { copyText } from "./CopyButton";
import { copyAll } from "./clipboard";

/** The copy control on a message. A plain click takes everything the message
 *  is — the words, its pictures, and the stored paths that let a paste in the
 *  same workspace put the pictures back. The second entry takes only what the
 *  person wrote: no pictures, and nothing the host addressed for them. */
export function MessageCopy({ text, images, imageURL }: { text: string; images: string[]; imageURL: (path: string) => string }) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  const [menu, setMenu] = useState(false);
  const more = useRef<HTMLButtonElement>(null);
  const timer = useRef<number | null>(null);

  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current); }, []);

  const settle = (copy: Promise<void>) => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    copy
      .then(() => setState("done"))
      .catch(() => setState("failed"))
      .finally(() => { timer.current = window.setTimeout(() => setState("idle"), 1600); });
  };
  const everything = () => settle(copyAll({ text, images: images.map((path) => ({ path, url: imageURL(path) })) }));
  const words = () => {
    setMenu(false);
    settle(copyText(text));
  };

  const said = state === "done" ? t("已复制") : state === "failed" ? t("复制失败，请重试") : "";
  // A message with nothing to restore is just a copy; one that carries pictures
  // says what the plain click takes, because the second entry takes less.
  const label = t(images.length === 0 ? "复制" : "复制全部");
  return (
    <span className="msgcopy">
      <TooltipButton
        type="button"
        className="copy"
        data-action="message.copy-all"
        data-state={state}
        data-feedback={state !== "idle" ? "" : undefined}
        title={said || label}
        aria-label={said || label}
        onClick={everything}
      >
        <StudioIcon name={state === "done" ? "check" : state === "failed" ? "warning" : "copy"} />
        {/* Icon-only, so the label is what a screen reader reads and what it
            announces when the copy lands or is refused. */}
        <span className={state === "idle" ? "sr-only" : undefined} aria-live="polite">{said || label}</span>
      </TooltipButton>
      {images.length > 0 && (
        <>
          <TooltipButton
            ref={more}
            type="button"
            className="copy msgcopy-more"
            data-action="message.copy-menu"
            title={t("更多复制方式")}
            aria-label={t("更多复制方式")}
            aria-haspopup="menu"
            aria-expanded={menu}
            onClick={() => setMenu((open) => !open)}
          >
            <StudioIcon name="down" />
          </TooltipButton>
          <ReplyMenu anchor={more} open={menu} onClose={() => setMenu(false)}>
            <button type="button" role="menuitem" data-action="message.copy-text" onClick={words}>
              <StudioIcon name="copy" /><span>{t("仅复制文字")}</span>
            </button>
          </ReplyMenu>
        </>
      )}
    </span>
  );
}
