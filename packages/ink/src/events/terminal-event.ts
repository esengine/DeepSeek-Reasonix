import { Event } from './event.js'

type EventPhase = 'none' | 'capturing' | 'at_target' | 'bubbling'

type TerminalEventInit = {
  bubbles?: boolean
  cancelable?: boolean
}

/** DOM-shaped base for events that traverse the rendered node tree. */
export class TerminalEvent extends Event {
  readonly type: string
  readonly timeStamp: number
  readonly bubbles: boolean
  readonly cancelable: boolean

  private currentTargetNode: EventTarget | null = null
  private targetNode: EventTarget | null = null
  private phase: EventPhase = 'none'
  private propagationStopped = false
  private prevented = false

  constructor(type: string, init?: TerminalEventInit) {
    super()
    this.type = type
    this.timeStamp = performance.now()
    this.bubbles = init?.bubbles ?? true
    this.cancelable = init?.cancelable ?? true
  }

  get target(): EventTarget | null {
    return this.targetNode
  }

  get currentTarget(): EventTarget | null {
    return this.currentTargetNode
  }

  get eventPhase(): EventPhase {
    return this.phase
  }

  get defaultPrevented(): boolean {
    return this.prevented
  }

  stopPropagation(): void {
    this.propagationStopped = true
  }

  override stopImmediatePropagation(): void {
    super.stopImmediatePropagation()
    this.propagationStopped = true
  }

  preventDefault(): void {
    // Browsers silently ignore preventDefault on non-cancelable events; we
    // mirror that so passive listeners (focus, resize) cannot block the
    // runtime's default handling.
    if (this.cancelable) {
      this.prevented = true
    }
  }

  // Internal hooks driven by the Dispatcher. The leading underscore keeps
  // them out of editor autocomplete on event objects exposed to user code.

  /** @internal */
  _setTarget(target: EventTarget): void {
    this.targetNode = target
  }

  /** @internal */
  _setCurrentTarget(target: EventTarget | null): void {
    this.currentTargetNode = target
  }

  /** @internal */
  _setEventPhase(phase: EventPhase): void {
    this.phase = phase
  }

  /** @internal */
  _isPropagationStopped(): boolean {
    return this.propagationStopped
  }

  /** @internal */
  _isImmediatePropagationStopped(): boolean {
    return this.didStopImmediatePropagation()
  }

  _prepareForTarget(_target: EventTarget): void {}
}

export type EventTarget = {
  parentNode: EventTarget | undefined
  _eventHandlers?: Record<string, unknown>
}
