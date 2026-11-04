import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react";
import { t } from "../i18n";
import { systemLabel } from "./osname";
import { memoryLines } from "./memoryrows";
import { tx } from "../i18n/rich";
import { FEEDBACK_CATEGORIES, FEEDBACK_CODE, FEEDBACK_REPO_ISSUES, type FeedbackCategory, type FeedbackEnv, type FeedbackProcessMemory, type FeedbackReceipt } from "../port/feedback";
import { host } from "../port/host";
import type { AgentPort } from "../port/port";
import { CopyButton } from "./CopyButton";
import { StudioIcon } from "./StudioIcon";
import { dropDraft, heldDraft, holdDraft, rememberContact } from "./feedbackdraft";
import { feedbackFailure, type FeedbackFailure } from "./feedbackfailure";
import { FeedbackShot } from "./FeedbackShot";
import { LazyMarkdown } from "./LazyMarkdown";
import { admit, megabytes, payload, readShot, REFUSED_LABEL, type Refused, type Shot } from "./feedbackshots";
import { useFileDrop } from "./filedrop";
import { listenAction } from "./listen";

export const SECURITY_URL = "https://github.com/esengine/DeepSeek-Reasonix/security/policy";

const CATEGORY_LABEL: Record<FeedbackCategory, string> = {
  bug: "feedback.bug",
  idea: "feedback.idea",
  question: "feedback.question",
  other: "ext.other",
};

const CATEGORY_HINT: Record<FeedbackCategory, string> = {
  bug: "feedback.bug_hint",
  idea: "feedback.idea_hint",
  question: "feedback.question_hint",
  other: "feedback.other_hint",
};

const encoder = new TextEncoder();

function uuid(): string {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  return "fb-" + Date.now().toString(36) + Math.random().toString(36).slice(2);
}

interface Props {
  port: AgentPort;
  pasteRoot: RefObject<HTMLElement | null>;
  active: boolean;
  onMine: () => void;
  onClose: () => void;
  onFile: (url: string) => void;
  onConceal: (hidden: boolean) => void;
}

export function FeedbackForm({ port, pasteRoot, active, onMine, onClose, onFile, onConceal }: Props) {
  const [env, setEnv] = useState<FeedbackEnv | null>(null);
  const [shellMemory, setShellMemory] = useState<FeedbackProcessMemory[]>([]);
  const [envFailure, setEnvFailure] = useState<FeedbackFailure | null>(null);
  const [category, setCategory] = useState<FeedbackCategory>(() => heldDraft().category);
  const [body, setBody] = useState(() => heldDraft().body);
  const [contact, setContact] = useState(() => heldDraft().contact);
  const [preview, setPreview] = useState(false);
  const [name, setName] = useState("");
  const [shots, setShots] = useState<Shot[]>(() => heldDraft().shots);
  const [refused, setRefused] = useState<Refused[]>([]);
  const [over, setOver] = useState(false);
  const [phase, setPhase] = useState<"editing" | "sending" | "sent">("editing");
  const [failure, setFailure] = useState<FeedbackFailure | null>(null);
  const [logsFailed, setLogsFailed] = useState(false);
  const [sent, setSent] = useState<FeedbackReceipt | null>(null);
  const key = useRef(uuid());
  const attempt = useRef("");
  const picker = useRef<HTMLInputElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const submit = useRef<HTMLButtonElement>(null);
  const done = useRef<HTMLHeadingElement>(null);
  const shotEpoch = useRef(0);
  const mounted = useRef(true);
  const focusFrame = useRef(0);
  useEffect(() => () => {
    mounted.current = false;
    cancelAnimationFrame(focusFrame.current);
  }, []);
  const captureButton = useRef<HTMLButtonElement>(null);
  const [capture, setCapture] = useState<"idle" | "taking" | "failed">("idle");
  const heldShots = useRef(shots);
  heldShots.current = shots;

  const load = useCallback(() => {
    setEnvFailure(null);
    port
      .feedbackEnv(document.documentElement.lang)
      .then((e) => {
        setEnv(e);
        setName((prev) => prev || e.displayName);
      })
      .catch((e) => setEnvFailure(feedbackFailure(e)));
  }, [port]);

  useEffect(load, [load]);
  useEffect(() => {
    let alive = true;
    void host().processMemory().then((rows) => alive && setShellMemory(rows));
    return () => { alive = false; };
  }, []);

  useEffect(() => {
    if (env && document.activeElement?.closest('[role="tablist"]')) field.current?.focus();
  }, [env]);

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
      const epoch = shotEpoch.current;
      const read = await Promise.allSettled(taken.map(readShot));
      if (epoch !== shotEpoch.current) return;
      const ok = read.flatMap((r) => (r.status === "fulfilled" ? [r.value] : []));
      setShots((prev) => [...prev, ...ok].slice(0, limits.images));
    },
    [limits],
  );

  const takeWindow = async () => {
    if (capture === "taking") return;
    setCapture("taking");
    onConceal(true);
    await new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(r, 60))));
    const bytes = await host().captureWindow();
    // The send panel is mounted for as long as the dialog is, so a form that is gone
    // has nothing left to unveil, and setting state then is a write into a torn-down page.
    if (!mounted.current) return;
    onConceal(false);
    setCapture(bytes ? "idle" : "failed");
    if (bytes) await add([new File([bytes as BlobPart], "window.png", { type: "image/png" })]);
    if (mounted.current) focusFrame.current = requestAnimationFrame(() => captureButton.current?.focus());
  };

  const zone = useFileDrop((d) => void add(d.files), setOver);

  // Layout phase, so the listener exists in the same commit that draws the form:
  // a paste in the frame after the form appears is taken, not dropped.
  useLayoutEffect(() => {
    const root = pasteRoot.current;
    if (!root || !active || phase !== "editing" || !limits) return;
    return listenAction(root, "paste", {
      action: "feedback.shot.add",
      listener: (event) => {
        const data = (event as ClipboardEvent).clipboardData;
        if (!data) return;
        const offered = [...data.files, ...[...(data.items ?? [])].flatMap((i) => (i.kind === "file" ? [i.getAsFile()] : []))];
        const images = offered.filter((f): f is File => f !== null && f.type.startsWith("image/"));
        const files = images.filter((f, i) => images.findIndex((g) => g.name === f.name && g.size === f.size && g.type === f.type) === i);
        if (files.length === 0) return;
        event.preventDefault();
        void add(files);
      },
    });
  }, [pasteRoot, active, phase, limits, add]);

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
        ...(shellMemory.length > 0 ? { memory: shellMemory } : {}),
      });
      shotEpoch.current++;
      rememberContact(contact.trim());
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
      <div className="fbs-sent" data-stage="sent">
        <span className="fbs-sent-mark"><StudioIcon name="check" /></span>
        <h3 ref={done} tabIndex={-1}>{t("feedback.your_feedback_was_received")}</h3>
        <div className="fbs-receipt">
          <code aria-label={t("feedback.receipt_number")}>{sent.receipt}</code>
          <CopyButton text={sent.receipt} iconOnly label={t("feedback.copy_receipt_number")} />
        </div>
        <p className="fbs-sent-note">{t("feedback.keep_the_receipt_number_it")}</p>
        {sent.underReview && <p className="fbs-sent-note" data-review="">{t("feedback.this_report_is_under_review")}</p>}
        {sent.redacted && (
          <p className="fbk-note" data-tone="info">
            <StudioIcon name="shield" />
            {t("feedback.text_in_your_report_that")}
          </p>
        )}
        <div className="fbs-sent-acts">
          <button type="button" className="fbs-btn" data-action="feedback.mine" onClick={onMine}>{t("feedback.view_my_feedback")}</button>
          <button type="button" className="fbs-btn" data-ghost="" data-action="feedback.another" onClick={again}>{t("feedback.write_another")}</button>
        </div>
      </div>
    );
  }

  const room = limits ? limits.images - shots.length : 0;
  const security = (
    <a href={SECURITY_URL} data-action="feedback.link" onClick={(e) => { e.preventDefault(); onFile(SECURITY_URL); }}>
      {t("feedback.report_it_privately_under_the")}
    </a>
  );

  return (
    <form
      className="fbs-form"
      ref={zone}
      data-over={over ? "" : undefined}
      aria-busy={sending}
      noValidate
      data-action-submit="feedback.send"
      tabIndex={-1}
      onSubmit={(e) => {
        e.preventDefault();
        void send();
      }}
    >
      {envFailure && (
        <div className="fbk-note" role="alert" data-tone="error">
          <StudioIcon name="warning" />
          <span>{envFailure.message}</span>
          <button type="button" className="btn sm" data-action="feedback.retry-env" onClick={load}>{t("common.retry")}</button>
        </div>
      )}

      <fieldset className="fbs-fields" disabled={sending || !env}>
        <legend className="sr-only">{t("feedback.feedback_details")}</legend>

        <div className="fbs-cats" role="radiogroup" aria-label={t("feedback.feedback_category")}>
          {FEEDBACK_CATEGORIES.map((c) => (
            <label key={c} className="fbs-cat" data-on={category === c ? "" : undefined}>
              <input className="sr-only" type="radio" name="fbk-category" aria-label={t(CATEGORY_LABEL[c])} checked={category === c} onChange={() => setCategory(c)} data-action="feedback.category" data-value={c} />
              <b>{t(CATEGORY_LABEL[c])}</b>
              <small>{t(CATEGORY_HINT[c])}</small>
            </label>
          ))}
        </div>

        <div className="fbs-field">
          <div className="fbs-labelrow">
            <label className="fbs-label" htmlFor="fbk-body">{t("feedback.what_happened")}</label>
            <button type="button" className="fbs-link" aria-pressed={preview} disabled={!body.trim()} onClick={() => setPreview((v) => !v)} data-action="feedback.preview">
              {t(preview ? "common.edit" : "common.preview")}
            </button>
          </div>
          {preview ? (
            <>
              <div className="fbs-preview" aria-label={t("feedback.body_preview")} role="region"><LazyMarkdown text={body} images={false} /></div>
              <span className="fbs-hint">{t("feedback.a_public_issue_shows_your")}</span>
            </>
          ) : (
            <>
              <textarea
                id="fbk-body"
                ref={field}
                className="fbs-body"
                data-action="feedback.body"
                value={body}
                rows={5}
                placeholder={t("feedback.describe_what_you_ran_into")}
                aria-invalid={problem === "body-long" ? true : undefined}
                aria-describedby="fbk-count"
                onChange={(e) => setBody(e.target.value)}
              />
              <div className="fbs-under">
                <span className="fbs-public"><b>{t("feedback.it_becomes_public_only_if")}</b>{t("feedback.do_not_put_secrets_passwords")}</span>
                <span id="fbk-count" className="fbs-count" data-over={problem === "body-long" ? "" : undefined}>
                  {t("feedback.n_max_bytes", { n: bytes.toLocaleString(), max: (limits?.bodyBytes ?? 0).toLocaleString() })}
                </span>
              </div>
            </>
          )}
        </div>

        <div className="fbs-field">
          <span className="sr-only" id="fbk-shots-t">{t("feedback.screenshots_optional_public_if_filed")}</span>
          <div className="fbs-shots" role="group" aria-labelledby="fbk-shots-t">
            {shots.length > 0 && (
              <ul className="fbk-shots">
                {shots.map((s) => (
                  <FeedbackShot key={s.id} shot={s} onRemove={() => setShots((prev) => prev.filter((x) => x.id !== s.id))} />
                ))}
              </ul>
            )}
            {host().capturesWindow() && (
              <button ref={captureButton} type="button" className="fbs-btn" data-action="feedback.shot.capture" disabled={room <= 0 || capture === "taking"} onClick={() => void takeWindow()}>
                <StudioIcon name="image" />{t("feedback.capture_this_window")}
              </button>
            )}
            <button type="button" className="fbs-btn" data-ghost="" data-action="feedback.shot.pick" disabled={room <= 0} onClick={() => picker.current?.click()}>
              <StudioIcon name="plus" />{t("feedback.add_screenshots")}
            </button>
            <input
              ref={picker}
              className="sr-only"
              type="file"
              accept="image/png,image/jpeg"
              multiple
              tabIndex={-1}
              aria-label={t("feedback.choose_screenshot_files")}
              data-action="feedback.shot.pick"
              onChange={(e) => {
                void add([...(e.target.files ?? [])]);
                e.target.value = "";
              }}
            />
          </div>
          <span className="fbs-hint">{limits ? t("feedback.you_can_also_paste_or", { n: limits.images, size: megabytes(limits.uploadBytes) }) : ""}</span>
          {capture === "failed" && <span className="fbs-hint" role="alert" data-tone="error">{t("feedback.could_not_capture_the_window")}</span>}
          <p className="fbs-warn">{t("feedback.screenshots_show_what_the_window_shows")}</p>
          {refused.length > 0 && (
            <ul className="fbk-refused" role="status">
              {refused.map((r, i) => (
                <li key={i}>{t(REFUSED_LABEL[r.why], { name: r.name, size: megabytes(limits?.uploadBytes ?? 0), n: limits?.images ?? 0 })}</li>
              ))}
            </ul>
          )}
        </div>

        <div className="fbs-row">
          <label className="fbs-field">
            <span className="fbs-label">{t("feedback.nickname_public_if_filed_as")}</span>
            <input
              className="fbs-input"
              data-action="feedback.name"
              value={name}
              autoComplete="nickname"
              aria-invalid={problem === "name-long" ? true : undefined}
              onChange={(e) => setName(e.target.value)}
            />
            <span className="fbs-hint">{t("feedback.if_it_is_filed_as", { name: name.trim() || "…" })}</span>
          </label>
          <label className="fbs-field">
            <span className="fbs-label">{t("feedback.contact_optional_private")}</span>
            <input
              className="fbs-input"
              data-action="feedback.contact"
              value={contact}
              autoComplete="off"
              aria-invalid={problem === "contact-long" ? true : undefined}
              onChange={(e) => setContact(e.target.value)}
            />
            <span className="fbs-hint">{t("feedback.email_or_qq_only_the")}</span>
          </label>
        </div>
      </fieldset>

      {env && (
        <section className="fbs-env" aria-labelledby="fbk-env-t">
          <details>
            <summary>
              {t("feedback.sent_along_version_and_system", { version: `Studio ${env.env.version}`, system: systemLabel(env.env.os, env.env.osVersion, env.env.arch) })}
            </summary>
            <h3 id="fbk-env-t" className="sr-only">{t("feedback.environment_sent_with_your_report")}</h3>
            <dl>
              <dt>{t("common.version")}</dt>
              <dd>{env.env.version}{env.env.commit ? ` (${env.env.commit})` : ""}</dd>
              <dt>{t("common.system")}</dt>
              <dd>{systemLabel(env.env.os, env.env.osVersion, env.env.arch)}</dd>
              <dt>{t("feedback.surface")}</dt>
              <dd>{env.env.surface}</dd>
              <dt>{t("common.language")}</dt>
              <dd>{env.env.locale}</dd>
              <dt>{t("feedback.channel")}</dt>
              <dd>{env.env.channel}</dd>
              <dt>{t("common.model_services")}</dt>
              <dd>{env.env.providerKind}</dd>
              <dt>{t("feedback.memory")}</dt>
              <dd>
                {memoryLines([...shellMemory, ...(env.env.memory ?? [])]).map((line) => <div key={line}>{line}</div>)}
              </dd>
            </dl>
            <p className="fbs-hint">{t("feedback.read_only_it_holds_no")}</p>
            {host().revealsLogs() && (
              <div className="fbs-logs">
                <button
                  type="button"
                  className="fbs-btn"
                  data-ghost=""
                  data-action="feedback.logs"
                  onClick={() => {
                    setLogsFailed(false);
                    void host().revealLogs().then((why) => setLogsFailed(why !== null));
                  }}
                >
                  {t("feedback.open_logs_folder")}
                </button>
                <p className="fbs-hint">{t("feedback.when_a_maintainer_asks_for")}</p>
                {logsFailed && <p className="fbs-hint" role="alert" data-tone="error">{t("feedback.the_logs_folder_could_not")}</p>}
              </div>
            )}
          </details>
        </section>
      )}

      {failure && (
        <div className="fbk-note" role="alert" data-tone="error" data-code={failure.code}>
          <StudioIcon name="warning" />
          <span>{failure.message}</span>
          {failure.code === FEEDBACK_CODE.duplicate && (
            <button type="button" className="btn sm" data-action="feedback.mine" onClick={onMine}>{t("feedback.view_my_feedback")}</button>
          )}
          {(failure.code === FEEDBACK_CODE.disabled || failure.code === FEEDBACK_CODE.unavailable) && (
            <button type="button" className="btn sm" data-action="feedback.link" onClick={() => onFile(FEEDBACK_REPO_ISSUES + "new/choose")}>
              {t("feedback.go_to_github")}
            </button>
          )}
        </div>
      )}

      <div className="fbs-foot">
        <span className="fbs-security">{tx("feedback.security_issues_go", { link: security })}</span>
        <span className="fbs-fill" />
        <button className="fbs-btn" type="button" data-ghost="" data-action="feedback.close" onClick={onClose}>{t("common.cancel")}</button>
        <button className="fbs-btn" type="submit" ref={submit} data-primary="" disabled={problem !== "" || sending}>
          <StudioIcon name="send" />
          {t(sending ? "common.sending" : failure?.retry === "same" ? "feedback.retry_sending" : "common.send")}
        </button>
      </div>
    </form>
  );
}
