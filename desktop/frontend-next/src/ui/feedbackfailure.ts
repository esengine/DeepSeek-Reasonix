import { HttpError } from "../port/http_error";
import { FEEDBACK_CODE, FEEDBACK_LIMIT } from "../port/feedback";
import { current, t } from "../i18n";

// What a person can do about a refusal; the button and the copy follow it.
export type FeedbackRetry = "same" | "edit" | "later" | "none";

export interface FeedbackFailure {
  code: string;
  retry: FeedbackRetry;
  message: string;
}

const INVALID: Record<string, string> = {
  "body.empty": "正文不能为空，请写下你遇到的情况。",
  "body.too_long": "正文太长了，请缩短后再发。",
  "displayName.empty": "请填写昵称。",
  "displayName.too_long": "昵称太长了，请缩短后再发。",
  "contact.too_long": "联系方式太长了，请缩短后再发。",
  "category.bad_value": "请重新选择反馈类别。",
  "images.too_many": "截图数量超出上限，请去掉几张后再发。",
  "images.format": "截图只支持 PNG 或 JPEG。",
  "images.too_large": "有截图太大了，请换小一点的图。",
  "images.undecodable": "有截图无法读取，请换一张图。",
  "receipt.bad_value": "找不到这条反馈，请刷新列表后再试。",
};

const REPLY_INVALID: Record<string, string> = {
  "body.empty": "回复不能为空。",
  "body.too_long": "回复太长了，请缩短后再发。",
};

type Params = Record<string, string | number | null> | undefined;

// What a window says it ran out of, by the identifier the service named. The
// words live here; the service's sentence is never read.
const WINDOW_SAID: Record<string, string> = {
  [FEEDBACK_LIMIT.installHourly]: "这一小时的反馈次数已用完。",
  [FEEDBACK_LIMIT.installDaily]: "今天的反馈次数已用完。",
  [FEEDBACK_LIMIT.ipHourly]: "这个网络这一小时的提交次数已用完。",
  [FEEDBACK_LIMIT.replyHourly]: "这一小时的回复次数已用完。",
  [FEEDBACK_LIMIT.globalDaily]: "反馈通道今天的接收量已满，不是你发得太多。内容都还在。",
  [FEEDBACK_LIMIT.globalBurst]: "反馈通道此刻很忙，不是你发得太多。内容都还在。",
};

const text = (v: string | number | null | undefined): string | null => (typeof v === "string" && v !== "" ? v : null);

function when(params: Params): string {
  const at = text(params?.resetsAt);
  const time = at === null ? NaN : Date.parse(at);
  if (!Number.isNaN(time) && time > Date.now()) {
    const loc = current() === "zh" ? "zh-CN" : "en";
    return t("feedback.it_resets_at_time", { time: new Date(time).toLocaleString(loc, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) });
  }
  const secs = Number(params?.retryAfterSeconds);
  if (!(secs > 0)) return "";
  if (secs < 60) return t("feedback.it_resets_in_about_n_3", { n: Math.ceil(secs) });
  if (secs < 3600) return t("feedback.it_resets_in_about_n", { n: Math.ceil(secs / 60) });
  return t("feedback.it_resets_in_about_n_2", { n: Math.ceil(secs / 3600) });
}

// A refusal that names its window; null from a service that sends none, which
// keeps the plain wording of each code.
function windowed(params: Params): string | null {
  const limit = text(params?.limit);
  if (limit === null) return null;
  const said = WINDOW_SAID[limit];
  const head = said ? t(said) : t("feedback.a_submission_limit_was_reached");
  const tail = when(params);
  return tail === "" ? head : `${head}${current() === "zh" ? "" : " "}${tail}`;
}

function invalid(params: Params): string {
  const said = INVALID[`${params?.field}.${params?.reason}`];
  return t(said ?? "feedback.something_in_the_form_was");
}

export function feedbackFailure(e: unknown): FeedbackFailure {
  if (!(e instanceof HttpError)) {
    return { code: FEEDBACK_CODE.offline, retry: "same", message: t("feedback.could_not_reach_the_feedback_3") };
  }
  const code = e.reason?.code ?? "";
  const params = e.reason?.params;
  switch (code) {
    case FEEDBACK_CODE.invalid:
      return { code, retry: "edit", message: invalid(params) };
    case FEEDBACK_CODE.badBody:
      return { code, retry: "edit", message: t("feedback.the_request_could_not_be") };
    case FEEDBACK_CODE.tooLarge:
      return { code, retry: "edit", message: t("feedback.the_report_was_too_large") };
    case FEEDBACK_CODE.rateLimited:
      return { code, retry: "later", message: windowed(params) ?? plainWait(params) };
    case FEEDBACK_CODE.busy:
      return { code, retry: "later", message: windowed(params) ?? t("feedback.the_feedback_channel_has_reached") };
    case FEEDBACK_CODE.challengeRequired:
      return { code, retry: "later", message: t("feedback.the_service_temporarily_asks_for") };
    case FEEDBACK_CODE.imageMetadata:
      return { code, retry: "edit", message: t("feedback.a_screenshot_could_not_be") };
    case FEEDBACK_CODE.duplicate:
      return { code, retry: "none", message: t("feedback.the_same_text_was_just") };
    case FEEDBACK_CODE.disabled:
      return { code, retry: "none", message: t("feedback.the_feedback_channel_is_switched_2") };
    case FEEDBACK_CODE.badToken:
      return { code, retry: "later", message: t("feedback.the_feedback_service_did_not") };
    case FEEDBACK_CODE.offline:
      return { code, retry: "same", message: t("feedback.could_not_reach_the_feedback_3") };
    case FEEDBACK_CODE.unavailable:
      return { code, retry: "later", message: t("feedback.the_feedback_service_is_having_2") };
    case FEEDBACK_CODE.internal:
      return { code, retry: "later", message: t("feedback.saving_the_feedback_record_on") };
    default:
      return { code, retry: "later", message: t("feedback.the_feedback_was_not_sent") };
  }
}

// A reply has no idempotency key: after an unanswered request the kernel does
// not retry, so the message says to look before sending again.
export function replyFailure(e: unknown): FeedbackFailure {
  if (!(e instanceof HttpError)) return replyOffline(FEEDBACK_CODE.offline);
  const code = e.reason?.code ?? "";
  const params = e.reason?.params;
  switch (code) {
    case FEEDBACK_CODE.invalid: {
      const said = REPLY_INVALID[`${params?.field}.${params?.reason}`] ?? INVALID[`${params?.field}.${params?.reason}`];
      return { code, retry: "edit", message: t(said ?? "feedback.the_reply_was_not_accepted") };
    }
    case FEEDBACK_CODE.badBody:
      return { code, retry: "edit", message: t("feedback.the_request_could_not_be") };
    case FEEDBACK_CODE.replyLimit:
      if (text(params?.limit) === FEEDBACK_LIMIT.replyItem) {
        return { code, retry: "none", message: t("feedback.this_report_has_reached_its") };
      }
      return { code, retry: "later", message: t("feedback.this_report_has_reached_its_2") };
    case FEEDBACK_CODE.notReplyable:
      return { code, retry: "none", message: t("feedback.this_report_takes_no_reply") };
    case FEEDBACK_CODE.rateLimited:
      return { code, retry: "later", message: windowed(params) ?? plainWait(params) };
    case FEEDBACK_CODE.badToken:
      return { code, retry: "none", message: t("feedback.this_computers_feedback_identity_has") };
    case FEEDBACK_CODE.disabled:
      return { code, retry: "none", message: t("feedback.the_feedback_channel_is_switched") };
    case FEEDBACK_CODE.offline:
      return replyOffline(code);
    case FEEDBACK_CODE.internal:
      return { code, retry: "later", message: t("feedback.saving_the_feedback_record_on") };
    case FEEDBACK_CODE.unavailable:
      return { code, retry: "later", message: t("feedback.the_feedback_service_is_having") };
    default:
      return { code, retry: "later", message: t("feedback.the_reply_was_not_sent") };
  }
}

function plainWait(params: Params): string {
  const secs = Number(params?.retryAfterSeconds);
  return secs > 0 ? t("feedback.you_are_sending_too_fast_2", { secs }) : t("feedback.you_are_sending_too_fast");
}

function replyOffline(code: string): FeedbackFailure {
  return { code, retry: "same", message: t("feedback.could_not_reach_the_feedback_2") };
}

// Linking an install to the account has its own refusals; the rest read like
// any other feedback refusal.
export function linkFailure(e: unknown): FeedbackFailure {
  if (!(e instanceof HttpError)) return { code: FEEDBACK_CODE.offline, retry: "same", message: t("feedback.could_not_reach_the_feedback") };
  const code = e.reason?.code ?? "";
  switch (code) {
    case FEEDBACK_CODE.accountRequired:
      return { code, retry: "none", message: t("feedback.sign_in_to_your_account_2") };
    case FEEDBACK_CODE.linkConflict:
      return { code, retry: "none", message: t("feedback.this_computer_is_already_linked") };
    case FEEDBACK_CODE.linkLimit:
      return { code, retry: "none", message: t("feedback.this_account_has_reached_its") };
    case FEEDBACK_CODE.noInstall:
      return { code, retry: "none", message: t("feedback.this_computer_has_not_sent") };
    case FEEDBACK_CODE.rateLimited:
      return { code, retry: "later", message: windowed(e.reason?.params) ?? plainWait(e.reason?.params) };
    case FEEDBACK_CODE.disabled:
    case FEEDBACK_CODE.unavailable:
      return { code, retry: "later", message: t("feedback.account_linking_is_unavailable_right") };
    case FEEDBACK_CODE.offline:
      return { code, retry: "same", message: t("feedback.could_not_reach_the_feedback") };
    default:
      return { code, retry: "later", message: t("feedback.that_did_not_finish_please") };
  }
}
