import { useEffect, useLayoutEffect } from 'react'
import { useEventCallback } from 'usehooks-ts'
import type { InputEvent, Key } from '../events/input-event.js'
import useStdin from './use-stdin.js'

type InputHandler = (input: string, key: Key, event: InputEvent) => void

type UseInputOptions = {
  /** Gate the handler without unmounting it. */
  isActive?: boolean
}

/** Subscribe to keyboard input from the active stdin stream. */
const useInput = (inputHandler: InputHandler, options: UseInputOptions = {}): void => {
  const { setRawMode, internal_exitOnCtrlC, internal_eventEmitter } = useStdin()

  // Layout-phase raw-mode toggle: see header comment for why this can't be
  // a regular useEffect.
  useLayoutEffect(() => {
    if (options.isActive === false) {
      return
    }

    setRawMode(true)

    return () => {
      setRawMode(false)
    }
  }, [options.isActive, setRawMode])

  // Stable-identity dispatcher that reads the latest options/handler from
  // closure. See header comment on listener ordering.
  const handleData = useEventCallback((event: InputEvent) => {
    if (options.isActive === false) {
      return
    }
    const { input, key } = event

    // Ctrl+C is the host's exit signal when exitOnCtrlC is on — only
    // forward it to the user handler when they've opted out. The emitter
    // already runs listeners inside a high-priority React update, so we
    // don't need to wrap our setState calls here.
    if (!(input === 'c' && key.ctrl) || !internal_exitOnCtrlC) {
      inputHandler(input, key, event)
    }
  })

  useEffect(() => {
    internal_eventEmitter?.on('input', handleData)

    return () => {
      internal_eventEmitter?.removeListener('input', handleData)
    }
  }, [internal_eventEmitter, handleData])
}

export default useInput
