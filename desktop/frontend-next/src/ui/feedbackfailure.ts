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
  "body.empty": "feedbackfailure.theTextIsEmptyPlease",
  "body.too_long": "feedbackfailure.theTextIsTooLong",
  "displayName.empty": "feedbackfailure.pleaseEnterANickname",
  "displayName.too_long": "feedbackfailure.theNicknameIsTooLong",
  "contact.too_long": "feedbackfailure.theContactIsTooLong",
  "category.bad_value": "feedbackfailure.pleasePickACategoryAgain",
  "images.too_many": "feedbackfailure.tooManyScreenshotsRemoveA",
  "images.format": "feedbackfailure.screenshotsMustBePNGOr",
  "images.too_large": "feedbackfailure.aScreenshotIsTooLarge",
  "images.undecodable": "feedbackfailure.aScreenshotCouldNotBe",
  "receipt.bad_value": "feedbackfailure.thatReportCouldNotBe",
};

const REPLY_INVALID: Record<string, string> = {
  "body.empty": "feedbackfailure.theReplyIsEmpty",
  "body.too_long": "feedbackfailure.theReplyIsTooLong",
};

type Params = Record<string, string | number | null> | undefined;

// What a window says it ran out of, by the identifier the service named. The
// words live here; the service's sentence is never read.
const WINDOW_SAID: Record<string, string> = {
  [FEEDBACK_LIMIT.installHourly]: "feedbackfailure.youHaveUsedThisHour",
  [FEEDBACK_LIMIT.installDaily]: "feedbackfailure.youHaveUsedTodayS",
  [FEEDBACK_LIMIT.ipHourly]: "feedbackfailure.thisNetworkHasUsedThis",
  [FEEDBACK_LIMIT.replyHourly]: "feedbackfailure.youHaveUsedThisHour2",
  [FEEDBACK_LIMIT.globalDaily]: "feedbackfailure.theFeedbackChannelIsFull",
  [FEEDBACK_LIMIT.globalBurst]: "feedbackfailure.theFeedbackChannelIsBusy",
};

const text = (v: string | number | null | undefined): string | null => (typeof v === "string" && v !== "" ? v : null);

function when(params: Params): string {
  const at = text(params?.resetsAt);
  const time = at === null ? NaN : Date.parse(at);
  if (!Number.isNaN(time) && time > Date.now()) {
    const loc = current() === "zh" ? "zh-CN" : "en";
    return t("feedbackfailure.itResetsAt", { time: new Date(time).toLocaleString(loc, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) });
  }
  const secs = Number(params?.retryAfterSeconds);
  if (!(secs > 0)) return "";
  if (secs < 60) return t("feedbackfailure.itResetsInAboutSec", { n: Math.ceil(secs) });
  if (secs < 3600) return t("feedbackfailure.itResetsInAboutMin", { n: Math.ceil(secs / 60) });
  return t("feedbackfailure.itResetsInAboutH", { n: Math.ceil(secs / 3600) });
}

// A refusal that names its window; null from a service that sends none, which
// keeps the plain wording of each code.
function windowed(params: Params): string | null {
  const limit = text(params?.limit);
  if (limit === null) return null;
  const said = WINDOW_SAID[limit];
  const head = said ? t(said) : t("feedbackfailure.aSubmissionLimitWasReached");
  const tail = when(params);
  return tail === "" ? head : `${head}${current() === "zh" ? "" : " "}${tail}`;
}

function invalid(params: Params): string {
  const said = INVALID[`${params?.field}.${params?.reason}`];
  return t(said ?? "feedbackfailure.somethingInTheFormWas");
}

export function feedbackFailure(e: unknown): FeedbackFailure {
  if (!(e instanceof HttpError)) {
    return { code: FEEDBACK_CODE.offline, retry: "same", message: t("feedbackfailure.couldNotReachTheFeedback") };
  }
  const code = e.reason?.code ?? "";
  const params = e.reason?.params;
  switch (code) {
    case FEEDBACK_CODE.invalid:
      return { code, retry: "edit", message: invalid(params) };
    case FEEDBACK_CODE.badBody:
      return { code, retry: "edit", message: t("feedbackfailure.theRequestCouldNotBe") };
    case FEEDBACK_CODE.tooLarge:
      return { code, retry: "edit", message: t("feedbackfailure.theReportWasTooLarge") };
    case FEEDBACK_CODE.rateLimited:
      return { code, retry: "later", message: windowed(params) ?? plainWait(params) };
    case FEEDBACK_CODE.busy:
      return { code, retry: "later", message: windowed(params) ?? t("feedbackfailure.theFeedbackChannelHasReached") };
    case FEEDBACK_CODE.challengeRequired:
      return { code, retry: "later", message: t("feedbackfailure.theServiceTemporarilyAsksFor") };
    case FEEDBACK_CODE.imageMetadata:
      return { code, retry: "edit", message: t("feedbackfailure.aScreenshotCouldNotBe2") };
    case FEEDBACK_CODE.duplicate:
      return { code, retry: "none", message: t("feedbackfailure.theSameTextWasJust") };
    case FEEDBACK_CODE.disabled:
      return { code, retry: "none", message: t("feedbackfailure.theFeedbackChannelIsSwitched") };
    case FEEDBACK_CODE.badToken:
      return { code, retry: "later", message: t("feedbackfailure.theFeedbackServiceDidNot") };
    case FEEDBACK_CODE.offline:
      return { code, retry: "same", message: t("feedbackfailure.couldNotReachTheFeedback") };
    case FEEDBACK_CODE.unavailable:
      return { code, retry: "later", message: t("feedbackfailure.theFeedbackServiceIsHaving") };
    case FEEDBACK_CODE.internal:
      return { code, retry: "later", message: t("feedbackfailure.savingTheFeedbackRecordOn") };
    default:
      return { code, retry: "later", message: t("feedbackfailure.theFeedbackWasNotSent") };
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
      return { code, retry: "edit", message: t(said ?? "feedbackfailure.theReplyWasNotAccepted") };
    }
    case FEEDBACK_CODE.badBody:
      return { code, retry: "edit", message: t("feedbackfailure.theRequestCouldNotBe") };
    case FEEDBACK_CODE.replyLimit:
      if (text(params?.limit) === FEEDBACK_LIMIT.replyItem) {
        return { code, retry: "none", message: t("feedbackfailure.thisReportHasReachedIts2") };
      }
      return { code, retry: "later", message: t("feedbackfailure.thisReportHasReachedIts") };
    case FEEDBACK_CODE.notReplyable:
      return { code, retry: "none", message: t("feedbackfailure.thisReportTakesNoReply") };
    case FEEDBACK_CODE.rateLimited:
      return { code, retry: "later", message: windowed(params) ?? plainWait(params) };
    case FEEDBACK_CODE.badToken:
      return { code, retry: "none", message: t("feedbackfailure.thisComputerSFeedbackIdentity") };
    case FEEDBACK_CODE.disabled:
      return { code, retry: "none", message: t("feedbackfailure.theFeedbackChannelIsSwitched2") };
    case FEEDBACK_CODE.offline:
      return replyOffline(code);
    case FEEDBACK_CODE.internal:
      return { code, retry: "later", message: t("feedbackfailure.savingTheFeedbackRecordOn") };
    case FEEDBACK_CODE.unavailable:
      return { code, retry: "later", message: t("feedbackfailure.theFeedbackServiceIsHaving2") };
    default:
      return { code, retry: "later", message: t("feedbackfailure.theReplyWasNotSent") };
  }
}

function plainWait(params: Params): string {
  const secs = Number(params?.retryAfterSeconds);
  return secs > 0 ? t("feedbackfailure.youAreSendingTooFast", { secs }) : t("feedbackfailure.youAreSendingTooFast2");
}

function replyOffline(code: string): FeedbackFailure {
  return { code, retry: "same", message: t("feedbackfailure.couldNotReachTheFeedback2") };
}

// Linking an install to the account has its own refusals; the rest read like
// any other feedback refusal.
export function linkFailure(e: unknown): FeedbackFailure {
  if (!(e instanceof HttpError)) return { code: FEEDBACK_CODE.offline, retry: "same", message: t("feedbackfailure.couldNotReachTheFeedback3") };
  const code = e.reason?.code ?? "";
  switch (code) {
    case FEEDBACK_CODE.accountRequired:
      return { code, retry: "none", message: t("feedbackfailure.signInToYourAccount") };
    case FEEDBACK_CODE.linkConflict:
      return { code, retry: "none", message: t("feedbackfailure.thisComputerIsAlreadyLinked") };
    case FEEDBACK_CODE.linkLimit:
      return { code, retry: "none", message: t("feedbackfailure.thisAccountHasReachedIts") };
    case FEEDBACK_CODE.noInstall:
      return { code, retry: "none", message: t("feedbackfailure.thisComputerHasNotSent") };
    case FEEDBACK_CODE.rateLimited:
      return { code, retry: "later", message: windowed(e.reason?.params) ?? plainWait(e.reason?.params) };
    case FEEDBACK_CODE.disabled:
    case FEEDBACK_CODE.unavailable:
      return { code, retry: "later", message: t("feedbackfailure.accountLinkingIsUnavailableRight") };
    case FEEDBACK_CODE.offline:
      return { code, retry: "same", message: t("feedbackfailure.couldNotReachTheFeedback3") };
    default:
      return { code, retry: "later", message: t("feedbackfailure.thatDidNotFinishPlease") };
  }
}
