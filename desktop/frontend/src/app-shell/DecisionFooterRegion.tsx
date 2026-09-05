import { lazy, Suspense, type ComponentProps, type CSSProperties } from "react";

import { Composer } from "../components/Composer";

const TodoPanel = lazy(() => import("../components/TodoPanel").then((module) => ({ default: module.TodoPanel })));
const UndoRewindBanner = lazy(() => import("../components/UndoRewindBanner").then((module) => ({ default: module.UndoRewindBanner })));
const ApprovalModal = lazy(() => import("../components/ApprovalModal").then((module) => ({ default: module.ApprovalModal })));
const AskCard = lazy(() => import("../components/AskCard").then((module) => ({ default: module.AskCard })));
const MCPInteractionCard = lazy(() => import("../components/MCPInteractionCard").then((module) => ({ default: module.MCPInteractionCard })));
const ExtensionFormDialog = lazy(() => import("../components/ExtensionFormDialog").then((module) => ({ default: module.ExtensionFormDialog })));
const RuntimeDecisionCard = lazy(() => import("../components/RuntimeDecisionCard").then((module) => ({ default: module.RuntimeDecisionCard })));
const ClearContextCard = lazy(() => import("../components/ClearContextCard").then((module) => ({ default: module.ClearContextCard })));

type ComposerProps = ComponentProps<(typeof import("../components/Composer"))["Composer"]>;
type TodoProps = ComponentProps<(typeof import("../components/TodoPanel"))["TodoPanel"]>;
type UndoProps = ComponentProps<(typeof import("../components/UndoRewindBanner"))["UndoRewindBanner"]>;
type ApprovalProps = ComponentProps<(typeof import("../components/ApprovalModal"))["ApprovalModal"]>;
type AskProps = ComponentProps<(typeof import("../components/AskCard"))["AskCard"]>;
type McpProps = ComponentProps<(typeof import("../components/MCPInteractionCard"))["MCPInteractionCard"]>;
type ExtensionProps = ComponentProps<(typeof import("../components/ExtensionFormDialog"))["ExtensionFormDialog"]>;
type RuntimeDecisionProps = ComponentProps<(typeof import("../components/RuntimeDecisionCard"))["RuntimeDecisionCard"]>;
type ClearContextProps = ComponentProps<(typeof import("../components/ClearContextCard"))["ClearContextCard"]>;

export type DecisionFooterSurface =
  | { kind: "approval"; identity: string; props: ApprovalProps }
  | { kind: "ask"; identity: string; props: AskProps }
  | { kind: "mcp"; identity: string; props: McpProps }
  | { kind: "extension"; identity: string; props: ExtensionProps }
  | { kind: "runtime"; identity: string; props: RuntimeDecisionProps }
  | { kind: "clear-context"; identity: string; props: ClearContextProps };

type DecisionFooterRegionProps = {
  hidden: boolean;
  className: string;
  style?: CSSProperties;
  footerRef: ComponentProps<"footer">["ref"];
  todo?: { identity: string; props: TodoProps };
  undo?: { identity: string; props: UndoProps };
  decision?: DecisionFooterSurface;
  composer: {
    hidden: boolean;
    inert: boolean;
    hero: boolean;
    headline?: string;
    props: ComposerProps;
  };
};

function DecisionSurface({ surface }: { surface: DecisionFooterSurface }) {
  switch (surface.kind) {
    case "approval":
      return <ApprovalModal key={surface.identity} {...surface.props} />;
    case "ask":
      return <AskCard key={surface.identity} {...surface.props} />;
    case "mcp":
      return <MCPInteractionCard key={surface.identity} {...surface.props} />;
    case "extension":
      return <ExtensionFormDialog key={surface.identity} {...surface.props} />;
    case "runtime":
      return <RuntimeDecisionCard key={surface.identity} {...surface.props} />;
    case "clear-context":
      return <ClearContextCard key={surface.identity} {...surface.props} />;
  }
}

export function DecisionFooterRegion({
  hidden,
  className,
  style,
  footerRef,
  todo,
  undo,
  decision,
  composer,
}: DecisionFooterRegionProps) {
  if (hidden) return null;

  return (
    <footer className={className} ref={footerRef} style={style} inert={composer.inert || undefined} aria-hidden={composer.inert || undefined}>
      <Suspense fallback={null}>
        {todo ? <TodoPanel key={todo.identity} {...todo.props} /> : null}
        {undo ? <UndoRewindBanner key={undo.identity} {...undo.props} /> : null}
        {decision ? <DecisionSurface surface={decision} /> : null}
      </Suspense>
      {/* Composer remains mounted while decisions are visible so session-scoped drafts survive. */}
      <div
        className={[
          "composer-decision-host",
          composer.inert ? "composer-decision-host--footprint-hidden" : composer.hidden ? "composer-decision-host--hidden" : "",
          composer.hero ? "composer-decision-host--creation-hero" : "",
        ].filter(Boolean).join(" ")}
        hidden={Boolean(decision) || undefined}
        inert={composer.hidden ? true : undefined}
        aria-hidden={composer.hidden ? true : undefined}
      >
        {composer.hero && composer.headline ? <h2 className="welcome-creation__headline">{composer.headline}</h2> : null}
        <Composer {...composer.props} />
      </div>
    </footer>
  );
}
