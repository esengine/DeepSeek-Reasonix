(() => {
  "use strict";

  const post = (payload) => {
    const message = JSON.stringify(payload);
    if (window.chrome?.webview?.postMessage) {
      window.chrome.webview.postMessage(message);
      return;
    }
    if (window.webkit?.messageHandlers?.reasonixNativeSmoke) {
      window.webkit.messageHandlers.reasonixNativeSmoke.postMessage(message);
    }
  };

  const waitFor = (predicate, timeout = 30000) => new Promise((resolve, reject) => {
    const startedAt = performance.now();
    const sample = () => {
      const value = predicate();
      if (value) {
        resolve(value);
        return;
      }
      if (performance.now() - startedAt >= timeout) {
        reject(new Error("native transcript fixture timed out"));
        return;
      }
      requestAnimationFrame(sample);
    };
    sample();
  });

  const state = {
    transcript: null,
    frames: [],
    active: false,
    growthTimer: 0,
    growthTicks: 0,
    growthSurface: null,
    initialDistance: 0,
    phase: "waiting-topic",
    writes: [],
    wheelEvents: 0,
    lastWheelAt: Number.NEGATIVE_INFINITY,
    wheelDelta: 0,
    wheelInsideTranscript: 0,
    wheelMaxDelta: 0,
    programmaticReaderWrites: 0,
    staleGenerationWrites: 0,
    reachableTailResidual: 0,
    composer: {
      enabled: false,
      active: false,
      input: null,
      initialValue: "",
      baseline: null,
      samples: [],
      observer: null,
      onScroll: null,
      resolve: null,
      reject: null,
      result: null,
    },
  };
  window.__reasonixNativeTranscriptSmokeState = state;
  window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
    const previousWrite = state.writes.at(-1);
    state.writes.push(write);
    if (state.writes.length > 80) state.writes.shift();
    const accepted = !write.rejectedReason && (write.outcome === "accepted" || write.outcome === "native-clamp");
    const surfaceGeneration = Number.parseInt(state.transcript?.dataset.transcriptGeneration ?? "0", 10);
    if (accepted && state.active && performance.now() - state.lastWheelAt <= 250) {
      state.programmaticReaderWrites += 1;
    }
    if (accepted && Number.isFinite(surfaceGeneration) && surfaceGeneration > 0 && write.generation !== surfaceGeneration) {
      state.staleGenerationWrites += 1;
    }
    if (write.owner !== "tail-follow" || !accepted || !Number.isFinite(write.acceptedOffset)) return;
    const repeatedClamp = previousWrite?.owner === "tail-follow"
      && !previousWrite.rejectedReason
      && Math.abs(previousWrite.acceptedOffset - write.acceptedOffset) <= 1
      && Math.abs(previousWrite.scrollHeight - write.scrollHeight) <= 1
      && Math.abs(previousWrite.clientHeight - write.clientHeight) <= 1
      && Math.abs(previousWrite.scrollTop - write.scrollTop) <= 0.5;
    const reportedResidual = write.scrollHeight - write.clientHeight - write.scrollTop;
    if (repeatedClamp && reportedResidual > 4 && reportedResidual <= 64) {
      state.reachableTailResidual = reportedResidual;
    }
    requestAnimationFrame(() => {
      const element = state.transcript;
      if (!(element instanceof HTMLElement)) return;
      const theoreticalTop = Math.max(0, element.scrollHeight - element.clientHeight);
      const residual = theoreticalTop - element.scrollTop;
      const sameGeometry = Math.abs(element.scrollHeight - write.scrollHeight) <= 1
        && Math.abs(element.clientHeight - write.clientHeight) <= 1;
      if (
        sameGeometry
        && Math.abs(write.acceptedOffset - write.scrollTop) <= 1
        && Math.abs(element.scrollTop - write.scrollTop) <= 0.5
        && residual > 4
        && residual <= 64
      ) {
        state.reachableTailResidual = residual;
      }
    });
  };
  window.addEventListener("wheel", (event) => {
    if (!state.active) return;
    state.lastWheelAt = performance.now();
    state.wheelEvents += 1;
    state.wheelDelta += event.deltaY;
    if (event.target instanceof Node && state.transcript?.contains(event.target)) state.wheelInsideTranscript += 1;
    state.wheelMaxDelta = Math.max(state.wheelMaxDelta, Math.abs(event.deltaY));
  }, { capture: true, passive: true });

  // WebView2 can expose a stable native scrollHeight with a small terminal
  // range that scrollTop cannot reach. Accept it only after repeated writer
  // observations prove the clamp at unchanged geometry.
  const tailDistance = (element) => {
    const theoreticalTop = Math.max(0, element.scrollHeight - element.clientHeight);
    const observedTop = theoreticalTop - state.reachableTailResidual;
    if (element.scrollTop <= observedTop + 4) return observedTop - element.scrollTop;
    state.reachableTailResidual = 0;
    return theoreticalTop - element.scrollTop;
  };

  const visibleRows = (element) => {
    const viewport = element.getBoundingClientRect();
    return [...element.querySelectorAll(".transcript__row")].filter((row) => {
      const rect = row.getBoundingClientRect();
      return rect.bottom > viewport.top && rect.top < viewport.bottom;
    });
  };

  const outerReaderPoint = (element) => {
    const viewport = element.getBoundingClientRect();
    for (const row of visibleRows(element)) {
      const rect = row.getBoundingClientRect();
      const visibleTop = Math.max(viewport.top, rect.top);
      const visibleBottom = Math.min(viewport.bottom, rect.bottom);
      if (visibleBottom - visibleTop < 2) continue;
      const y = visibleTop + (visibleBottom - visibleTop) / 2;
      // Match the browser gate: row padding is owned by Transcript, while
      // code/table descendants may own their own nested scrollports.
      for (const x of [rect.left + 16, rect.right - 16]) {
        if (document.elementFromPoint(x, y) === row) {
          return { x: Math.round(x), y: Math.round(y) };
        }
      }
    }
    return null;
  };

  const scheduleSample = () => {
    requestAnimationFrame(() => window.setTimeout(sample, 0));
  };

  const sample = () => {
    if (!state.active || !(state.transcript instanceof HTMLElement)) return;
    const element = state.transcript;
    const viewport = element.getBoundingClientRect();
    const rows = visibleRows(element);
    const projection = element.querySelector(".transcript__projection");
    const mounted = [...element.querySelectorAll(".transcript__window-item[data-index]")]
      .map((block) => Number.parseInt(block.dataset.index ?? "", 10))
      .filter(Number.isFinite);
    const mountedCount = Number.parseInt(
      projection?.getAttribute("data-transcript-mounted-blocks") ?? "0",
      10,
    );
    const visible = [...element.querySelectorAll("[data-transcript-block-key]")]
      .filter((block) => {
        const rect = block.getBoundingClientRect();
        return rect.bottom > viewport.top && rect.top < viewport.bottom;
      }).map((block) => {
        const item = block.closest(".transcript__window-item[data-index]");
        return {
          index: block.dataset.transcriptBlockKey ?? "",
          itemIndex: Number.parseInt(item?.dataset.index ?? "", 10),
          top: block.getBoundingClientRect().top - viewport.top,
        };
      });
    const blankGeometry = rows.length === 0 ? {
      viewportTop: viewport.top,
      viewportBottom: viewport.bottom,
      coldTop: element.querySelector(".transcript__window")?.getBoundingClientRect().top ?? null,
      coldBottom: element.querySelector(".transcript__window")?.getBoundingClientRect().bottom ?? null,
      items: [...element.querySelectorAll(".transcript__window-item[data-index]")].map((item) => {
        const rect = item.getBoundingClientRect();
        const block = item;
        const blockRect = block?.getBoundingClientRect();
        return {
          index: Number.parseInt(item.dataset.index ?? "", 10),
          modelTop: Number.parseFloat(item.style.top || "0"),
          top: rect.top - viewport.top,
          bottom: rect.bottom - viewport.top,
          height: rect.height,
          blockTop: blockRect ? blockRect.top - viewport.top : null,
          blockBottom: blockRect ? blockRect.bottom - viewport.top : null,
          blockHeight: blockRect?.height ?? null,
        };
      }),
    } : undefined;
    state.frames.push({
      top: element.scrollTop,
      height: element.scrollHeight,
      occupied: rows.length > 0,
      mode: element.dataset.scrollMode ?? "missing",
      readerIntent: element.dataset.transcriptIntent ?? "missing",
      rangeSource: projection?.getAttribute("data-transcript-range-source") ?? "none",
      mountedFirst: mounted.length > 0 ? Math.min(...mounted) : null,
      mountedLast: mounted.length > 0 ? Math.max(...mounted) : null,
      mountedCount,
      visible,
      blankGeometry,
    });
    // ResizeObserver is delivered after rAF layout work but before paint.
    // Sample from the following task so the contract measures the geometry a
    // native WebView actually painted, not an intermediate pre-observer state.
    scheduleSample();
  };

  const growFooter = () => {
    if (!(state.growthSurface instanceof HTMLElement) || state.growthTicks >= 64) return;
    state.growthSurface.style.height = `${Number.parseFloat(state.growthSurface.style.height || "0") + 2}px`;
    state.growthTicks += 1;
  };

  const waitForStableViewport = (element, requiredFrames = 8, timeout = 10000) => new Promise((resolve, reject) => {
    const startedAt = performance.now();
    let previous = null;
    let stableFrames = 0;
    const recent = [];
    const sample = () => {
      const current = { top: element.scrollTop, height: element.scrollHeight, clientHeight: element.clientHeight };
      recent.push(current);
      if (recent.length > 12) recent.shift();
      const stable = previous
        && Math.abs(current.top - previous.top) <= 1
        && Math.abs(current.height - previous.height) <= 1
        && current.clientHeight === previous.clientHeight
        && visibleRows(element).length > 0;
      stableFrames = stable ? stableFrames + 1 : 0;
      previous = current;
      if (stableFrames >= requiredFrames) {
        resolve();
        return;
      }
      if (performance.now() - startedAt >= timeout) {
        reject(new Error(`native transcript viewport did not stabilize: ${JSON.stringify({
          stableFrames,
          recent,
          visibleRows: visibleRows(element).length,
          mode: element.dataset.scrollMode ?? "missing",
          rows: element.dataset.transcriptRowCount ?? "0",
        })}`));
        return;
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });

  const waitForStableTail = (element, requiredFrames = 2, timeout = 5000) => new Promise((resolve) => {
    const startedAt = performance.now();
    let stableFrames = 0;
    const sample = () => {
      const stable = element.dataset.scrollMode === "tail-follow"
        && tailDistance(element) <= 4
        && visibleRows(element).length > 0;
      stableFrames = stable ? stableFrames + 1 : 0;
      if (stableFrames >= requiredFrames || performance.now() - startedAt >= timeout) {
        resolve();
        return;
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });

  const settleFrames = (count = 4) => new Promise((resolve) => {
    const settle = () => {
      count -= 1;
      if (count <= 0) resolve();
      else requestAnimationFrame(settle);
    };
    requestAnimationFrame(settle);
  });

  const setNativeTextareaValue = (input, value) => {
    const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
    setter?.call(input, value);
    input.dispatchEvent(new InputEvent("input", {
      bubbles: true,
      data: value,
      inputType: "insertText",
    }));
    input.setSelectionRange(value.length, value.length);
  };

  const sampleComposer = (source) => {
    const composer = state.composer;
    const element = state.transcript;
    if (!composer.active || !(element instanceof HTMLElement)) return;
    composer.samples.push({
      source,
      top: element.scrollTop,
      height: element.scrollHeight,
      clientHeight: element.clientHeight,
      distance: tailDistance(element),
    });
  };

  const prepareNativeComposer = async (element) => {
    if (new URLSearchParams(window.location.search).get("nativeComposer") !== "1") return;
    state.transcript = element;
    state.phase = "preparing-native-composer";
    const input = await waitFor(() => document.querySelector(
      "textarea.composer__input:not(.composer__input--measure)",
    ), 10000);
    const initialValue = "existing first line\nexisting second line";
    setNativeTextareaValue(input, initialValue);
    input.focus();
    await waitFor(() => input.value === initialValue && input.getBoundingClientRect().height > 32, 10000);
    await document.fonts.ready;

    state.composer.input = input;
    state.composer.initialValue = initialValue;
  };

  const runNativeComposer = async (element) => {
    if (new URLSearchParams(window.location.search).get("nativeComposer") !== "1") return;
    state.transcript = element;
    state.phase = "waiting-native-composer";
    const composer = state.composer;
    const input = composer.input;
    if (!(input instanceof HTMLTextAreaElement) || input.value !== composer.initialValue) {
      throw new Error("native composer draft was not prepared before loading history");
    }
    input.focus();
    await waitForStableTail(element, 2, 10000);
    await waitForStableViewport(element, 12, 15000);
    const initialDistance = tailDistance(element);
    if (element.dataset.scrollMode !== "tail-follow" || initialDistance > 4) {
      throw new Error(`native composer did not start at a stable tail: ${describeTranscriptState(element)}`);
    }

    composer.enabled = true;
    composer.active = true;
    composer.input = input;
    composer.samples = [];
    composer.result = null;
    composer.baseline = {
      top: element.scrollTop,
      height: element.scrollHeight,
      clientHeight: element.clientHeight,
      inputHeight: input.getBoundingClientRect().height,
      initialValue: composer.initialValue,
    };
    composer.onScroll = () => sampleComposer("scroll");
    element.addEventListener("scroll", composer.onScroll, { passive: true });
    composer.observer = new ResizeObserver(() => sampleComposer("resize"));
    composer.observer.observe(element);
    sampleComposer("baseline");
    const sampleFrame = () => {
      if (!composer.active) return;
      sampleComposer("frame");
      requestAnimationFrame(sampleFrame);
    };
    requestAnimationFrame(sampleFrame);

    const completion = new Promise((resolve, reject) => {
      composer.resolve = resolve;
      composer.reject = reject;
    });
    const rect = input.getBoundingClientRect();
    state.phase = "native-composer-ready";
    post({
      type: "composer-ready",
      point: { x: Math.round(rect.left + rect.width / 2), y: Math.round(rect.top + rect.height / 2) },
    });
    await completion;
    state.phase = "native-composer-complete";
  };

  const finishComposer = async () => {
    const composer = state.composer;
    const element = state.transcript;
    if (!composer.active || !(element instanceof HTMLElement) || !(composer.input instanceof HTMLTextAreaElement)) {
      return composer.result;
    }
    try {
      await settleFrames(8);
      sampleComposer("final");
      composer.active = false;
      composer.observer?.disconnect();
      if (composer.onScroll) element.removeEventListener("scroll", composer.onScroll);
      const baseline = composer.baseline;
      const minTop = Math.min(baseline.top, ...composer.samples.map((sample) => sample.top));
      const geometryChanges = composer.samples.filter((sample) => (
        Math.abs(sample.height - baseline.height) > 0.5 || sample.clientHeight !== baseline.clientHeight
      )).length;
      const finalDistance = tailDistance(element);
      const finalGeometry = {
        top: element.scrollTop,
        height: element.scrollHeight,
        clientHeight: element.clientHeight,
      };
      const changedSamples = composer.samples.filter((sample) => (
        Math.abs(sample.height - baseline.height) > 0.5 || sample.clientHeight !== baseline.clientHeight
      ));
      composer.result = {
        passed: composer.samples.length >= 8
          && baseline.inputHeight > 32
          && composer.input.value === baseline.initialValue
          && baseline.top - minTop <= 1
          && geometryChanges === 0
          && element.dataset.scrollMode === "tail-follow"
          && finalDistance <= 4,
        samples: composer.samples.length,
        maxReverse: baseline.top - minTop,
        geometryChanges,
        finalDistance,
        inputHeight: baseline.inputHeight,
        finalInputHeight: composer.input.getBoundingClientRect().height,
        finalValueMatches: composer.input.value === baseline.initialValue,
        baseline: {
          top: baseline.top,
          height: baseline.height,
          clientHeight: baseline.clientHeight,
        },
        finalGeometry,
        changedSamples: changedSamples.length > 0
          ? [changedSamples[0], changedSamples[changedSamples.length - 1]]
          : [],
      };
      if (!composer.result.passed) {
        throw new Error(`native composer stability failed: ${JSON.stringify(composer.result)}`);
      }
      setNativeTextareaValue(composer.input, "");
      await waitForStableTail(element, 2, 10000);
      await waitForStableViewport(element, 2, 10000);
      composer.resolve?.();
    } catch (error) {
      composer.reject?.(error);
    }
    return composer.result;
  };

  // A bare "timed out" cannot distinguish a missing rail, a stuck jump mask,
  // or a jump-bottom button that never appears on a hosted runner, so every
  // internal gate reports the live transcript state when it fails.
  const describeTranscriptState = (element) => {
    const overlay = document.querySelector(".transcript-navigation-overlay");
    const loadedTurns = [...document.querySelectorAll(".jump-item[data-loaded='true']")]
      .map((marker) => Number(marker.getAttribute("data-turn")))
      .filter(Number.isFinite);
    return JSON.stringify({
      rows: Number.parseInt(element?.dataset.transcriptRowCount ?? "0", 10),
      markers: document.querySelectorAll(".jump-item").length,
      earliestTurn: loadedTurns.length > 0 ? Math.min(...loadedTurns) : null,
      overlayPhase: overlay ? overlay.getAttribute("data-question-jump-phase") ?? "present" : null,
      shellBusy: document.querySelector(".transcript-shell")?.getAttribute("aria-busy") ?? null,
      mode: element?.dataset.scrollMode ?? "missing",
      top: element ? Math.round(element.scrollTop) : null,
      height: element?.scrollHeight ?? null,
      clientHeight: element?.clientHeight ?? null,
      offsetHeight: element?.offsetHeight ?? null,
      rectHeight: element ? element.getBoundingClientRect().height : null,
      reachableTailResidual: state.reachableTailResidual,
      bottomDistance: element
        ? Math.round(tailDistance(element))
        : null,
      recentWrites: state.writes.slice(-8),
    });
  };

  const loadHistoryRows = async (element, minimumRows) => {
    const rowCount = () => Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10);
    if (rowCount() >= minimumRows) return;
    const earliestLoadedTurn = () => {
      const turns = [...document.querySelectorAll(".jump-item[data-loaded='true']")]
        .map((marker) => Number(marker.getAttribute("data-turn")))
        .filter(Number.isFinite);
      return turns.length > 0 ? Math.min(...turns) : null;
    };
    const rail = await waitFor(() => document.querySelector(".jump-scroll"), 10000)
      .catch(() => {
        throw new Error(`native transcript fixture question rail unavailable: ${describeTranscriptState(element)}`);
      });
    // Target progressively earlier questions through the real navigation UI.
    // A midpoint request usually supplies the 400+ variable-height rows the
    // smoke needs without forcing every archived turn into its first geometry
    // pass at once.
    for (const ratio of [0.65, 0.5, 0.35, 0.2, 0]) {
      if (rowCount() >= minimumRows) return;
      const beforeRows = rowCount();
      const beforeTurn = earliestLoadedTurn();
      const beforeJumpTransaction = Math.max(0, ...state.writes
        .filter((write) => write.owner === "question-jump")
        .map((write) => Number(write.transactionId ?? write.transaction ?? 0)));
      const rect = rail.getBoundingClientRect();
      rail.dispatchEvent(new MouseEvent("mousedown", {
        button: 0,
        clientX: rect.left + rect.width / 2,
        clientY: rect.top + rect.height * ratio,
        bubbles: true,
        cancelable: true,
      }));
      // Data may commit before React paints the loading mask, so absence of
      // the mask is not a completion signal. Wait for the kernel's physical
      // landing transaction, then require the UI mask to be gone.
      await waitFor(() => state.writes.some((write) => (
        write.owner === "question-jump"
          && Number(write.transactionId ?? write.transaction ?? 0) > beforeJumpTransaction
          && ["accepted", "native-clamp", "no-op"].includes(write.outcome)
      )), 30000)
        .catch(() => {
          throw new Error(`native transcript fixture question jump did not land at ratio ${ratio}: ${describeTranscriptState(element)}`);
        });
      await waitFor(() => !document.querySelector(".transcript-navigation-overlay"), 15000)
        .catch(() => {
          throw new Error(`native transcript fixture question jump surface stuck at ratio ${ratio}: ${describeTranscriptState(element)}`);
        });
      // A fixed-size history window keeps the mounted row count constant when
      // an earlier page arrives, and clicking an already-loaded turn changes
      // nothing. Accept an earlier first loaded turn or genuine row growth; a
      // no-op click falls through to the next, earlier ratio instead of
      // stalling the fixture on one signal.
      await waitFor(() => {
        const earliest = earliestLoadedTurn();
        return rowCount() > beforeRows
          || (earliest !== null && (beforeTurn === null || earliest < beforeTurn));
      }, 8000).catch(() => {});
    }
    if (rowCount() >= minimumRows) return;
    throw new Error(`native transcript fixture could not load ${minimumRows} rows (rows=${rowCount()} earliestTurn=${earliestLoadedTurn()})`);
  };

  const start = async () => {
    const topic = await waitFor(() => [...document.querySelectorAll(".project-tree__topic-main")]
      .find((candidate) => candidate.textContent?.includes("bench:windowed-1000t")));
    topic.click();
    state.phase = "waiting-topic-selection";
    await waitFor(() => (
      document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:windowed-1000t")
        && document.querySelector(".transcript")?.textContent?.includes("Windowed turn 1000")
    ), 30000);
    state.phase = "waiting-navigation-surface";
    await waitFor(() => !document.querySelector(".transcript-navigation-overlay"), 15000);
    const element = await waitFor(() => {
      const candidate = document.querySelector(".transcript");
      const rows = Number.parseInt(candidate?.dataset.transcriptRowCount ?? "0", 10);
      return candidate instanceof HTMLElement && rows > 0 && candidate.scrollHeight > candidate.clientHeight
        ? candidate
        : null;
    });
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    state.phase = "waiting-initial-tail";
    await waitForStableTail(element, 2, 10000);
    state.phase = "waiting-initial-stability";
    // WebView2 can revisit the first block estimates while pending Markdown
    // resolves. Two painted stable frames are sufficient before the
    // idempotent history-navigation setup; the loaded 400+ row surface still
    // has to satisfy the stricter tail and geometry gates below before native
    // sampling starts.
    await waitForStableViewport(element, 2, 15000);
    await prepareNativeComposer(element);
    state.phase = "loading-targeted-history";
    await loadHistoryRows(element, 400);
    // A real history jump owns manual-reader mode and must expose the product's
    // jump-bottom control. When the initial window already has enough rows,
    // the control is correctly absent and the product must preserve its
    // physical tail without a fixture-owned scroll write.
    let jumpBottom = document.querySelector(".transcript__jump-bottom:not([hidden])");
    if (!jumpBottom && element.dataset.scrollMode !== "tail-follow") {
      jumpBottom = await waitFor(() => document.querySelector(".transcript__jump-bottom:not([hidden])"), 10000)
        .catch(() => null);
    }
    if (jumpBottom instanceof HTMLElement) {
      jumpBottom.click();
    } else if (element.dataset.scrollMode !== "tail-follow") {
      throw new Error(`native transcript fixture left manual-reader mode without a jump-bottom control: ${describeTranscriptState(element)}`);
    }
    state.phase = "waiting-loaded-tail";
    await waitForStableTail(element, 2, 10000);
    if (element.dataset.scrollMode !== "tail-follow"
      || tailDistance(element) > 4) {
      throw new Error(`native transcript fixture could not establish the loaded tail: ${describeTranscriptState(element)}`);
    }
    // The initial history page can keep settling block estimates after a
    // couple of visually stable frames on a slower hosted WebView2. Run the
    // composer regression only after the long loaded surface has held both
    // its tail and geometry across a stricter painted-frame window, so the
    // samples isolate keyboard-driven layout from first-load measurement.
    await waitForStableViewport(element, 12, 15000);
    await runNativeComposer(element);
    // Position through the product's indexed question navigator so every
    // physical write remains owned by TranscriptViewportWriter.
    const rail = await waitFor(() => document.querySelector(".jump-scroll"), 10000);
    state.phase = "waiting-reader-stability";
    for (const ratio of [0.35, 0.2, 0]) {
      const before = element.scrollTop;
      const rect = rail.getBoundingClientRect();
      rail.dispatchEvent(new MouseEvent("mousedown", {
        button: 0,
        clientX: rect.left + rect.width / 2,
        clientY: rect.top + rect.height * ratio,
        bubbles: true,
        cancelable: true,
      }));
      await waitFor(() => (
        Math.abs(element.scrollTop - before) > element.clientHeight / 2
          || document.querySelector(".transcript-navigation-overlay")
      ), 10000);
      await waitFor(() => !document.querySelector(".transcript-navigation-overlay"), 15000);
      await waitForStableViewport(element);
      state.initialDistance = tailDistance(element);
      if (state.initialDistance >= element.clientHeight * 2) break;
    }
    if (state.initialDistance < element.clientHeight * 2) {
      throw new Error(`native transcript fixture did not establish a deep reader start (${state.initialDistance}px)`);
    }
    // The navigator is programmatic setup only. Claim manual reader ownership
    // before the host begins the platform-native downward traversal.
    element.dispatchEvent(new WheelEvent("wheel", { deltaY: -1, bubbles: true, cancelable: true }));
    await waitFor(() => element.dataset.scrollMode === "manual", 5000);
    state.phase = "waiting-reader-geometry";
    // Establish the baseline only after the turn window is populated and
    // bounded. Pending Markdown remains part of the streamed test itself.
    await waitFor(() => (
      document.querySelector("[data-transcript-render-mode='windowed']")
        && Number(document.querySelector(".transcript__projection")?.getAttribute("data-transcript-mounted-blocks")) > 0
        && Number(document.querySelector(".transcript__projection")?.getAttribute("data-transcript-mounted-blocks")) <= 40
    ), 30000);
    await waitForStableViewport(element, 8, 15000);
    const residentTail = element.querySelector(".transcript__resident-tail");
    if (!(residentTail instanceof HTMLElement)) throw new Error("native transcript resident tail is unavailable");
    const growthSurface = document.createElement("div");
    growthSurface.setAttribute("aria-hidden", "true");
    growthSurface.style.cssText = "height:0;width:100%;pointer-events:none;";
    residentTail.append(growthSurface);
    state.transcript = element;
    state.frames = [];
    state.active = true;
    state.lastWheelAt = Number.NEGATIVE_INFINITY;
    state.growthTicks = 0;
    state.growthSurface = growthSurface;
    state.wheelEvents = 0;
    state.wheelDelta = 0;
    state.wheelInsideTranscript = 0;
    state.wheelMaxDelta = 0;
    state.programmaticReaderWrites = 0;
    state.staleGenerationWrites = 0;
    state.phase = "ready";
    state.growthTimer = window.setInterval(growFooter, 16);
    scheduleSample();
    const point = outerReaderPoint(element);
    if (!point) throw new Error("native transcript outer reader target is unavailable");
    post({
      type: "ready",
      rows: Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10),
      top: element.scrollTop,
      point,
    });
  };

  const finish = async () => {
    window.clearInterval(state.growthTimer);
    const element = state.transcript;
    if (element instanceof HTMLElement) await waitForStableTail(element);
    state.active = false;
    const frames = state.frames;
    let maxReverse = 0;
    let rawMaxReverse = 0;
    let worstReverse = null;
    for (let index = 1; index < frames.length; index += 1) {
      const previous = frames[index - 1];
      const current = frames[index];
      const rawReverse = previous.top - current.top;
      rawMaxReverse = Math.max(rawMaxReverse, rawReverse);
      const currentTops = new Map(current.visible.map((row) => [row.index, row.top]));
      const visibleDeltas = previous.visible
        .filter((row) => row.index && currentTops.has(row.index))
        .map((row) => currentTops.get(row.index) - row.top)
        .sort((left, right) => left - right);
      // scrollTop can decrease when a measured extent above the viewport
      // contracts even though the same rows remain visually stationary. The
      // user-visible reverse displacement is the median screen movement of
      // rows painted in both adjacent frames; raw scrollTop remains attached
      // to the failure record for range diagnostics.
      const reverse = visibleDeltas.length > 0
        ? visibleDeltas[Math.floor(visibleDeltas.length / 2)]
        : rawReverse;
      if (reverse > maxReverse) {
        maxReverse = reverse;
        worstReverse = { previous, current, rawReverse, commonRows: visibleDeltas.length };
      }
    }
    const firstTop = frames[0]?.top ?? 0;
    const lastTop = frames.at(-1)?.top ?? firstTop;
    const blankFrames = frames.filter((frame) => !frame.occupied);
    const distance = element instanceof HTMLElement
      ? tailDistance(element)
      : Number.POSITIVE_INFINITY;
    const viewportRect = element instanceof HTMLElement ? element.getBoundingClientRect() : null;
    const footerRect = state.growthSurface?.parentElement?.getBoundingClientRect();
    const initialScrollHeight = frames[0]?.height ?? element?.scrollHeight ?? 0;
    const minScrollHeight = Math.min(initialScrollHeight, ...frames.map((frame) => frame.height));
    const maxScrollHeight = Math.max(initialScrollHeight, ...frames.map((frame) => frame.height));
    const finalScrollHeight = element?.scrollHeight ?? frames.at(-1)?.height ?? 0;
    const collapseTolerance = Math.max(96, (element?.clientHeight ?? 0) * 0.5);
    const mountedCoverage = frames.length > 0 ? (frames.length - blankFrames.length) / frames.length : 0;
    const result = {
      type: "result",
      passed: frames.length >= 20
        && lastTop > firstTop + 96
        && maxReverse <= 4
        && frames.every((frame) => frame.occupied)
        && state.initialDistance >= (element?.clientHeight ?? Number.POSITIVE_INFINITY) * 2
        && distance <= 4
        && element?.dataset.scrollMode === "tail-follow"
        && frames.every((frame) => frame.mountedCount <= 40)
        && state.programmaticReaderWrites === 0
        && state.staleGenerationWrites === 0
        && finalScrollHeight >= maxScrollHeight - collapseTolerance
        && (!state.composer.enabled || state.composer.result?.passed === true),
      rows: Number.parseInt(element?.dataset.transcriptRowCount ?? "0", 10),
      frames: frames.length,
      firstTop,
      lastTop,
      initialDistance: state.initialDistance,
      maxReverse,
      rawMaxReverse,
      worstReverse,
      occupied: frames.every((frame) => frame.occupied),
      blankFrames: blankFrames.length,
      firstBlank: blankFrames[0] ?? null,
      distance,
      mode: element?.dataset.scrollMode ?? "missing",
      growthTicks: state.growthTicks,
      initialScrollHeight,
      minScrollHeight,
      maxScrollHeight,
      finalScrollHeight,
      initialScrollTop: firstTop,
      finalScrollTop: lastTop,
      totalFrames: frames.length,
      mountedCoverage,
      finalBottomDistance: distance,
      finalMode: element?.dataset.scrollMode ?? "missing",
      deliveredNativeEvents: state.wheelEvents,
      wheelEvents: state.wheelEvents,
      wheelDelta: state.wheelDelta,
      wheelInsideTranscript: state.wheelInsideTranscript,
      wheelMaxDelta: state.wheelMaxDelta,
      programmaticReaderWrites: state.programmaticReaderWrites,
      staleGenerationWrites: state.staleGenerationWrites,
      paddingBottom: element instanceof HTMLElement ? Number.parseFloat(getComputedStyle(element).paddingBottom) : null,
      footerBottomDistance: viewportRect && footerRect ? footerRect.bottom - viewportRect.bottom : null,
      composerEnabled: state.composer.enabled,
      composerPassed: state.composer.result?.passed ?? false,
      composerSamples: state.composer.result?.samples ?? 0,
      composerMaxReverse: state.composer.result?.maxReverse ?? 0,
      composerGeometryChanges: state.composer.result?.geometryChanges ?? 0,
      composerFinalDistance: state.composer.result?.finalDistance ?? 0,
      composerInputHeight: state.composer.result?.inputHeight ?? 0,
      composerFinalValueMatches: state.composer.result?.finalValueMatches ?? false,
      writes: state.writes.slice(-20),
    };
    post(result);
    return result;
  };

  const finishMicro = () => {
    window.clearInterval(state.growthTimer);
    state.active = false;
    const element = state.transcript;
    const frames = state.frames;
    const blankFrames = frames.filter((frame) => !frame.occupied);
    const heights = frames.map((frame) => frame.height);
    const result = {
      type: "result",
      passed: state.wheelEvents > 0 && frames.length > 0,
      rows: Number.parseInt(element?.dataset.transcriptRowCount ?? "0", 10),
      frames: frames.length,
      totalFrames: frames.length,
      firstTop: frames[0]?.top ?? 0,
      lastTop: frames.at(-1)?.top ?? 0,
      initialScrollTop: frames[0]?.top ?? 0,
      finalScrollTop: frames.at(-1)?.top ?? 0,
      initialScrollHeight: heights[0] ?? element?.scrollHeight ?? 0,
      minScrollHeight: heights.length > 0 ? Math.min(...heights) : element?.scrollHeight ?? 0,
      maxScrollHeight: heights.length > 0 ? Math.max(...heights) : element?.scrollHeight ?? 0,
      finalScrollHeight: element?.scrollHeight ?? heights.at(-1) ?? 0,
      blankFrames: blankFrames.length,
      mountedCoverage: frames.length > 0 ? (frames.length - blankFrames.length) / frames.length : 0,
      finalBottomDistance: element instanceof HTMLElement ? tailDistance(element) : 0,
      finalMode: element?.dataset.scrollMode ?? "missing",
      deliveredNativeEvents: state.wheelEvents,
      mode: element?.dataset.scrollMode ?? "missing",
      occupied: blankFrames.length === 0,
    };
    post(result);
    return result;
  };

  const reportTail = () => {
    const element = state.transcript;
    post({
      type: "tail-status",
      distance: element instanceof HTMLElement ? tailDistance(element) : Number.MAX_SAFE_INTEGER,
      mode: element?.dataset.scrollMode ?? "missing",
    });
  };

  window.__reasonixNativeTranscriptSmoke = { start, finish, finishMicro, finishComposer, reportTail };
  start().catch((error) => {
    const message = String(error?.message ?? error);
    post({ type: "error", message: `${message} (${state.phase})`, phase: state.phase });
  });
})();
