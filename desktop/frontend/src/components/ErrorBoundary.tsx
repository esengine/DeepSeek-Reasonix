import { Component, type ReactNode } from "react";
import { reportCrash } from "../lib/crash";

export class ErrorBoundary extends Component<{ children: ReactNode }, { crashed: boolean; error: string }> {
  state = { crashed: false, error: "" };

  static getDerivedStateFromError(error: unknown) {
    return { crashed: true, error: String(error) };
  }

  componentDidCatch(error: unknown, info: { componentStack?: string | null }) {
    reportCrash("react", error, info.componentStack ?? undefined);
  }

  render() {
    if (this.state.crashed) {
      return (
        <div style={{ padding: 40, fontFamily: "monospace", whiteSpace: "pre-wrap" }}>
          <h2>React Error</h2>
          <pre>{this.state.error}</pre>
        </div>
      );
    }
    return this.props.children;
  }
}
