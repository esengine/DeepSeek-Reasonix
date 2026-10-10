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
    return t("将在 {time} 重置。", { time: new Date(time).toLocaleString(loc, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) });
  }
  const secs = Number(params?.retryAfterSeconds);
  if (!(secs > 0)) return "";
  if (secs < 60) return t("约 {n} 秒后重置。", { n: Math.ceil(secs) });
  if (secs < 3600) return t("约 {n} 分钟后重置。", { n: Math.ceil(secs / 60) });
  return t("约 {n} 小时后重置。", { n: Math.ceil(secs / 3600) });
}

// A refusal that names its window; null from a service that sends none, which
// keeps the plain wording of each code.
function windowed(params: Params): string | null {
  const limit = text(params?.limit);
  if (limit === null) return null;
  const said = WINDOW_SAID[limit];
  const head = said ? t(said) : t("已达到一项提交上限。");
  const tail = when(params);
  return tail === "" ? head : `${head}${current() === "zh" ? "" : " "}${tail}`;
}

function invalid(params: Params): string {
  const said = INVALID[`${params?.field}.${params?.reason}`];
  return t(said ?? "内容不符合要求，请检查后重试。");
}

export function feedbackFailure(e: unknown): FeedbackFailure {
  if (!(e instanceof HttpError)) {
    return { code: FEEDBACK_CODE.offline, retry: "same", message: t("没能连上反馈服务。已填的内容都还在，网络恢复后可以直接重试，不会重复提交。") };
  }
  const code = e.reason?.code ?? "";
  const params = e.reason?.params;
  switch (code) {
    case FEEDBACK_CODE.invalid:
      return { code, retry: "edit", message: invalid(params) };
    case FEEDBACK_CODE.badBody:
      return { code, retry: "edit", message: t("这次的内容没能被解析，请检查后重试。") };
    case FEEDBACK_CODE.tooLarge:
      return { code, retry: "edit", message: t("内容太大，服务端没有接收。请缩短文字，或去掉一张截图后再发。") };
    case FEEDBACK_CODE.rateLimited:
      return { code, retry: "later", message: windowed(params) ?? plainWait(params) };
    case FEEDBACK_CODE.busy:
      return { code, retry: "later", message: windowed(params) ?? t("反馈通道今天的接收量已满，不是你发得太多。内容都还在，请明天再试。") };
    case FEEDBACK_CODE.challengeRequired:
      return { code, retry: "later", message: t("服务暂时要求额外验证，当前版本还不能显示验证步骤。请稍后再试，或直接到 GitHub 提交问题。") };
    case FEEDBACK_CODE.imageMetadata:
      return { code, retry: "edit", message: t("有一张截图没能清除其中的隐藏信息。请换一张，或重新导出后再添加。") };
    case FEEDBACK_CODE.duplicate:
      return { code, retry: "none", message: t("相同内容刚刚已经提交过，不需要再发一次。可以在「我的反馈」里查看它。") };
    case FEEDBACK_CODE.disabled:
      return { code, retry: "none", message: t("反馈通道暂时关闭。你可以直接到 GitHub 提交问题。") };
    case FEEDBACK_CODE.badToken:
      return { code, retry: "later", message: t("反馈服务没有认出这台电脑，请稍后重试。") };
    case FEEDBACK_CODE.offline:
      return { code, retry: "same", message: t("没能连上反馈服务。已填的内容都还在，网络恢复后可以直接重试，不会重复提交。") };
    case FEEDBACK_CODE.unavailable:
      return { code, retry: "later", message: t("反馈服务暂时出了问题。已填的内容都还在，请稍等片刻再试，也可以直接到 GitHub 提交问题。") };
    case FEEDBACK_CODE.internal:
      return { code, retry: "later", message: t("本机保存反馈记录时出错，请重试。") };
    default:
      return { code, retry: "later", message: t("反馈没有发出去，请稍后重试。") };
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
      return { code, retry: "edit", message: t(said ?? "回复不符合要求，请检查后重试。") };
    }
    case FEEDBACK_CODE.badBody:
      return { code, retry: "edit", message: t("这次的内容没能被解析，请检查后重试。") };
    case FEEDBACK_CODE.replyLimit:
      if (text(params?.limit) === FEEDBACK_LIMIT.replyItem) {
        return { code, retry: "none", message: t("这份反馈的回复次数已到上限，不会自动重置。如有新的情况，可以另外提交一条反馈。") };
      }
      return { code, retry: "later", message: t("这份反馈的回复次数已到上限，或你回复得太频繁了。请稍后再试，必要时另外提交一条新反馈。") };
    case FEEDBACK_CODE.notReplyable:
      return { code, retry: "none", message: t("这份反馈现在不接收回复，它可能已经处理完毕或被关闭。请刷新列表查看最新状态。") };
    case FEEDBACK_CODE.rateLimited:
      return { code, retry: "later", message: windowed(params) ?? plainWait(params) };
    case FEEDBACK_CODE.badToken:
      return { code, retry: "none", message: t("这台电脑的反馈身份已经变了，这份反馈不能再从这里回复。可以另外提交一条新反馈。") };
    case FEEDBACK_CODE.disabled:
      return { code, retry: "none", message: t("反馈通道暂时关闭，暂时不能回复。") };
    case FEEDBACK_CODE.offline:
      return replyOffline(code);
    case FEEDBACK_CODE.internal:
      return { code, retry: "later", message: t("本机保存反馈记录时出错，请重试。") };
    case FEEDBACK_CODE.unavailable:
      return { code, retry: "later", message: t("反馈服务暂时出了问题。你写的回复还在，请稍等片刻；重发前先刷新列表确认是否送达，也可以直接到 GitHub 提交问题。") };
    default:
      return { code, retry: "later", message: t("回复没有发出去，请稍后重试。") };
  }
}

function plainWait(params: Params): string {
  const secs = Number(params?.retryAfterSeconds);
  return secs > 0 ? t("提交得太频繁了，请等 {secs} 秒后再试。", { secs }) : t("提交得太频繁了，请稍等一会儿再试。");
}

function replyOffline(code: string): FeedbackFailure {
  return { code, retry: "same", message: t("没能连上反馈服务。你写的回复还在；如果不确定它有没有送达，请先刷新列表看看，再决定要不要重发。") };
}
