import { type Dispatch, type SetStateAction, useCallback, useEffect, useState } from "react";

export interface ProgressSinkRef {
  current:
    | ((info: { toolName: string; progress: number; total?: number; message?: string }) => void)
    | null;
}

export interface ToolProgressDisplay {
  ongoingTool: { name: string; args?: string } | null;
  setOngoingTool: Dispatch<SetStateAction<{ name: string; args?: string } | null>>;
  toolProgress: { progress: number; total?: number; message?: string } | null;
  setToolProgress: Dispatch<
    SetStateAction<{ progress: number; total?: number; message?: string } | null>
  >;
  statusLine: string | null;
  setStatusLine: Dispatch<SetStateAction<string | null>>;
  /** Clears all three — call from the turn-end `finally`. */
  clear: () => void;
}

export function useToolProgressDisplay(progressSink?: ProgressSinkRef): ToolProgressDisplay {
  const [ongoingTool, setOngoingTool] = useState<{ name: string; args?: string } | null>(null);
  const [toolProgress, setToolProgress] = useState<{
    progress: number;
    total?: number;
    message?: string;
  } | null>(null);
  const [statusLine, setStatusLine] = useState<string | null>(null);

  useEffect(() => {
    if (!progressSink) return;
    progressSink.current = (info) => {
      setToolProgress({
        progress: info.progress,
        total: info.total,
        message: info.message,
      });
    };
    return () => {
      if (progressSink.current) progressSink.current = null;
    };
  }, [progressSink]);

  const clear = useCallback(() => {
    setOngoingTool(null);
    setToolProgress(null);
    setStatusLine(null);
  }, []);

  return {
    ongoingTool,
    setOngoingTool,
    toolProgress,
    setToolProgress,
    statusLine,
    setStatusLine,
    clear,
  };
}
