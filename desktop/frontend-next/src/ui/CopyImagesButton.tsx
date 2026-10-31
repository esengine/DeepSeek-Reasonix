import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { StudioIcon } from "./StudioIcon";
import { Picker, type MenuItem } from "./Menu";
import { copyText } from "./CopyButton";
import { annotateImagePath } from "./imagepaths";
import "../styles/copy-split.css";

// A message that carries images has two copies: the words alone, or the words
// with the images the message was about. The button copies the second, because
// that is what a reader pasting the message into another turn wants; the arrow
// beside it offers both, so the other answer is one click away instead of a
// guess about what a copy button does.

const COPY_ALL = "all";
const COPY_TEXT = "text";

// How long an image gets to answer before it counts as unreadable. A copy is
// a small thing to ask for; a spinner is not.
const imageTimeout = 4000;

export function CopyImagesButton({
  text,
  images,
  imageURL,
  label,
  onNotice,
}: {
  text: string;
  images: string[];
  imageURL: (path: string) => string;
  label?: string;
  // What the reader is told when a copy could not do what it was asked to do.
  // An empty string means the copy went through.
  onNotice?: (message: string) => void;
}) {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");
  const timer = useRef<number | null>(null);

  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current); }, []);

  const settle = (next: "idle" | "done" | "failed") => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    setState(next);
    timer.current = window.setTimeout(() => { setState("idle"); timer.current = null; }, 1600);
  };

  const run = async (mode: string) => {
    let payload = text;
    // A notice outlives the copy that caused it: a degraded copy that then
    // succeeded must not have its reason wiped by the success.
    let notice = "";
    if (mode === COPY_ALL && images.length > 0) {
      const why = await unreadableImage(images, imageURL);
      if (why) {
        // A path pointing at a file that is not there is worse than no path at
        // all: the copy falls back to the words, and says why it did.
        notice = t("图片读取失败，已降级为仅复制文字：{reason}", { reason: why });
        payload = text;
      } else {
        payload = [...images.map(annotateImagePath), text].filter(Boolean).join(" ");
      }
    }
    try {
      await copyText(payload);
      onNotice?.(notice);
      settle("done");
    } catch (e) {
      // A refused clipboard is not a silent nothing: the reader is told, and
      // told what to do instead.
      onNotice?.(reason(e));
      settle("failed");
    }
  };

  const items: MenuItem[] = [
    { value: COPY_ALL, label: t("全部复制"), desc: t("复制文字与图片路径") },
    { value: COPY_TEXT, label: t("仅复制文字"), desc: t("只复制这条消息的文字") },
  ];

  const word = state === "done" ? t("已复制") : state === "failed" ? t("复制失败，请重试") : label ?? t("复制");

  return (
    <span className="copy-split">
      <button
        className="copy"
        type="button"
        data-action="turn.copy"
        data-state={state}
        data-feedback={state !== "idle" ? "" : undefined}
        onClick={() => void run(COPY_ALL)}
        aria-label={word}
        title={word}
      >
        <StudioIcon name={state === "done" ? "check" : state === "failed" ? "warning" : "copy"} />
        <span className="sr-only" aria-live="polite">{word}</span>
      </button>
      <Picker
        className="copy copy-arrow"
        label={<StudioIcon name="down" />}
        items={items}
        onPick={(value) => void run(value)}
        place="top"
        align="end"
        ariaLabel={t("更多复制选项")}
        triggerAction="turn.copy-menu"
        data-action="turn.copy-menu"
      />
    </span>
  );
}

// The image is read before its path goes into the clipboard: a path that
// points at a file which is not there is a broken promise to whoever pastes
// it later. The first image that cannot be read names the reason.
async function unreadableImage(images: string[], imageURL: (path: string) => string): Promise<string> {
  for (const path of images) {
    const why = await imageProblem(imageURL(path));
    if (why) return why;
  }
  return "";
}

async function imageProblem(url: string): Promise<string> {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(imageTimeout) });
    if (!res.ok) return t("图片无法读取（{code}）", { code: res.status });
    const kind = res.headers.get("content-type") ?? "";
    if (kind && !kind.startsWith("image/")) return t("图片格式无法识别：{type}", { type: kind });
    return "";
  } catch (e) {
    return reason(e);
  }
}
