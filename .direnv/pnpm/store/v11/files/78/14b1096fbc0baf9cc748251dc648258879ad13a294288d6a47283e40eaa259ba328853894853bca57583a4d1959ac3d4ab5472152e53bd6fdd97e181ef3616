declare namespace gsap.core {

  type TimelineChild = string | Animation | Callback | Array<string | Animation | Callback>;

  interface Labels {
    [key: string]: number;
  }

  class Timeline extends Animation {

    autoRemoveChildren: boolean;
    labels: Labels;
    smoothChildTiming: boolean;
    vars: TimelineVars;
    [key: string]: any; // for gsap.registerEffect({... extendTimeline: true})

    constructor(vars?: TimelineVars, time?: number);

    static updateRoot(time: number): void;

    /**
     * Adds a label, tween, timeline, or an array of those values to the timeline, optionally at the specified time.
     *
     * ```js
     * tl.add("myLabel");  // add a label at the end of the timeline
     * tl.add(myTween, 1); // add a tween at the 1 second mark
     * tl.add(myTimeline, "-=1"); // add a timeline 1 second before the end of the timeline
     * tl.add(["myLabel", myTween, myTimeline], "<"); // add a label, tween, and timeline at the start of the previous tween
     * ```
     *
     * @param {TimelineChild} child
     * @param {Position} [position]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/add()
     */
    add(child: TimelineChild, position?: Position): this;

    /**
     * Adds a label to the timeline, optionally at the specified time.
     *
     * ```js
     * tl.addLabel("myLabel", 1); // add a label at the 1 second mark
     * ```
     *
     * @param {string} label
     * @param {Position} [position]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/addLabel()
     */
    addLabel(label: string, position?: Position): this;

    /**
     * Adds a pause to the timeline, optionally at the specified time.
     *
     * ```js
     * tl.addPause(); // add a pause at the end of the timeline
     * tl.addPause(1, myCallback); // add a pause at the 1 second mark with a callback
     * ```
     *
     * @param {Position} [position]
     * @param {Callback} [callback]
     * @param {any[]} [params]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/addPause()
     */
    addPause(position?: Position, callback?: Callback, params?: any[]): this;

    /**
     * Call a function, optionally at the specified time.
     *
     * ```js
     * tl.call(myCallback); // add a function call at the end of the timeline
     * tl.call(myCallback, ["param"], 1); // add a function call at the 1 second mark with a parameter passed in
     * ```
     *
     * @param {Callback} callback
     * @param {any[]} [params]
     * @param {Position} [position]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/call()
     */
    call(callback: Callback, params?: any[], position?: Position): this;

    /**
     * Empties the timeline of all tweens, timelines, callbacks, and optionally labels.
     *
     * ```js
     * tl.clear();     // empty the timeline not including labels
     * tl.clear(true); // empty the timeline including labels
     * ```
     *
     * @param {boolean} labels
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/clear()
     */
    clear(labels?: boolean): this;

    /**
     * Makes the timeline's progress jump to the provided label.
     *
     * ```js
     * tl.currentLabel("myLabel"); 
     * ```
     *
     * @param {string} value
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/currentLabel()
     */
    currentLabel(value: string): this;

    /**
     * Gets the closest label that is at or before the current time.
     *
     * ```js
     * tl.currentLabel();
     * ```
     *
     * @returns {string} The nearest label
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/currentLabel()
     */
    currentLabel(): string;

    /**
     * Creates a tween coming FROM the given values.
     *
     * ```js
     * tl.from(".class", { x: 100 }, "+=1"); // adds the tween one second after the end of the timeline
     * ```
     *
     * @param {TweenTarget} targets
     * @param {TweenVars} vars
     * @param {Position} [position]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/from()
     */
    from(targets: TweenTarget, vars: TweenVars, position?: Position): this;

    /**
     * **Deprecated method signature.** Use the `duration` property instead.
     *
     * ```js
     * tl.from(".class", 1, { x: 100 }, "+=1"); // adds the tween one second after the end of the timeline
     * ```
     *
     * @deprecated since version 3.0.0
     * @param {TweenTarget} targets
     * @param {number} duration - The duration parameter is deprecated. Use the `duration` property instead.
     * @param {TweenVars} vars
     * @param {Position} [position]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/from()
     */
    from(targets: TweenTarget, duration: number, vars: TweenVars, position?: Position): this;

    /**
     * Creates a tween coming FROM the first set of values going TO the second set of values.
     *
     * ```js
     * tl.fromTo(".class", {x: 0}, { x: 100 }, "+=1"); // adds the tween one second after the end of the timeline
     * ```
     *
     * @param {TweenTarget} targets
     * @param {TweenVars} fromVars
     * @param {TweenVars} toVars
     * @param {Position} [position]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/fromTo()
     */
    fromTo(targets: TweenTarget, fromVars: TweenVars, toVars: TweenVars, position?: Position): this;

    /**
     * **Deprecated method signature.** Use the `duration` property instead.
     *
     * ```js
     * tl.fromTo(".class", 1, {x: 0}, { x: 100 }, "+=1"); // adds the tween one second after the end of the timeline
     * ```
     *
     * @deprecated since version 3.0.0
     * @param {TweenTarget} targets
     * @param {number} duration - The duration parameter is deprecated. Use the `duration` property instead.
     * @param {TweenVars} fromVars
     * @param {TweenVars} toVars
     * @param {Position} [position]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/fromTo()
     */
    fromTo(targets: TweenTarget, duration: number, fromVars: TweenVars, toVars: TweenVars, position?: Position): this;
    
    /**
     * Returns the tween or timeline associated with the provided ID.
     *
     * ```js
     * tl.getById("myTween");
     * ```
     *
     * @param {string} id
     * @returns {Tween | Timeline} 
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/getById()
     */
    getById(id: string): Tween | Timeline;

    /**
     * Returns an array containing all the tweens and/or timelines nested in this timeline.
     *
     * ```js
     * tl.getChildren();
     * tl.getChildren(true, true, true, 0.5);
     * ```
     *
     * @param {boolean} [nested]
     * @param {boolean} [tweens]
     * @param {boolean} [timelines]
     * @param {number} [ignoreBeforeTime]
     * @returns {(Tween | Timeline)[]} Array of tweens and timelines
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/getChildren()
     */
    getChildren(nested?: boolean, tweens?: boolean, timelines?: boolean, ignoreBeforeTime?: number): (Tween | Timeline)[]; 

    /**
     * Returns the tweens of a particular object that are inside this timeline.
     *
     * ```js
     * tl.getTweensOf(".myClass");
     * tl.getTweensOf(myElem, true);
     * ```
     *
     * @param {TweenTarget} targets
     * @param {boolean} [onlyActive]
     * @returns {Tween[]} Array of tweens
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/getTweensOf()
     */
    getTweensOf(targets: TweenTarget, onlyActive?: boolean): Tween[];

    /**
     * Returns the next label in the timeline, optionally from the provided time.
     *
     * ```js
     * tl.nextLabel();
     * tl.nextLabel(2);
     * ```
     *
     * @param {number} [time]
     * @returns {string} The next label
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/nextLabel()
     */
    nextLabel(time?: number): string;

    /**
     * Returns the previous label in the timeline, optionally from the provided time.
     *
     * ```js
     * tl.previousLabel();
     * tl.previousLabel(2);
     * ```
     *
     * @param {number} [time]
     * @returns {string} The previous label
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/previousLabel()
     */
    previousLabel(time?: number): string;

    /**
     * Returns the most recently added child tween, timeline, or callback regardless of its position in the timeline.
     *
     * ```js
     * tl.recent();
     * ```
     *
     * @returns {Tween | Timeline | Function} The most recent tween, timeline, or callback
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/recent()
     */
    recent(): Tween | Timeline | Function;

    /**
     * Removes a tween, timeline, callback, label, or array of those values from the timeline.
     *
     * ```js
     * tl.remove(myTween);
     * tl.remove([myTween, mySubTimeline, "myLabel"]);
     * ```
     *
     * @param {TimelineChild} value
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/remove()
     */
    remove(value: TimelineChild): this;

    /**
     * Removes a label from the timeline and returns the time of that label.
     *
     * ```js
     * tl.removeLabel("myLabel"); // returns the label time like 1.0
     * ```
     *
     * @param {string} label
     * @returns {number} The time of the removed label
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/removeLabel()
     */
    removeLabel(label: string): number;

    /**
     * Removes pauses that were added to a timeline via its .addPause() method.
     *
     * ```js
     * tl.removePause(1); // returns the pause at time 1
     * ```
     *
     * @param {Position} position
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/removePause()
     */
    removePause(position: Position): this;

    /**
     * Sets properties of the target(s) to the properties specified at the time of the set call.
     *
     * ```js
     * tl.set(".class", {x: 100, y: 50, opacity: 0}, 1);
     * ```
     *
     * @param {TweenTarget} targets
     * @param {TweenVars} vars
     * @param {Position} [position]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/set()
     */
    set(targets: TweenTarget, vars: TweenVars, position?: Position): this;

    /**
     * Shifts the startTime of the timeline's children by a certain amount and optionally adjusts labels too.
     *
     * ```js
     * tl.shiftChildren(1); // shift the child tweens, timelines, and callbacks by 1 second
     * ```
     *
     * @param {number} amount
     * @param {boolean} [adjustLabels]
     * @param {number} [ignoreBeforeTime]
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/shiftChildren()
     */
    shiftChildren(amount: number, adjustLabels?: boolean, ignoreBeforeTime?: number): this;

    /**
     * **Deprecated method.** Use the `.from()` method instead.
     *
     * @deprecated since version 3.0.0
     * @param {TweenTarget} targets
     * @param {TweenVars} vars
     * @param {Position} position
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/from()
     */
    staggerFrom(targets: TweenTarget, vars: TweenVars, position?: Position): this;

     /**
      * **Deprecated method.** Use the `.from()` method instead.
      * 
      * @deprecated since version 3.0.0
      * @param {TweenTarget} targets
      * @param {number} duration - The duration parameter is deprecated. Use the `duration` property instead.
      * @param {Timeline} vars
      * @param {Position} position
      * @returns {Timeline} The timeline
      * @memberof Timeline
      * @link https://greensock.com/docs/v3/GSAP/Timeline/from()
      */
     staggerFrom(targets: TweenTarget, duration: number, vars: TweenVars, position?: Position): this;

     /**
     * **Deprecated method.** Use the `.fromTo()` method instead.
     *
     * @deprecated since version 3.0.0
     * @param {TweenTarget} targets
     * @param {TweenVars} vars
     * @param {Position} position
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/fromTo()
     */
    staggerFromTo(targets: TweenTarget, fromVars: TweenVars, toVars: TweenVars, position?: Position): this;

    /**
     * **Deprecated method.** Use the `.fromTo()` method instead.
     * 
     * @deprecated since version 3.0.0
     * @param {TweenTarget} targets
     * @param {number} duration - The duration parameter is deprecated. Use the `duration` property instead.
     * @param {Timeline} vars
     * @param {Position} position
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/fromTo()
     */
    staggerFromTo(targets: TweenTarget, duration: number, fromVars: TweenVars, toVars: TweenVars, position?: Position): this;

    /**
     * **Deprecated method.** Use the `.to()` method instead.
     *
     * @deprecated since version 3.0.0
     * @param {TweenTarget} targets
     * @param {TweenVars} vars
     * @param {Position} position
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/to()
     */
    staggerTo(targets: TweenTarget, vars: TweenVars, position?: Position): this;

    /**
     * **Deprecated method.** Use the `.to()` method instead.
     * 
     * @deprecated since version 3.0.0
     * @param {TweenTarget} targets
     * @param {number} duration - The duration parameter is deprecated. Use the `duration` property instead.
     * @param {Timeline} vars
     * @param {Position} position
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/to()
     */
    staggerTo(targets: TweenTarget, duration: number, vars: TweenVars, position?: Position): this;

    /**
     * Creates a tween going TO the given values.
     *
     * ```js
     * tl.to(".class", {x: 100}, 1);
     * ```
     *
     * @param {TweenTarget} targets
     * @param {TweenVars} vars
     * @param {Position} position
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/to()
     */
    to(targets: TweenTarget, vars: TweenVars, position?: Position): this;

    /**
     * **Deprecated method signature.** Use the `duration` property instead.
     * 
     * ```js
     * tl.to(".class", 1, {x: 100}, 1);
     * ```
     * @deprecated since version 3.0.0
     * @param {TweenTarget} targets
     * @param {number} duration - The duration parameter is deprecated. Use the `duration` property instead.
     * @param {Timeline} vars
     * @param {Position} position
     * @returns {Timeline} The timeline
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/to()
     */
    to(targets: TweenTarget, duration: number, vars: TweenVars, position?: Position): this;

    /**
     * Tween linearly from a particular time or label to another time or label and then stops. 
     *
     * ```js
     * tl.tweenFromTo("myLabel", 5); // tween from myLabel to the 5 second mark
     * ```
     *
     * @param {Position} fromPosition
     * @param {Position} toPosition
     * @param {TweenVars} [vars]
     * @returns {Tween} The tweenFromTo tween
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/tweenFromTo()
     */
    tweenFromTo(fromPosition: Position, toPosition: Position, vars?: TweenVars): Tween;

    /**
     * Tween linearly to a particular time and then stops. 
     *
     * ```js
     * tl.tweenTo("myLabel"); // tween to myLabel
     * ```
     *
     * @param {Position} position
     * @param {TweenVars} [vars]
     * @returns {Tween} The tweenTo tween
     * @memberof Timeline
     * @link https://greensock.com/docs/v3/GSAP/Timeline/tweenTo()
     */
    tweenTo(position: Position, vars?: TweenVars): Tween;
  }
}
