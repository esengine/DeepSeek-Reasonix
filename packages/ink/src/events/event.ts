/** Minimal base class for every event the runtime hands to user code. */
export class Event {
  private didStop = false

  didStopImmediatePropagation(): boolean {
    return this.didStop
  }

  stopImmediatePropagation(): void {
    this.didStop = true
  }
}
