import { useCallback, useEffect, useMemo, useRef, useState, type ClipboardEvent } from "react";
import { t } from "../i18n";
import { tx } from "../i18n/rich";
import { FEEDBACK_CATEGORIES, FEEDBACK_CODE, FEEDBACK_REPO_ISSUES, type FeedbackCategory, type FeedbackEnv, type FeedbackReceipt } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { CopyButton } from "./CopyButton";
import { StudioIcon } from "./StudioIcon";
import { dropDraft, heldDraft, holdDraft } from "./feedbackdraft";
import { feedbackFailure, type FeedbackFailure } from "./feedbackfailure";
import { admit, megabytes, payload, readShot, type Refused, type Shot } from "./feedbackshots";
import { useFileDrop } from "./filedrop";

export const SECURITY_URL = "https://github.com/esengine/DeepSeek-Reasonix/security/policy";

const CATEGORY_LABEL: Record<FeedbackCategory, string> = {
  bug: "问题",
  idea: "建议",
  question: "疑问",
  other: "其他",
};

const REFUSED_LABEL = {
  format: "{name}：只支持 PNG 或 JPEG",
  too_large: "{name}：超过 {size} MB，没有添加",
  too_many: "{name}：最多 {n} 张，没有添加",
} as const;

const encoder = new TextEncoder();

function uuid(): string {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  return "fb-" + Date.now().toString(36) + Math.random().toString(36).slice(2);
}

interface Props {
  port: AgentPort;
  onMine: () => void;
  onClose: () => void;
  onFile: (url: string) => void;
}

export function FeedbackForm({ port, onMine, onClose, onFile }: Props) {
  const [env, setEnv] = useState<FeedbackEnv | null>(null);
  const [envFailure, setEnvFailure] = useState<FeedbackFailure | null>(null);
  const [category, setCategory] = useState<FeedbackCategory>(() => heldDraft().category);
  const [body, setBody] = useState(() => heldDraft().body);
  const [contact, setContact] = useState(() => heldDraft().contact);
  const [name, setName] = useState("");
  const [shots, setShots] = useState<Shot[]>(() => heldDraft().shots);
  const [refused, setRefused] = useState<Refused[]>([]);
  const [over, setOver] = useState(false);
  const [phase, setPhase] = useState<"editing" | "sending" | "sent">("editing");
  const [failure, setFailure] = useState<FeedbackFailure | null>(null);
  const [sent, setSent] = useState<FeedbackReceipt | null>(null);
  const key = useRef(uuid());
  const attempt = useRef("");
  const picker = useRef<HTMLInputElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const submit = useRef<HTMLButtonElement>(null);
  const done = useRef<HTMLHeadingElement>(null);
  const heldShots = useRef(shots);
  heldShots.current = shots;

  const load = useCallback(() => {
    setEnvFailure(null);
    port
      .feedbackEnv(document.documentElement.lang)
      .then((e) => {
        setEnv(e);
        setName((prev) => prev || e.displayName);
        requestAnimationFrame(() => {
          if (document.activeElement?.closest('[role="tablist"]')) field.current?.focus();
        });
      })
      .catch((e) => setEnvFailure(feedbackFailure(e)));
  }, [port]);

  useEffect(load, [load]);

  useEffect(() => {
    if (phase !== "sent") holdDraft({ category, body, contact, shots });
  }, [category, body, contact, shots, phase]);

  useEffect(() => {
    if (phase === "sent") done.current?.focus();
  }, [phase]);

  useEffect(() => {
    if (failure) submit.current?.focus();
  }, [failure]);

  const limits = env?.limits;

  useEffect(() => {
    if (!limits) return;
    setShots((prev) => {
      const fit = prev.filter((s) => s.bytes <= limits.uploadBytes).slice(0, limits.images);
      return fit.length === prev.length ? prev : fit;
    });
  }, [limits]);

  const add = useCallback(
    async (files: File[]) => {
      if (!limits || files.length === 0) return;
      const { taken, refused: no } = admit(heldShots.current.length, files, limits);
      setRefused(no);
      if (taken.length === 0) return;
      const read = await Promise.allSettled(taken.map(readShot));
      const ok = read.flatMap((r) => (r.status === "fulfilled" ? [r.value] : []));
      setShots((prev) => [...prev, ...ok].slice(0, limits.images));
    },
    [limits],
  );

  const zone = useFileDrop((d) => void add(d.files), setOver);

  const onPaste = (e: ClipboardEvent<HTMLFormElement>) => {
    const files = [...e.clipboardData.files].filter((f) => f.type.startsWith("image/"));
    if (files.length === 0) return;
    e.preventDefault();
    void add(files);
  };

  const bytes = useMemo(() => encoder.encode(body).length, [body]);
  const nameChars = [...name.trim()].length;
  const contactChars = [...contact.trim()].length;
  const problem = !limits
    ? "env"
    : body.trim() === ""
      ? "body"
      : bytes > limits.bodyBytes
        ? "body-long"
        : nameChars === 0
          ? "name"
          : nameChars > limits.nameChars
            ? "name-long"
            : contactChars > limits.contactChars
              ? "contact-long"
              : "";
  const sending = phase === "sending";

  const send = async () => {
    if (problem || sending) return;
    const sig = JSON.stringify([category, body, name, contact, shots.map((s) => s.id)]);
    if (sig !== attempt.current) key.current = uuid();
    attempt.current = sig;
    setPhase("sending");
    setFailure(null);
    try {
      const receipt = await port.sendFeedback({
        idempotencyKey: key.current,
        category,
        body: body.trim(),
        displayName: name.trim(),
        contact: contact.trim() || undefined,
        locale: document.documentElement.lang,
        images: payload(shots),
      });
      dropDraft();
      setSent(receipt);
      setPhase("sent");
    } catch (e) {
      setFailure(feedbackFailure(e));
      setPhase("editing");
    }
  };

  const again = () => {
    key.current = uuid();
    attempt.current = "";
    setBody("");
    setShots([]);
    setRefused([]);
    setSent(null);
    setFailure(null);
    setPhase("editing");
  };

  if (phase === "sent" && sent) {
    return (
      <div className="fbk-sent" data-stage="sent">
        <StudioIcon name="check" />
        <h3 ref={done} tabIndex={-1}>{t("已收到你的反馈")}</h3>
        <p className="fbk-hint">{t("回执号是这份反馈的凭据，请留着它。我们会先看一遍，只有被登记为 GitHub 议题的内容才会公开。进展、议题链接，以及我们的回复或提问，都会出现在「我的反馈」里。")}</p>
        <div className="fbk-receipt">
          <code aria-label={t("回执号")}>{sent.receipt}</code>
          <CopyButton text={sent.receipt} label={t("复制回执号")} />
        </div>
        {sent.redacted && (
          <p className="fbk-note" data-tone="info">
            <StudioIcon name="shield" />
            {t("你的文字里有看起来像密钥的内容，已在发出前打码。")}
          </p>
        )}
        <div className="fbk-acts">
          <button className="act" data-primary data-action="feedback.mine" onClick={onMine}>{t("查看我的反馈")}</button>
          <button className="act" data-action="feedback.another" onClick={again}>{t("再写一条")}</button>
          <button className="act" data-action="feedback.close" onClick={onClose}>{t("关闭")}</button>
        </div>
      </div>
    );
  }

  const room = limits ? limits.images - shots.length : 0;

  return (
    <form
      className="fbk-form"
      ref={zone}
      data-over={over ? "" : undefined}
      aria-busy={sending}
      noValidate
      data-action-paste="feedback.shot.add"
      data-action-submit="feedback.send"
      onPaste={onPaste}
      onSubmit={(e) => {
        e.preventDefault();
        void send();
      }}
    >
      <section className="fbk-notice" aria-labelledby="fbk-public-t">
        <StudioIcon name="warning" />
        <div>
          <h3 id="fbk-public-t">{t("只有被我们登记为议题的反馈才会公开")}</h3>
          <p>{t("如果登记为 GitHub 议题，你的文字、昵称和截图会发布在 esengine/DeepSeek-Reasonix 仓库，任何人都看得到；不登记就不会公开。你会在这里收到回执，登记后还会看到议题链接，我们也可能在这里回复或向你提问。")}</p>
          <p data-tone="warn">{t("请不要在文字或截图里写入密钥、账号密码、聊天记录或私有代码：一旦公开就收不回来。")}</p>
          <p>
            {tx("发现的是安全漏洞？请不要在这里提交，{link}。", {
              link: (
                <a href={SECURITY_URL} data-action="feedback.link" onClick={(e) => { e.preventDefault(); onFile(SECURITY_URL); }}>
                  {t("按安全策略私下报告")}
                </a>
              ),
            })}
          </p>
        </div>
      </section>

      {envFailure && (
        <div className="fbk-note" role="alert" data-tone="error">
          <StudioIcon name="warning" />
          <span>{envFailure.message}</span>
          <button type="button" className="btn sm" data-action="feedback.retry-env" onClick={load}>{t("重试")}</button>
        </div>
      )}

      <fieldset className="fbk-fields" disabled={sending || !env}>
        <legend className="sr-only">{t("反馈内容")}</legend>

        <div className="fbk-cats" role="radiogroup" aria-label={t("反馈类别")}>
          {FEEDBACK_CATEGORIES.map((c) => (
            <label key={c} className="fbk-cat">
              <input type="radio" name="fbk-category" checked={category === c} onChange={() => setCategory(c)} data-action="feedback.category" data-value={c} />
              <span>{t(CATEGORY_LABEL[c])}</span>
            </label>
          ))}
        </div>

        <label className="fbk-field">
          <span className="fbk-label">{t("发生了什么？")}</span>
          <textarea
            ref={field}
            data-action="feedback.body"
            value={body}
            rows={6}
            placeholder={t("描述你遇到的情况，或你希望它怎样。")}
            aria-invalid={problem === "body-long" ? true : undefined}
            aria-describedby="fbk-count"
            onChange={(e) => setBody(e.target.value)}
          />
          <span id="fbk-count" className="fbk-count" data-over={problem === "body-long" ? "" : undefined}>
            {t("{n} / {max} 字节", { n: bytes.toLocaleString(), max: (limits?.bodyBytes ?? 0).toLocaleString() })}
          </span>
        </label>

        <div className="fbk-row">
          <label className="fbk-field">
            <span className="fbk-label">{t("昵称（登记为议题时公开）")}</span>
            <input
              data-action="feedback.name"
              value={name}
              autoComplete="nickname"
              aria-invalid={problem === "name-long" ? true : undefined}
              onChange={(e) => setName(e.target.value)}
            />
            <span className="fbk-hint">{t("如果登记为议题，会显示为「由 {name} 提交」。用昵称即可，不必真名。", { name: name.trim() || "…" })}</span>
          </label>
          <label className="fbk-field">
            <span className="fbk-label">{t("联系方式（可选，私密）")}</span>
            <input
              data-action="feedback.contact"
              value={contact}
              autoComplete="off"
              aria-invalid={problem === "contact-long" ? true : undefined}
              onChange={(e) => setContact(e.target.value)}
            />
            <span className="fbk-hint">{t("邮箱或 QQ。只有维护者看得到，绝不会出现在 GitHub 上。")}</span>
          </label>
        </div>

        <div className="fbk-field">
          <span className="fbk-label" id="fbk-shots-t">{t("截图（可选，登记为议题时公开）")}</span>
          <div className="fbk-drop" role="group" aria-labelledby="fbk-shots-t">
            {shots.length > 0 && (
              <ul className="fbk-shots">
                {shots.map((s) => (
                  <li key={s.id}>
                    <img src={s.preview} alt={s.name} />
                    <span className="fbk-shot-name">{s.name}</span>
                    <button
                      type="button"
                      className="fbk-shot-x"
                      data-action="feedback.shot.remove"
                      data-target={s.id}
                      aria-label={t("移除截图 {name}", { name: s.name })}
                      onClick={() => setShots((prev) => prev.filter((x) => x.id !== s.id))}
                    >
                      <StudioIcon name="close" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
            <div className="fbk-drop-line">
              <button type="button" className="btn sm" data-action="feedback.shot.pick" disabled={room <= 0} onClick={() => picker.current?.click()}>
                <StudioIcon name="plus" />
                {t("添加截图…")}
              </button>
              <span className="fbk-hint">
                {limits
                  ? t("也可以直接粘贴或拖进来。PNG 或 JPEG，最多 {n} 张，每张不超过 {size} MB，较大的图会自动缩小。", { n: limits.images, size: megabytes(limits.uploadBytes) })
                  : ""}
              </span>
            </div>
            <input
              ref={picker}
              className="sr-only"
              type="file"
              accept="image/png,image/jpeg"
              multiple
              tabIndex={-1}
              aria-label={t("选择截图文件")}
              data-action="feedback.shot.pick"
              onChange={(e) => {
                void add([...(e.target.files ?? [])]);
                e.target.value = "";
              }}
            />
          </div>
          <span className="fbk-hint" data-tone="warn">{t("如果这份反馈被登记为公开议题，截图会和文字一起公开。发送前请确认没有露出密钥、聊天记录或私有代码。")}</span>
          {refused.length > 0 && (
            <ul className="fbk-refused" role="status">
              {refused.map((r, i) => (
                <li key={i}>{t(REFUSED_LABEL[r.why], { name: r.name, size: megabytes(limits?.uploadBytes ?? 0), n: limits?.images ?? 0 })}</li>
              ))}
            </ul>
          )}
        </div>
      </fieldset>

      {env && (
        <section className="fbk-env" aria-labelledby="fbk-env-t">
          <h3 id="fbk-env-t">{t("随反馈一起发送的环境信息")}</h3>
          <dl>
            <dt>{t("版本")}</dt>
            <dd>{env.env.version}{env.env.commit ? ` (${env.env.commit})` : ""}</dd>
            <dt>{t("系统")}</dt>
            <dd>{[env.env.os, env.env.osVersion, env.env.arch].filter(Boolean).join(" ")}</dd>
            <dt>{t("入口")}</dt>
            <dd>{env.env.surface}</dd>
            <dt>{t("语言")}</dt>
            <dd>{env.env.locale}</dd>
            <dt>{t("渠道")}</dt>
            <dd>{env.env.channel}</dd>
            <dt>{t("模型服务")}</dt>
            <dd>{env.env.providerKind}</dd>
          </dl>
          <p className="fbk-hint">{t("只读。不包含密钥、地址或任何文件内容。")}</p>
        </section>
      )}

      <div className="fbk-foot">
        {failure && (
          <div className="fbk-note" role="alert" data-tone="error" data-code={failure.code}>
            <StudioIcon name="warning" />
            <span>{failure.message}</span>
            {failure.code === FEEDBACK_CODE.duplicate && (
              <button type="button" className="btn sm" data-action="feedback.mine" onClick={onMine}>{t("查看我的反馈")}</button>
            )}
            {(failure.code === FEEDBACK_CODE.disabled || failure.code === FEEDBACK_CODE.unavailable) && (
              <button type="button" className="btn sm" data-action="feedback.link" onClick={() => onFile(FEEDBACK_REPO_ISSUES + "new/choose")}>
                {t("去 GitHub")}
              </button>
            )}
          </div>
        )}

        <div className="fbk-acts">
          <button className="act" type="submit" ref={submit} data-primary disabled={problem !== "" || sending}>
            {t(sending ? "正在发送…" : failure?.retry === "same" ? "重试发送" : "发送反馈")}
          </button>
          <button className="act" type="button" data-action="feedback.close" onClick={onClose}>{t("取消")}</button>
        </div>
      </div>
    </form>
  );
}
