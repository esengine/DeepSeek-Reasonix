import {
  type Dispatch,
  type MutableRefObject,
  type SetStateAction,
  useEffect,
  useRef,
  useState,
} from "react";

export interface WorkspaceRoot {
  /** Live working directory — every rootDir-dependent surface (hook cwd, memory root, shell allowlist root, `@file` mention root, applyEditBlocks base, run_command cwd) reads this. */
  currentRootDir: string;
  setCurrentRootDir: Dispatch<SetStateAction<string>>;
  /** Mirror for closures captured at boot (dashboard server, tool interceptor) — without it those reads freeze on the launch root after `/cwd`. */
  currentRootDirRef: MutableRefObject<string>;
}

export function useWorkspaceRoot(launchRoot: string | undefined): WorkspaceRoot {
  const [currentRootDir, setCurrentRootDir] = useState<string>(() => launchRoot ?? process.cwd());
  const currentRootDirRef = useRef<string>(currentRootDir);
  useEffect(() => {
    currentRootDirRef.current = currentRootDir;
  }, [currentRootDir]);
  return { currentRootDir, setCurrentRootDir, currentRootDirRef };
}
