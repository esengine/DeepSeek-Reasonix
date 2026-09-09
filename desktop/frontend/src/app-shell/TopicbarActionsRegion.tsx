import { Fragment, type ComponentProps } from "react";
import { ExternalOpener } from "../components/ExternalOpener";
import { TopicbarSessionActions } from "../components/TopicbarSessionActions";

type Props = {
  sessionIdentity?: string;
  external?: ComponentProps<typeof ExternalOpener>;
  session?: ComponentProps<typeof TopicbarSessionActions>;
};

/** Resource keys are local to a role, never shared by heterogeneous siblings. */
export function TopicbarActionsRegion({ sessionIdentity, external, session }: Props) {
  return <>
    <Fragment key="external-opener">
      {external && <ExternalOpener key={external.tabId} {...external} />}
    </Fragment>
    <Fragment key="session-actions">
      {session && <TopicbarSessionActions key={sessionIdentity} {...session} />}
    </Fragment>
  </>;
}
