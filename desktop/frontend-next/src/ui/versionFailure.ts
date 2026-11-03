import { t } from "../i18n";
import type { UpdateProgress } from "../port/port";

// What a failed move means and what to do about it, keyed by the code the step
// that failed attached. The kernel's own sentence rides along as detail: it is
// what a bug report needs, and not what a person reads first.
export type FailureCopy = { title: string; why: string; manual: boolean };

export function failureCopy(p: UpdateProgress, current: string): FailureCopy {
  const v = p.version;
  switch (p.code) {
    case "update.catalog_unreachable":
      return { title: t("versionFailure.couldNotReadTheRelease", { v }), why: t("versionFailure.theVersionIndexCannotBe"), manual: false };
    case "update.download_failed":
      return { title: t("versionFailure.downloadingWasInterrupted", { v }), why: t("versionFailure.theConnectionDroppedOrTimed"), manual: false };
    case "update.download_stalled":
      return { title: t("versionFailure.downloadingGotStuck", { v }), why: t("versionFailure.theDownloadConnectionDeliveredNothin"), manual: true };
    case "update.signature_invalid":
      return { title: t("versionFailure.failedItsSignatureCheck", { v }), why: t("versionFailure.theDownloadedFileDoesNot"), manual: false };
    case "update.disk":
      return { title: t("versionFailure.theUpdateCouldNotBe"), why: t("versionFailure.theDiskIsFullOr"), manual: false };
    case "update.no_package":
      return { title: t("versionFailure.hasNoPackageForThis", { v }), why: t("versionFailure.thisVersionPublishesNoAutomatically"), manual: true };
    case "update.installer_failed":
      return { title: t("versionFailure.installingDidNotStart", { v }), why: t("versionFailure.theSystemOrSecuritySoftware"), manual: true };
    case "update.not_applied":
      return {
        title: t("versionFailure.theLastUpdateToDid", { v }),
        why: t("versionFailure.afterTheRestartIsStill", { current }),
        manual: true,
      };
  }
  return { title: t("versionFailure.switchingToFailed", { v }), why: t("versionFailure.theRunningVersionWasNot"), manual: false };
}

// Why this move downloads the whole package instead of only what changed,
// keyed by the code the kernel attached when it gave the delta up.
export function deltaSkippedCopy(code: string): string {
  switch (code) {
    case "update.delta.not_swappable":
      return t("versionFailure.theInstallFolderCannotHave");
    case "update.delta.fetch_failed":
      return t("versionFailure.theChangedPartsCouldNot");
    case "update.delta.timed_out":
      return t("versionFailure.theChangedPartsWereDownloading");
    case "update.delta.mismatch":
      return t("versionFailure.theChangedPartsFailedVerification");
    case "update.delta.too_large":
      return t("versionFailure.thisJumpSpansManyVersions");
    case "update.delta.disk":
      return t("versionFailure.theChangedPartsCouldNot2");
  }
  return t("versionFailure.updatingOnlyTheChangedParts");
}

// The release page carries every platform's full installer.
export function releasePage(v: string): string {
  return `https://github.com/esengine/DeepSeek-Reasonix/releases/tag/studio-v${v.replace(/^v/, "")}`;
}
