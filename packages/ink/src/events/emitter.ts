import { EventEmitter as NodeEventEmitter } from 'events'
import { Event } from './event.js'

/** Node's EventEmitter, taught to respect {@link Event.stopImmediatePropagation}. */
export class EventEmitter extends NodeEventEmitter {
  constructor() {
    super()
    this.setMaxListeners(0)
  }

  override emit(type: string | symbol, ...args: unknown[]): boolean {
    // `error` keeps node's special semantics (unhandled throws): we delegate
    // straight through so callers that rely on the throw-when-unhandled
    // behavior are not silently swallowed.
    if (type === 'error') {
      return super.emit(type, ...args)
    }

    const listeners = this.rawListeners(type)
    if (listeners.length === 0) {
      return false
    }

    // Only the first argument is inspected for the stop flag; events are
    // always emitted as `emit('type', event)` in the runtime.
    const event = args[0] instanceof Event ? args[0] : null

    for (const listener of listeners) {
      listener.apply(this, args)
      if (event?.didStopImmediatePropagation()) {
        break
      }
    }

    return true
  }
}
