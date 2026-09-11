import {
  FrontendDiagnosticsControl,
  type FrontendDiagnosticsControlProps,
} from "./FrontendDiagnosticsControl";
import { isFrontendDiagnosticsBuild } from "../lib/frontendDiagnostics";
import "./ScrollDiagnosticPanel.css";

const diagnosticsAvailable = isFrontendDiagnosticsBuild(
  typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "stable",
  Boolean(import.meta.env?.DEV),
);

export { FrontendDiagnosticsControl };
export type { FrontendDiagnosticsControlProps };

export default function FrontendDiagnosticsPanel(props: FrontendDiagnosticsControlProps) {
  if (!diagnosticsAvailable) return null;
  return <FrontendDiagnosticsControl {...props} />;
}
