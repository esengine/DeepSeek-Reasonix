import { t } from "../i18n";

export type StartupFailureState = {
  details: string[];
};

function toErrorText(error: unknown): string {
  if (error instanceof Error) return error.message;
  return String(error);
}

export function coerceStartupFailure(
  error: unknown,
  stderrLines: readonly string[] = [],
): StartupFailureState {
  const details = [toErrorText(error), ...stderrLines].map((line) => line.trim()).filter(Boolean);
  return { details: details.length > 0 ? details : [t("app.startupFailedUnknown")] };
}

export function StartupFailure({
  details,
  onRetry,
}: {
  details: readonly string[];
  onRetry: () => void;
}) {
  return (
    <main className="startup-failure" role="alert" aria-live="assertive">
      <section className="startup-failure-panel">
        <p className="startup-failure-kicker">{t("app.errorLabel")}</p>
        <h1>{t("app.startupFailedTitle")}</h1>
        <p>{t("app.startupFailedMessage")}</p>
        <pre>{details.join("\n")}</pre>
        <button type="button" onClick={onRetry}>
          {t("app.startupFailedRetry")}
        </button>
      </section>
    </main>
  );
}
