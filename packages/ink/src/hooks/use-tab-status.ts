import { useContext, useEffect, useRef } from 'react'
import {
  CLEAR_TAB_STATUS,
  supportsTabStatus,
  tabStatus,
  wrapForMultiplexer,
} from '../termio/osc.js'
import type { Color } from '../termio/types.js'
import { TerminalWriteContext } from '../useTerminalNotification.js'

export type TabStatusKind = 'idle' | 'busy' | 'waiting'

const rgb = (r: number, g: number, b: number): Color => ({
  type: 'rgb',
  r,
  g,
  b,
})

type TabStatusPreset = {
  indicator: Color
  status: string
  statusColor: Color
}

// Mapping mirrors the suggested defaults from the OSC 21337 usage guide;
// terminals that support the sequence render these as a tab-sidebar dot
// plus a short label.
const TAB_STATUS_PRESETS: Record<TabStatusKind, TabStatusPreset> = {
  idle: {
    indicator: rgb(0, 215, 95),
    status: 'Idle',
    statusColor: rgb(136, 136, 136),
  },
  busy: {
    indicator: rgb(255, 149, 0),
    status: 'Working…',
    statusColor: rgb(255, 149, 0),
  },
  waiting: {
    indicator: rgb(95, 135, 255),
    status: 'Waiting',
    statusColor: rgb(95, 135, 255),
  },
}

/** Declaratively project a tab-status indicator into the terminal sidebar. */
export function useTabStatus(kind: TabStatusKind | null): void {
  const writeRaw = useContext(TerminalWriteContext)
  const prevKindRef = useRef<TabStatusKind | null>(null)

  useEffect(() => {
    // Transitioning from "had a dot" to "wants none" — clear it so the
    // sidebar doesn't keep showing stale state after the consumer disabled
    // the feature.
    if (kind === null) {
      if (prevKindRef.current !== null && writeRaw && supportsTabStatus()) {
        writeRaw(wrapForMultiplexer(CLEAR_TAB_STATUS))
      }
      prevKindRef.current = null
      return
    }

    prevKindRef.current = kind
    if (!writeRaw || !supportsTabStatus()) return
    writeRaw(wrapForMultiplexer(tabStatus(TAB_STATUS_PRESETS[kind])))
  }, [kind, writeRaw])
}
