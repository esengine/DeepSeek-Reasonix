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
      return { title: t("about.could_not_read_the_release", { v }), why: t("about.the_version_index_cannot_be"), manual: false };
    case "update.download_failed":
      return { title: t("about.downloading_v_was_interrupted", { v }), why: t("about.the_connection_dropped_or_timed"), manual: false };
    case "update.download_stalled":
      return { title: t("about.downloading_v_got_stuck", { v }), why: t("about.the_download_connection_delivered_nothing"), manual: true };
    case "update.signature_invalid":
      return { title: t("about.v_failed_its_signature_check", { v }), why: t("about.the_downloaded_file_does_not"), manual: false };
    case "update.disk":
      return { title: t("about.the_update_could_not_be"), why: t("about.the_disk_is_full_or"), manual: false };
    case "update.no_package":
      return { title: t("about.v_has_no_package_for", { v }), why: t("about.this_version_publishes_no_automatically"), manual: true };
    case "update.installer_failed":
      return { title: t("about.installing_v_did_not_start", { v }), why: t("about.the_system_or_security_software"), manual: true };
    case "update.not_applied":
      return {
        title: t("about.the_last_update_to_v", { v }),
        why: t("about.after_the_restart_current_is", { current }),
        manual: true,
      };
  }
  return { title: t("about.switching_to_v_failed", { v }), why: t("about.the_running_version_was_not"), manual: false };
}

// Why this move downloads the whole package instead of only what changed,
// keyed by the code the kernel attached when it gave the delta up.
export function deltaSkippedCopy(code: string): string {
  switch (code) {
    case "update.delta.not_swappable":
      return t("about.the_install_folder_cannot_have");
    case "update.delta.fetch_failed":
      return t("about.the_changed_parts_could_not");
    case "update.delta.timed_out":
      return t("about.the_changed_parts_were_downloading");
    case "update.delta.mismatch":
      return t("about.the_changed_parts_failed_verification");
    case "update.delta.too_large":
      return t("about.this_jump_spans_many_versions");
    case "update.delta.disk":
      return t("about.the_changed_parts_could_not_2");
  }
  return t("about.updating_only_the_changed_parts");
}

// The release page carries every platform's full installer.
export function releasePage(v: string): string {
  return `https://github.com/esengine/DeepSeek-Reasonix/releases/tag/studio-v${v.replace(/^v/, "")}`;
}
