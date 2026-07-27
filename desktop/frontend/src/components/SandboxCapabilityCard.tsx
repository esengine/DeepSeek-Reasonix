import { useT } from "../lib/i18n";
import type {
  WireSandboxCapabilityPrompt,
  WireCapabilityPath,
  WireCapabilityDevice,
  WireCapabilityRiskFinding,
} from "../lib/types";

interface Props {
  sandboxCapability: WireSandboxCapabilityPrompt;
}

export function SandboxCapabilityCard({ sandboxCapability: sc }: Props) {
  const t = useT();
  const review = sc.review;
  const delta = review.effective_delta;
  const hasReadPaths = delta.read_paths && delta.read_paths.length > 0;
  const hasWritePaths = delta.write_paths && delta.write_paths.length > 0;
  const hasDevices = delta.devices && delta.devices.length > 0;
  const hasRiskFindings =
    review.risk.findings && review.risk.findings.length > 0;
  const isCriticalRisk = review.risk.level === "critical";
  const hasWarnings = sc.warnings && sc.warnings.length > 0;
  const hasGrantPrefix = sc.grant_prefix && sc.grant_prefix.length > 0;
  const showBackgroundWarning = sc.preserve_background_processes;
  const findingMessage = (finding: WireCapabilityRiskFinding): string => {
    const key = `approval.sandboxCapabilityFinding.${finding.code}` as const;
    const localized = t(key as any);
    return localized === key ? finding.message : localized;
  };

  return (
    <div className="sandbox-capability-card">
      {/* Danger banners */}
      {showBackgroundWarning && (
        <div className="sandbox-capability-card__danger-banner">
          <span className="sandbox-capability-card__danger-banner-icon">⚠</span>
          <span>{t("approval.sandboxCapabilityPreserveWarning")}</span>
        </div>
      )}
      {isCriticalRisk && (
        <div className="sandbox-capability-card__danger-banner sandbox-capability-card__danger-banner--critical">
          <span className="sandbox-capability-card__danger-banner-icon">⚠</span>
          <span>{t("approval.sandboxCapabilityCriticalRiskWarning")}</span>
        </div>
      )}

      {/* Justification */}
      {review.justification && (
        <div className="sandbox-capability-card__section">
          <div className="sandbox-capability-card__label">
            {t("approval.sandboxCapabilityJustification")}
          </div>
          <p className="sandbox-capability-card__justification">
            {review.justification}
          </p>
        </div>
      )}

      {/* Command */}
      {sc.canonical_executable && (
        <div className="sandbox-capability-card__section">
          <div className="sandbox-capability-card__label">
            {t("approval.sandboxCapabilityCommand")}
          </div>
          <code className="sandbox-capability-card__command">
            {sc.canonical_executable}
            {sc.argv && sc.argv.length > 0 && (
              <span className="sandbox-capability-card__args">
                {" "}
                {sc.argv.join(" ")}
              </span>
            )}
          </code>
        </div>
      )}

      {/* Grant prefix */}
      {hasGrantPrefix && (
        <div className="sandbox-capability-card__section">
          <div className="sandbox-capability-card__label">
            {t("approval.sandboxCapabilityGrantPrefix")}
          </div>
          <code className="sandbox-capability-card__grant-prefix">
            {sc.grant_prefix!.join(" ")}
          </code>
        </div>
      )}

      {/* Network */}
      <div className="sandbox-capability-card__section">
        <div className="sandbox-capability-card__label">
          {t("approval.sandboxCapabilityNetwork")}
        </div>
        <span
          className={`sandbox-capability-card__toggle ${delta.network ? "sandbox-capability-card__toggle--on" : ""}`}
        >
          {delta.network ? "ON" : "OFF"}
        </span>
      </div>

      {/* Background */}
      <div className="sandbox-capability-card__section">
        <div className="sandbox-capability-card__label">
          {t("approval.sandboxCapabilityBackground")}
        </div>
        <span
          className={`sandbox-capability-card__toggle ${sc.background ? "sandbox-capability-card__toggle--on" : ""}`}
        >
          {sc.background ? "ON" : "OFF"}
        </span>
      </div>

      {/* Preserve background processes */}
      {sc.preserve_background_processes && (
        <div className="sandbox-capability-card__section">
          <div className="sandbox-capability-card__label">
            {t("approval.sandboxCapabilityPreserveProcesses")}
          </div>
          <span className="sandbox-capability-card__toggle sandbox-capability-card__toggle--on">
            ON
          </span>
        </div>
      )}

      {/* Read paths */}
      {hasReadPaths && (
        <div className="sandbox-capability-card__section">
          <div className="sandbox-capability-card__label">
            {t("approval.sandboxCapabilityReadPaths")}
          </div>
          <ul className="sandbox-capability-card__path-list">
            {delta.read_paths!.map((p: WireCapabilityPath, i: number) => (
              <li key={i} className="sandbox-capability-card__path-item">
                <span className="sandbox-capability-card__path-identity">
                  {p.identity}
                </span>
                <span className="sandbox-capability-card__path-canonical">
                  {p.canonical}
                </span>
                <span className="sandbox-capability-card__path-kind">
                  {p.kind}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Write paths */}
      {hasWritePaths && (
        <div className="sandbox-capability-card__section">
          <div className="sandbox-capability-card__label">
            {t("approval.sandboxCapabilityWritePaths")}
          </div>
          <ul className="sandbox-capability-card__path-list">
            {delta.write_paths!.map((p: WireCapabilityPath, i: number) => (
              <li key={i} className="sandbox-capability-card__path-item">
                <span className="sandbox-capability-card__path-identity">
                  {p.identity}
                </span>
                <span className="sandbox-capability-card__path-canonical">
                  {p.canonical}
                </span>
                <span className="sandbox-capability-card__path-kind">
                  {p.kind}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* Devices */}
      {hasDevices && (
        <div className="sandbox-capability-card__section">
          <div className="sandbox-capability-card__label">
            {t("approval.sandboxCapabilityDevices")}
          </div>
          <ul className="sandbox-capability-card__device-list">
            {delta.devices!.map((d: WireCapabilityDevice, i: number) => (
              <li key={i} className="sandbox-capability-card__device-item">
                <span className="sandbox-capability-card__device-path">
                  {d.path}
                </span>
                <span className="sandbox-capability-card__device-canonical">
                  {d.canonical}
                </span>
                <span className="sandbox-capability-card__device-kind">
                  {d.kind}
                </span>
                <span className="sandbox-capability-card__device-major-minor">
                  {d.major}:{d.minor}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {/* ⚠ Risk findings */}
      {hasRiskFindings && (
        <div
          className={`sandbox-capability-card__section ${isCriticalRisk ? "sandbox-capability-card__risk--critical" : ""}`}
        >
          <div className="sandbox-capability-card__label">
            {t("approval.sandboxCapabilityRisk")}
          </div>
          <ul className="sandbox-capability-card__finding-list">
            {review.risk.findings!.map(
              (f: WireCapabilityRiskFinding, i: number) => (
                <li
                  key={i}
                  className={`sandbox-capability-card__finding-item ${isCriticalRisk ? "sandbox-capability-card__finding-item--critical" : ""}`}
                >
                  <span className="sandbox-capability-card__finding-code">
                    [{f.code}]
                  </span>
                  <span className="sandbox-capability-card__finding-message">
                    {findingMessage(f)}
                  </span>
                </li>
              ),
            )}
          </ul>
        </div>
      )}

      {/* Warnings */}
      {hasWarnings && (
        <div className="sandbox-capability-card__section">
          <ul className="sandbox-capability-card__warning-list">
            {sc.warnings!.map((w: string, i: number) => (
              <li key={i} className="sandbox-capability-card__warning-item">
                ⚠ {w}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
