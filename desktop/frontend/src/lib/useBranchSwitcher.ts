// Branch list, checkout and create for the launcher's branch row. The list is
// fetched lazily the first time the menu opens; the active branch is mirrored
// locally so a checkout flips the row immediately while the workspace meta
// catches up on its own (cached) cadence.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { RefObject } from "react";
import { app } from "./bridge";
import { markWorkspaceRefresh } from "./workspaceRefreshStore";

// Mirrors the backend's validGitBranchName so the create action is only
// enabled for names git would accept.
export function isValidBranchName(name: string): boolean {
  const trimmed = name.trim();
  if (!trimmed || trimmed.startsWith("-") || trimmed.includes("..") ||
      trimmed.endsWith("/") || trimmed.endsWith(".") || trimmed.includes("@{")) {
    return false;
  }
  return !/[\s~^:?*[\\\t\n]/.test(trimmed);
}

interface BranchSwitcherInput {
  tabId: string;
  scopeKey: string;
  workspaceRoot: string;
  /** Current git branch for the active workspace; undefined when unknown. */
  gitBranch: string | undefined;
  /** The launcher card, used to dismiss the menu on an outside click. */
  rootRef: RefObject<HTMLElement | null>;
  /** Fired after a checkout/create so the caller can refresh derived data. */
  onBranchChanged: () => void;
}

export function useBranchSwitcher({ tabId, scopeKey, workspaceRoot, gitBranch, rootRef, onBranchChanged }: BranchSwitcherInput) {
  const identity = useMemo(() => ({}), [tabId, scopeKey, workspaceRoot]);
  const currentIdentity = useRef<object | null>(null);
  const listRequest = useRef(0);
  const operation = useRef<object | null>(null);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [branches, setBranches] = useState<string[]>([]);
  const [branchesLoading, setBranchesLoading] = useState(false);
  const [branchesErr, setBranchesErr] = useState("");
  const [branchQuery, setBranchQuery] = useState("");
  const [switchingBranch, setSwitchingBranch] = useState("");
  const [branchSwitchErr, setBranchSwitchErr] = useState("");
  // Local mirror of the active branch: the workspace meta refreshes on a
  // cached cadence, so a checkout flips this immediately and the prop catches
  // up (the prop wins whenever it diverges from local optimism).
  const [activeBranch, setActiveBranch] = useState(gitBranch);
  const branchSearchRef = useRef<HTMLInputElement | null>(null);
  const onBranchChangedRef = useRef(onBranchChanged);
  onBranchChangedRef.current = onBranchChanged;

  useLayoutEffect(() => {
    currentIdentity.current = identity;
    listRequest.current++;
    operation.current = null;
    setBranches([]);
    setBranchesLoading(false);
    setBranchesErr("");
    setBranchQuery("");
    setBranchMenuOpen(false);
    setSwitchingBranch("");
    setBranchSwitchErr("");
    return () => { currentIdentity.current = null; };
  }, [identity]);

  useEffect(() => {
    setActiveBranch(gitBranch);
  }, [gitBranch, identity]);

  // Dismiss the branch switcher on any click outside the launcher card, plus
  // Escape — baseline popover behavior.
  useEffect(() => {
    if (!branchMenuOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      if (rootRef.current && event.target instanceof Node && !rootRef.current.contains(event.target)) {
        setBranchMenuOpen(false);
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setBranchMenuOpen(false);
    };
    window.addEventListener("pointerdown", onPointerDown, true);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown, true);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [branchMenuOpen, rootRef]);

  const loadBranches = useCallback(async () => {
    if (!tabId || currentIdentity.current !== identity) return;
    const request = ++listRequest.current;
    const current = () => currentIdentity.current === identity && request === listRequest.current;
    setBranchesLoading(true);
    setBranchesErr("");
    try {
      const list = await app.GitBranchesForTab(tabId, workspaceRoot);
      if (current()) setBranches(Array.isArray(list) ? list : []);
    } catch (error) {
      if (current()) setBranchesErr(error instanceof Error ? error.message : String(error));
    } finally {
      if (current()) setBranchesLoading(false);
    }
  }, [identity, tabId, workspaceRoot]);

  const toggleBranchMenu = useCallback(() => {
    const next = !branchMenuOpen;
    setBranchMenuOpen(next);
    setBranchSwitchErr("");
    setBranchQuery("");
    if (next && branches.length === 0 && !branchesLoading) void loadBranches();
    if (next) window.setTimeout(() => {
      if (currentIdentity.current === identity) branchSearchRef.current?.focus();
    }, 0);
  }, [branchMenuOpen, branches.length, branchesLoading, loadBranches, identity]);

  const finishBranchSwitch = useCallback((branch: string) => {
    if (currentIdentity.current !== identity) return;
    setActiveBranch(branch);
    setBranchMenuOpen(false);
    setBranchQuery("");
    // The working tree just changed under the diff badge.
    markWorkspaceRefresh(tabId, scopeKey);
    onBranchChangedRef.current();
    void loadBranches();
  }, [identity, loadBranches, tabId, scopeKey]);

  const checkoutBranch = useCallback(async (branch: string) => {
    if (!tabId || operation.current || currentIdentity.current !== identity) return;
    const request = {};
    operation.current = request;
    setSwitchingBranch(branch);
    setBranchSwitchErr("");
    try {
      await app.GitCheckoutForTab(tabId, workspaceRoot, branch);
      if (operation.current === request) finishBranchSwitch(branch);
    } catch (error) {
      if (currentIdentity.current === identity && operation.current === request) setBranchSwitchErr(error instanceof Error ? error.message : String(error));
    } finally {
      if (currentIdentity.current === identity && operation.current === request) {
        operation.current = null;
        setSwitchingBranch("");
      }
    }
  }, [finishBranchSwitch, identity, tabId, workspaceRoot]);

  const createBranch = useCallback(async (name: string) => {
    if (!tabId || operation.current || currentIdentity.current !== identity) return;
    const request = {};
    operation.current = request;
    setSwitchingBranch(name);
    setBranchSwitchErr("");
    try {
      await app.GitCreateBranchForTab(tabId, workspaceRoot, name);
      if (operation.current === request) finishBranchSwitch(name);
    } catch (error) {
      if (currentIdentity.current === identity && operation.current === request) setBranchSwitchErr(error instanceof Error ? error.message : String(error));
    } finally {
      if (currentIdentity.current === identity && operation.current === request) {
        operation.current = null;
        setSwitchingBranch("");
      }
    }
  }, [finishBranchSwitch, identity, tabId, workspaceRoot]);

  const trimmedQuery = branchQuery.trim();
  const filteredBranches = useMemo(() => {
    const q = trimmedQuery.toLowerCase();
    return q ? branches.filter((branch) => branch.toLowerCase().includes(q)) : branches;
  }, [branches, trimmedQuery]);
  const exactMatch = filteredBranches.some((branch) => branch === trimmedQuery);
  const canCreate = trimmedQuery !== "" && !exactMatch && isValidBranchName(trimmedQuery);

  return {
    activeBranch,
    branchMenuOpen,
    branchSearchRef,
    branches,
    branchesLoading,
    branchesErr,
    branchQuery,
    setBranchQuery,
    branchSwitchErr,
    setBranchSwitchErr,
    switchingBranch,
    filteredBranches,
    exactMatch,
    canCreate,
    trimmedQuery,
    toggleBranchMenu,
    checkoutBranch,
    createBranch,
  };
}
