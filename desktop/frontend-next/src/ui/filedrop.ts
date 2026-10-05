import { useCallback, useRef } from "react";
import { host } from "../port/host";

// What a drop hands over depends on which shell the page is in, and the two
// halves are not interchangeable. A window reports where the file lives, which
// is the only form that lets a turn work on the file itself. A browser tab
// reports bytes and no path, because a page there is never told one.
export interface Dropped {
  paths: string[];
  files: File[];
  // A drag that carried no file at all — a link, a selection. Absent for every
  // drop that did, so the two never have to be told apart downstream.
  text?: string;
}

// How long a window's drop waits for the paths before it settles for the bytes
// it already has.
const PATH_GRACE = 250;

interface Clock {
  now: () => number;
  wait: (fn: () => void, ms: number) => void;
  grace: number;
}

// A drop and the paths describing it arrive on two channels, and which lands
// first is a platform detail: on Windows the paths take a round trip through
// the shell and follow the DOM event, while on macOS the native handler posts
// them before WebKit dispatches the drop at all. The router settles a drop once
// in either order, and settles for the bytes when no path ever comes — a file
// dragged out of another application can be a promise rather than something on
// disk, and answering that with nothing is how a drop looks broken.
export interface DropRouter<Z> {
  over(zone: Z | null): void;
  dropped(zone: Z, files: File[], text?: string): void;
  paths(list: string[]): void;
  forget(zone: Z): void;
}

export function createDropRouter<Z>(deliver: (zone: Z, d: Dropped) => void, clock: Clock): DropRouter<Z> {
  let hover: Z | null = null;
  let pending: { id: number; zone: Z; files: File[]; text?: string } | null = null;
  let seq = 0;
  let settled = -Infinity;

  const settle = (zone: Z, d: Dropped) => {
    settled = clock.now();
    pending = null;
    deliver(zone, d);
  };

  return {
    over(zone) {
      hover = zone;
    },
    dropped(zone, files, text) {
      // The paths beat the DOM event to it, and this is the same drop.
      if (clock.now() - settled < clock.grace) return;
      // No file was dropped, so no host will ever name one: waiting out the
      // grace here would only make a dropped link feel broken.
      if (files.length === 0 && text) {
        settle(zone, { paths: [], files, text });
        return;
      }
      const id = ++seq;
      pending = { id, zone, files, text };
      clock.wait(() => {
        if (pending?.id === id) settle(zone, { paths: [], files, text });
      }, clock.grace);
    },
    paths(list) {
      const zone = pending?.zone ?? hover;
      const files = pending?.files ?? [];
      const text = pending?.text;
      pending = null;
      if (!zone || list.length === 0) return;
      settle(zone, { paths: list, files, text });
    },
    forget(zone) {
      if (hover === zone) hover = null;
      if (pending?.zone === zone) pending = null;
    },
  };
}

interface Zone {
  el: HTMLElement;
  onDrop: (d: Dropped) => void;
  // The transfer rides along so a zone can say what letting go will do while
  // the drag is still in the air; it names kinds and MIME types only, never a
  // path and never bytes.
  onOver: (over: boolean, dt: DataTransfer | null) => void;
}

const zones = new Set<Zone>();
let router: DropRouter<Zone> | null = null;
let lit: Zone | null = null;

function carriesFiles(e: DragEvent): boolean {
  const types = e.dataTransfer ? [...e.dataTransfer.types] : [];
  return types.includes("Files") || types.includes("text/uri-list") || types.includes("text/plain");
}

function zoneAt(target: EventTarget | null): Zone | null {
  if (!(target instanceof Node)) return null;
  for (const zone of zones) {
    if (zone.el.contains(target)) return zone;
  }
  return null;
}

function light(next: Zone | null, dt: DataTransfer | null = null) {
  if (lit === next) return;
  lit?.onOver(false, null);
  lit = next;
  lit?.onOver(true, dt);
  router?.over(next);
}

// install wires the window once, at boot, and not from a drop zone: a page with
// no zone mounted still has to refuse a file, or the webview navigates to it and
// the app is replaced by whatever was dropped. Nothing else in the window
// prevents that default, and there is no way back short of a reload.
export function install() {
  if (router) return;
  router = createDropRouter<Zone>((zone, d) => zone.onDrop(d), {
    now: () => Date.now(),
    wait: (fn, ms) => setTimeout(fn, ms),
    grace: PATH_GRACE,
  });

  addEventListener(
    "dragover",
    (e: DragEvent) => {
      if (!carriesFiles(e)) return;
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
      light(zoneAt(e.target), e.dataTransfer);
    },
    true,
  );
  // A drag that leaves the window entirely reports a null relatedTarget; one
  // crossing between two elements reports the element it entered, and clearing
  // on that would flicker the highlight off under the pointer.
  addEventListener(
    "dragleave",
    (e: DragEvent) => {
      if (carriesFiles(e) && !e.relatedTarget) light(null);
    },
    true,
  );
  addEventListener(
    "drop",
    (e: DragEvent) => {
      if (!carriesFiles(e)) return;
      e.preventDefault();
      const zone = zoneAt(e.target) ?? lit;
      light(null);
      if (!zone) return;
      const dt = e.dataTransfer;
      const files = [...(dt?.files ?? [])];
      const text = files.length > 0 ? undefined : dt?.getData("text/uri-list") || dt?.getData("text/plain") || undefined;
      router?.dropped(zone, files, text);
      // A shell that can place the files answers here and now; the grace
      // period is what a browser tab, which never learns a path, waits out.
      const placed = host().pathsForFiles(files);
      if (placed.length > 0) router?.paths(placed);
    },
    true,
  );
}

// useFileDrop makes one element the target of a drop, and answers with the ref
// to hang on it. Which element a drop landed on is decided here, against the
// DOM, because that is the only place it is a fact: the shell can offer
// coordinates, and coordinates stop agreeing with CSS pixels the moment the
// interface is zoomed.
//
// A ref rather than an effect on a held element: a pane that swaps its drop
// zone for another screen mounts a different node, and an effect keyed on the
// ref object would keep the old one registered.
export function useFileDrop(
  onDrop: (d: Dropped) => void,
  onOver: (over: boolean, dt: DataTransfer | null) => void,
): (node: HTMLElement | null) => (() => void) | void {
  // Read through a ref so a caller does not have to memoize either callback to
  // avoid re-registering the zone on every render.
  const live = useRef({ onDrop, onOver });
  live.current = { onDrop, onOver };
  return useCallback((node: HTMLElement | null) => {
    if (!node) return;
    const zone: Zone = {
      el: node,
      onDrop: (d) => live.current.onDrop(d),
      onOver: (over, dt) => live.current.onOver(over, dt),
    };
    zones.add(zone);
    return () => {
      zones.delete(zone);
      if (lit === zone) lit = null;
      router?.forget(zone);
    };
  }, []);
}
