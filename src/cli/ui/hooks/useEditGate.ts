import {
  type Dispatch,
  type MutableRefObject,
  type SetStateAction,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import type { EditBlock } from "../../../code/edit-blocks.js";
import { type EditMode, loadEditMode, saveEditMode } from "../../../config.js";

const FLASH_MS = 1200;

export interface EditGate {
  pendingEdits: MutableRefObject<EditBlock[]>;
  pendingCount: number;
  /** Bumped on every queue-mutating sync so /walk's `useMemo` re-picks block 0 of the new queue. */
  pendingTick: number;
  syncPendingCount: () => void;
  editMode: EditMode;
  setEditMode: Dispatch<SetStateAction<EditMode>>;
  /** Live-mode mirror — interceptor closure reads this so mode cycles don't reinstall the hook. */
  editModeRef: MutableRefObject<EditMode>;
  /** True for ~1.2s after a mode flip — drives the soft "yes, it changed" highlight on the bottom bar. */
  modeFlash: boolean;
}

export function useEditGate(codeMode: boolean): EditGate {
  const pendingEdits = useRef<EditBlock[]>([]);
  const [pendingCount, setPendingCount] = useState(0);
  const [pendingTick, setPendingTick] = useState(0);
  const syncPendingCount = useCallback(() => {
    setPendingCount(pendingEdits.current.length);
    setPendingTick((t) => t + 1);
  }, []);

  const [editMode, setEditMode] = useState<EditMode>(() => (codeMode ? loadEditMode() : "review"));
  const editModeRef = useRef<EditMode>(editMode);
  useEffect(() => {
    editModeRef.current = editMode;
    if (codeMode) saveEditMode(editMode);
  }, [editMode, codeMode]);

  const [modeFlash, setModeFlash] = useState(false);
  const flashTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const prevEditModeRef = useRef<EditMode>(editMode);
  useEffect(() => {
    if (prevEditModeRef.current === editMode) return;
    prevEditModeRef.current = editMode;
    setModeFlash(true);
    if (flashTimerRef.current) clearTimeout(flashTimerRef.current);
    flashTimerRef.current = setTimeout(() => {
      setModeFlash(false);
      flashTimerRef.current = null;
    }, FLASH_MS);
  }, [editMode]);

  return {
    pendingEdits,
    pendingCount,
    pendingTick,
    syncPendingCount,
    editMode,
    setEditMode,
    editModeRef,
    modeFlash,
  };
}
