import { Component, type ReactNode } from "react";
import { reportCrash } from "../lib/crash";

export class ErrorBoundary extends Component<{ children: ReactNode }, { crashed: boolean }> {
  state = { crashed: false };

  static getDerivedStateFromError() {
    return { crashed: true };
  }

  componentDidCatch(error: unknown, info: { componentStack?: string | null }) {
    reportCrash("react", error, info.componentStack ?? undefined);
  }

  render() {
    if (this.state.crashed) {
      return (
        <div className="error-boundary-fallback" role="alert">
          <p className="error-boundary-fallback__message">Something went wrong rendering this section.</p>
          <button
            className="error-boundary-fallback__retry"
            type="button"
            onClick={() => this.setState({ crashed: false })}
          >
            Try again
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
