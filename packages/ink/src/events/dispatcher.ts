import {
  ContinuousEventPriority,
  DefaultEventPriority,
  DiscreteEventPriority,
  NoEventPriority,
} from 'react-reconciler/constants.js'
import { logError } from '../_internal/log.js'
import { type EventHandlerProps, HANDLER_FOR_EVENT } from './event-handlers.js'
import type { EventTarget, TerminalEvent } from './terminal-event.js'

type DispatchPhase = 'capturing' | 'at_target' | 'bubbling'

type DispatchListener = {
  node: EventTarget
  handler: (event: TerminalEvent) => void
  phase: DispatchPhase,
}

function resolveHandler(
  node: EventTarget,
  eventType: string,
  capture: boolean,
): ((event: TerminalEvent) => void) | undefined {
  const handlers = node._eventHandlers as EventHandlerProps | undefined
  if (!handlers) return undefined

  const mapping = HANDLER_FOR_EVENT[eventType]
  if (!mapping) return undefined

  const propName = capture ? mapping.capture : mapping.bubble
  if (!propName) return undefined

  return handlers[propName] as ((event: TerminalEvent) => void) | undefined
}

function collectListeners(
  target: EventTarget,
  event: TerminalEvent,
): DispatchListener[] {
  const listeners: DispatchListener[] = []

  let node: EventTarget | undefined = target
  while (node) {
    const isTarget = node === target

    const captureHandler = resolveHandler(node, event.type, true)
    const bubbleHandler = resolveHandler(node, event.type, false)

    if (captureHandler) {
      listeners.unshift({
        node,
        handler: captureHandler,
        phase: isTarget ? 'at_target' : 'capturing',
      })
    }

    if (bubbleHandler && (event.bubbles || isTarget)) {
      listeners.push({
        node,
        handler: bubbleHandler,
        phase: isTarget ? 'at_target' : 'bubbling',
      })
    }

    node = node.parentNode
  }

  return listeners
}

function runDispatchQueue(
  listeners: DispatchListener[],
  event: TerminalEvent,
): void {
  let previousNode: EventTarget | undefined

  for (const { node, handler, phase } of listeners) {
    if (event._isImmediatePropagationStopped()) {
      break
    }

    if (event._isPropagationStopped() && node !== previousNode) {
      break
    }

    event._setEventPhase(phase)
    event._setCurrentTarget(node)
    event._prepareForTarget(node)

    try {
      handler(event)
    } catch (error) {
      logError(error)
    }

    previousNode = node
  }
}

/** Map terminal-event types to React scheduler priorities. */
function priorityForEventType(eventType: string): number {
  switch (eventType) {
    case 'keydown':
    case 'keyup':
    case 'click':
    case 'focus':
    case 'blur':
    case 'paste':
      return DiscreteEventPriority as number
    case 'resize':
    case 'scroll':
    case 'mousemove':
      return ContinuousEventPriority as number
    default:
      return DefaultEventPriority as number
  }
}

type DiscreteUpdates = <A, B>(
  fn: (a: A, b: B) => boolean,
  a: A,
  b: B,
  c: undefined,
  d: undefined,
) => boolean

/** State and entry points for terminal event dispatch. */
export class Dispatcher {
  currentEvent: TerminalEvent | null = null
  currentUpdatePriority: number = DefaultEventPriority as number
  discreteUpdates: DiscreteUpdates | null = null

  resolveEventPriority(): number {
    if (this.currentUpdatePriority !== (NoEventPriority as number)) {
      return this.currentUpdatePriority
    }
    if (this.currentEvent) {
      return priorityForEventType(this.currentEvent.type)
    }
    return DefaultEventPriority as number
  }

  dispatch(target: EventTarget, event: TerminalEvent): boolean {
    const previousEvent = this.currentEvent
    this.currentEvent = event
    try {
      event._setTarget(target)

      const listeners = collectListeners(target, event)
      runDispatchQueue(listeners, event)

      event._setEventPhase('none')
      event._setCurrentTarget(null)

      return !event.defaultPrevented
    } finally {
      this.currentEvent = previousEvent
    }
  }

  /** Dispatch under discrete (synchronous) priority. */
  dispatchDiscrete(target: EventTarget, event: TerminalEvent): boolean {
    if (!this.discreteUpdates) {
      return this.dispatch(target, event)
    }
    return this.discreteUpdates(
      (t, e) => this.dispatch(t, e),
      target,
      event,
      undefined,
      undefined,
    )
  }

  /** Dispatch under continuous priority. */
  dispatchContinuous(target: EventTarget, event: TerminalEvent): boolean {
    const previousPriority = this.currentUpdatePriority
    try {
      this.currentUpdatePriority = ContinuousEventPriority as number
      return this.dispatch(target, event)
    } finally {
      this.currentUpdatePriority = previousPriority
    }
  }
}
