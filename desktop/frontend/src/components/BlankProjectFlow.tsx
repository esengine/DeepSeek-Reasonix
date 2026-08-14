import { useEffect, useRef, useState } from "react";
import { app } from "../lib/bridge";
import { useToast } from "../lib/toast";
import { BlankProjectDialog } from "./BlankProjectDialog";

export function BlankProjectFlow({
  onOpenProject,
  onRefresh,
  onClose,
}: {
  onOpenProject: (path: string) => Promise<void>;
  onRefresh: () => Promise<void>;
  onClose: () => void;
}) {
  const { showToast } = useToast();
  const onCloseRef = useRef(onClose);
  const showToastRef = useRef(showToast);
  onCloseRef.current = onClose;
  showToastRef.current = showToast;
  const [draft, setDraft] = useState<{ parentDirectory: string; createdPath?: string; error?: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);

  useEffect(() => {
    let cancelled = false;
    void app.PickBlankProjectParent().then((parentDirectory) => {
      if (cancelled) return;
      if (parentDirectory) setDraft({ parentDirectory });
      else onCloseRef.current();
    }).catch((err) => {
      if (cancelled) return;
      showToastRef.current(err instanceof Error ? err.message : String(err), "error");
      onCloseRef.current();
    });
    return () => { cancelled = true; };
  }, []);

  const submit = async (projectName: string) => {
    if (!draft || busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setDraft((current) => current ? { ...current, error: undefined } : current);
    let createdPath = draft.createdPath ?? "";
    try {
      if (!createdPath) {
        createdPath = await app.CreateBlankProject(draft.parentDirectory, projectName);
        setDraft((current) => current ? { ...current, createdPath } : current);
      }
      await onOpenProject(createdPath);
      await onRefresh();
      busyRef.current = false;
      setBusy(false);
      onCloseRef.current();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setDraft((current) => current ? { ...current, createdPath: createdPath || current.createdPath, error: message } : current);
    } finally {
      if (busyRef.current) {
        busyRef.current = false;
        setBusy(false);
      }
    }
  };

  if (!draft) return null;
  return (
    <BlankProjectDialog
      parentDirectory={draft.parentDirectory}
      createdPath={draft.createdPath}
      busy={busy}
      error={draft.error}
      onSubmit={(name) => void submit(name)}
      onCancel={() => {
        if (!busyRef.current) onCloseRef.current();
      }}
    />
  );
}
