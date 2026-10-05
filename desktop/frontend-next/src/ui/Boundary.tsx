import { Component, type ReactNode } from "react";

// Model output is arbitrary text and the renderers that read it can throw —
// KaTeX does it for one undefined control sequence. A throw during render
// unmounts the whole tree, so the window goes blank over a single bad token.
// Falling one message back to its source text keeps the rest readable.
//
// retryKey is what the failure was about. When it changes the children get
// another try: a throw on half a streamed answer, or a renderer chunk that
// failed to load once, must not hold the finished answer in the fallback.
interface Props {
  fallback: ReactNode;
  children: ReactNode;
  retryKey?: unknown;
}

interface State {
  failed: boolean;
  key: unknown;
}

export class Boundary extends Component<Props, State> {
  state: State = { failed: false, key: this.props.retryKey };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  static getDerivedStateFromProps(props: Props, state: State): Partial<State> | null {
    return props.retryKey !== state.key ? { failed: false, key: props.retryKey } : null;
  }

  componentDidCatch(error: unknown) {
    console.warn("[reasonix] a block fell back to its source text:", error);
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}
