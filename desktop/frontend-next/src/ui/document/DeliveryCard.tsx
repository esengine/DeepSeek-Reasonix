import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { DeliveredFile } from "../../port/workspace";
import type { ReplyActions } from "../cards/SayCard";
import { copyText } from "../CopyButton";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import "./delivery.css";
import { StudioIcon } from "../StudioIcon";
import { TooltipButton } from "../TooltipButton";
import { grantedFilePath } from "../grantedfiles";
import { DeliveryMenu, type DeliveryAction } from "./DeliveryMenu";
import { FileTypeIcon } from "./FileTypeIcon";
import { fileReference } from "../fileReference";

export function DeliveryCard({ files, reply }: { files: DeliveredFile[]; reply?: ReplyActions }) {
  const counts = new Map<string, number>();
  for (const file of files) {
    const name = file.path.split("/").at(-1)!;
    counts.set(name, (counts.get(name) ?? 0) + 1);
  }
  return <div className="delivery-files" aria-label={t("交付文件")}>
    <div className="delivery-grid" data-single={files.length === 1 ? "" : undefined}>
      {files.map((file) => <DeliveryEntry key={JSON.stringify([file.root, file.path])}
        file={file} reply={reply} showLocation={(counts.get(file.path.split("/").at(-1)!) ?? 0) > 1} />)}
    </div>
  </div>;
}

function DeliveryEntry({ file, reply, showLocation }: { file: DeliveredFile; reply?: ReplyActions; showLocation: boolean }) {
  const path = file.root ? `${file.root.replace(/[\\/]+$/, "")}/${file.path}` : file.path;
  const href = path.split("/").map(encodeURIComponent).join("/");
  const open = reply?.files?.file?.(file) ?? reply?.files?.link(href);
  const locate = reply?.files?.locate?.(href);
  const absolute = file.root ? path : reply?.files?.title?.(href) || path;
  const reference = fileReference(path);
  const name = file.path.split("/").at(-1)!;
  const dot = name.lastIndexOf(".");
  const stem = dot > 0 ? name.slice(0, dot) : name, extension = dot > 0 ? name.slice(dot) : "";
  const current = reply?.activeFile === (file.root ? grantedFilePath(file.root, file.path) : file.path);
  const location = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : file.root;
  const description = file.description?.trim() || extension.slice(1).toUpperCase() || t("文件");
  const [menu, setMenu] = useState(false);
  const close = useCallback(() => setMenu(false), []);
  const trigger = useRef<HTMLButtonElement>(null);
  const [feedback, setFeedback] = useState<{ kind: "busy" | "success" | "error"; text: string }>();
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const live = useRef(true);
  const detailID = useId();
  useEffect(() => { live.current = true; return () => { live.current = false; clearTimeout(timer.current); }; }, []);
  const run = async (action: () => void | Promise<void>, success: string) => {
    if (feedback?.kind === "busy") return;
    clearTimeout(timer.current);
    setFeedback({ kind: "busy", text: t("正在处理…") });
    try {
      await action();
      if (!live.current) return;
      setFeedback({ kind: "success", text: success });
      timer.current = setTimeout(() => { if (live.current) setFeedback(undefined); }, 3000);
    } catch (error) {
      if (live.current) setFeedback({ kind: "error", text: reason(error) });
    }
  };
  const actions: DeliveryAction[] = [];
  if (locate) actions.push({ kind: "locate", label: t("在文件树中显示"), icon: "folder", run: locate });
  if (reply?.onAddFile) actions.push({ kind: "add", label: t("添加到对话"), icon: "plus", disabled: !reference,
    title: reference ? undefined : t("此名称不能表示为 @ 引用"),
    run: () => void run(() => reply.onAddFile!(reference), t("已添加到输入框")) });
  if (reply?.openDeliveredFile && /\.(md|markdown|txt|csv|tsv|pdf|html?|svg|docx?|xlsx?|pptx?)$/i.test(path))
    actions.push({ kind: "open", label: t("用系统应用打开"), icon: "external", run: () => void run(() => reply.openDeliveredFile!(absolute), t("已请求系统应用打开")) });
  if (!file.root && reply?.revealDeliveredFile)
    actions.push({ kind: "reveal", label: t("在文件夹中显示"), icon: "folder", run: () => void run(() => reply.revealDeliveredFile!(file.path), t("已请求显示文件位置")) });
  actions.push({ kind: "copy", label: t("复制路径"), icon: "copy", run: () => void run(() => copyText(absolute), t("已复制路径")) });
  return <div className="delivery-file" data-current={current ? "" : undefined}
    data-action-contextmenu="delivery.more" data-action-keydown="delivery.more"
    onContextMenu={(event) => {
      if (document.getSelection()?.isCollapsed === false || feedback?.kind === "busy") return;
      event.preventDefault(); event.stopPropagation(); trigger.current?.focus(); setMenu(true);
    }}
    onKeyDown={(event) => {
      if (event.key !== "ContextMenu" && !(event.key === "F10" && event.shiftKey)) return;
      if (feedback?.kind === "busy") return;
      event.preventDefault(); event.stopPropagation(); trigger.current?.focus(); setMenu(true);
    }}>
    <button type="button" className="delivery-open" data-action="delivery.preview" data-target={path} disabled={!open} title={`${absolute}\n${description}`}
      aria-label={t("预览 {name}", { name })} aria-describedby={detailID} aria-current={current ? "true" : undefined} onClick={() => open?.()}>
      <FileTypeIcon path={file.path} />
      <span className="delivery-details">
        <span className="delivery-heading">
          <strong className="delivery-name"><span>{stem}</span><span className="delivery-extension">{extension}</span></strong>
          {current && <span className="delivery-current">{t("查看中")}</span>}
        </span>
        {showLocation && <small className="delivery-location" title={absolute}>{location || t("项目根目录")}</small>}
        <small id={detailID} className="delivery-description">{description}</small>
      </span>
    </button>
    <TooltipButton ref={trigger} className="delivery-more" data-action="delivery.more" data-target={path} title={t("更多操作")} aria-label={t("更多操作：{name}", { name })}
      aria-haspopup="menu" aria-expanded={menu} disabled={feedback?.kind === "busy"} onClick={() => setMenu((value) => !value)}>
      <span>{t("更多")}</span><StudioIcon name="down" />
    </TooltipButton>
    {menu && <DeliveryMenu anchor={trigger} label={t("文件操作：{name}", { name })} target={path} actions={actions} onClose={close} />}
    {feedback && <div className="delivery-feedback" role={feedback.kind === "error" ? "alert" : "status"} data-kind={feedback.kind}>{feedback.text}</div>}
  </div>;
}
