import { memo, useContext } from "react";
import type { AssistantItem } from "../lib/transcriptRows";
import { AssistantMessage } from "./Message";
import { LiveStreamContext } from "./LiveStreamContext";

export const LiveAssistantMessage = memo(function LiveAssistantMessage({
  item,
  creationMode = false,
}: {
  item: AssistantItem;
  creationMode?: boolean;
}) {
  const live = useContext(LiveStreamContext);
  const shown = {
    ...item,
    ...(live && live.id === item.id
      ? {
          text: live.text,
          reasoning: "",
          streaming: true,
          reasoningComplete: true,
          reasoningDurationMs: undefined,
        }
      : { reasoning: "", reasoningComplete: true, reasoningDurationMs: undefined }),
  };
  return <AssistantMessage item={shown} defaultExpanded={false} expandWhileStreaming={false} creationMode={creationMode} />;
});
