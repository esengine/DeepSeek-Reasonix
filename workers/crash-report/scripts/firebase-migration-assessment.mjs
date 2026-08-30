const STATUS_KEYS = ["open", "resolved", "ignored", "other"];
const AGE_KEYS = ["under30d", "days30to59d", "days60plus", "invalid"];

function emptyCounts(keys) {
  return Object.fromEntries(keys.map((key) => [key, 0]));
}

function statusKey(value) {
  return value === "open" || value === "resolved" || value === "ignored" ? value : "other";
}

function ageKey(value, now) {
  const age = now.getTime() - new Date(value == null ? "" : String(value)).getTime();
  if (!Number.isFinite(age)) return "invalid";
  if (age < 30 * 86400_000) return "under30d";
  return age < 60 * 86400_000 ? "days30to59d" : "days60plus";
}

export function createMigrationCapacityAssessment() {
  return {
    statusTotals: emptyCounts(STATUS_KEYS),
    ageByStatus: Object.fromEntries(STATUS_KEYS.map((status) => [status, emptyCounts(AGE_KEYS)])),
  };
}

export function accumulateMigrationCapacityAssessment(summary, rows, now = new Date()) {
  for (const row of rows) {
    const status = statusKey(row.status);
    const age = ageKey(row.last_seen, now);
    summary.statusTotals[status]++;
    summary.ageByStatus[status][age]++;
  }
  return summary;
}

export function finalizeMigrationCapacityAssessment(summary) {
  const { open, resolved, ignored, other } = summary.ageByStatus;
  return {
    ...summary,
    activeReasons: {
      open: summary.statusTotals.open,
      otherStatus: summary.statusTotals.other,
      recentResolvedOrIgnored: resolved.under30d + ignored.under30d,
      invalidResolvedOrIgnored: resolved.invalid + ignored.invalid,
    },
    automaticRetention: {
      compacted: resolved.days30to59d + ignored.days30to59d,
      archived: resolved.days60plus + ignored.days60plus,
    },
    manualReview: {
      open30to59d: open.days30to59d,
      open60dPlus: open.days60plus,
      otherStatus30dPlus: other.days30to59d + other.days60plus,
    },
  };
}

export function assessMigrationCapacity(rows, now = new Date()) {
  const summary = accumulateMigrationCapacityAssessment(createMigrationCapacityAssessment(), rows, now);
  return finalizeMigrationCapacityAssessment(summary);
}
