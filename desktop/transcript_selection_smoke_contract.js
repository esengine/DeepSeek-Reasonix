(() => {
  const post = (payload) => window.chrome.webview.postMessage(JSON.stringify(payload));
  const wait = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
  const frame = () => new Promise((resolve) => requestAnimationFrame(resolve));
  const waitFor = async (read, label, timeout = 30000) => {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      const value = read();
      if (value) return value;
      await wait(50);
    }
    throw new Error(`timed out waiting for ${label}`);
  };
  const rect = (value) => value ? {
    left: value.left,
    top: value.top,
    right: value.right,
    bottom: value.bottom,
  } : null;
  const transcriptActionHosts = () => {
    const scoped = [...document.querySelectorAll('.transcript-selection-action[data-surface="transcript"]')];
    return scoped.length > 0 ? scoped : [...document.querySelectorAll("body > .transcript-selection-action")];
  };
  const selectionTableTopic = () => [...document.querySelectorAll(".project-tree__topic-main")]
    .find((element) => element.textContent?.includes("bench:selection-table"));
  const selectionTarget = () => [...document.querySelectorAll("strong")]
    .find((element) => element.textContent?.includes("SELECTION REPAINT TARGET"));
  const activateSelectionTableTopic = async (timeout = 30000) => {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      const topic = selectionTableTopic();
      if (topic?.closest(".project-tree__topic--active")) return topic;
      if (topic) {
        topic.click();
        // ProjectTree intentionally delays single-click opening by 200ms so a
        // double click can become rename. It can also render the topic before
        // its openRequest is ready, in which case the click is ignored. Wait
        // beyond that decision window, then retry only while still inactive.
        const attemptDeadline = Math.min(deadline, Date.now() + 750);
        while (Date.now() < attemptDeadline) {
          if (selectionTableTopic()?.closest(".project-tree__topic--active")) {
            return selectionTableTopic();
          }
          await wait(50);
        }
      } else {
        await wait(50);
      }
    }
    throw new Error("timed out activating selection table topic");
  };

  const settleSelectionTarget = async (initialTarget, timeout = 10000) => {
    const deadline = Date.now() + timeout;
    let target = initialTarget;
    let previous = null;
    let stableSamples = 0;
    while (Date.now() < deadline) {
      if (!target?.isConnected) target = selectionTarget();
      const transcript = document.querySelector(".transcript");
      const shell = document.querySelector(".transcript-shell");
      if (!(target instanceof HTMLElement) || !(transcript instanceof HTMLElement) || !(shell instanceof HTMLElement)) {
        await wait(50);
        continue;
      }
      target.scrollIntoView({ block: "center", inline: "nearest" });
      await frame();
      await frame();
      await wait(50);
      const targetRect = target.getBoundingClientRect();
      const shellRect = shell.getBoundingClientRect();
      const current = {
        top: targetRect.top,
        bottom: targetRect.bottom,
        scrollTop: transcript.scrollTop,
        scrollHeight: transcript.scrollHeight,
      };
      const insideShell = targetRect.top >= shellRect.top && targetRect.bottom <= shellRect.bottom;
      const geometryStable = previous && Object.keys(current)
        .every((key) => Math.abs(current[key] - previous[key]) <= 0.5);
      const geometryPending = transcript.querySelector("[data-transcript-geometry-pending], .reasoning--loading");
      stableSamples = insideShell && geometryStable && !geometryPending ? stableSamples + 1 : 0;
      if (stableSamples >= 4) return target;
      previous = current;
    }
    throw new Error("timed out settling selection repaint target");
  };

  const start = async () => {
    await activateSelectionTableTopic();
    let target = await waitFor(
      () => {
        const target = selectionTarget();
        if (target) return target;
        // The marker is in the fixture's final virtual row. WebView2 can
        // publish the active topic before Virtuoso performs its initial tail
        // landing, leaving that row unmounted indefinitely. Keep setup on the
        // physical bottom until the final row exists; all compositor captures
        // still begin only after the target is centered and settled below.
        const transcript = document.querySelector(".transcript");
        if (transcript instanceof HTMLElement) transcript.scrollTop = transcript.scrollHeight;
        return null;
      },
      "selection repaint target",
    );
    await waitFor(() => !document.querySelector(".transcript-navigation-overlay"), "settled transcript navigation");
    await document.fonts?.ready;
    target = await settleSelectionTarget(target);

    const transcript = document.querySelector(".transcript");
    const shell = document.querySelector(".transcript-shell");
    const table = target.closest("table");
    const row = target.closest("tr");
    const host = transcriptActionHosts()[0] ?? null;
    if (!transcript || !shell || !table || !row) throw new Error("selection fixture DOM is incomplete");

    const eventSamples = [];
    let lastToolbar = null;
    const geometry = (label) => {
      const currentHosts = transcriptActionHosts();
      const currentHost = currentHosts[0] ?? null;
      const currentTable = target.closest("table") ?? table;
      const currentRow = target.closest("tr") ?? row;
      const selection = document.getSelection();
      const selectionRects = selection?.rangeCount
        ? [...selection.getRangeAt(selection.rangeCount - 1).getClientRects()].map(rect)
        : [];
      const hostOpen = currentHost?.getAttribute("data-state") === "open";
      const currentToolbar = currentHost ? rect(currentHost.getBoundingClientRect()) : null;
      if (currentToolbar && currentToolbar.right > currentToolbar.left && currentToolbar.bottom > currentToolbar.top) {
        lastToolbar = currentToolbar;
      }
      return {
        label,
        timestamp: performance.now(),
        dpr: window.devicePixelRatio,
        viewport: { width: window.innerWidth, height: window.innerHeight },
        shell: rect(shell.getBoundingClientRect()),
        table: rect(currentTable.getBoundingClientRect()),
        row: rect(currentRow.getBoundingClientRect()),
        target: rect(target.getBoundingClientRect()),
        toolbar: hostOpen ? currentToolbar : lastToolbar,
        selectionRects,
        scrollTop: transcript.scrollTop,
        scrollHeight: transcript.scrollHeight,
        clientHeight: transcript.clientHeight,
        hostCount: currentHosts.length,
        hostStable: currentHost === host,
        hostState: currentHost?.getAttribute("data-state") ?? null,
      };
    };
    const recordEvent = (label) => {
      if (eventSamples.length < 256) eventSamples.push(geometry(label));
    };
    document.addEventListener("pointerdown", () => recordEvent("pointerdown"), true);
    document.addEventListener("pointerup", () => {
      recordEvent("pointerup");
      requestAnimationFrame(() => {
        recordEvent("pointerup-raf-1");
        requestAnimationFrame(() => recordEvent("pointerup-raf-2"));
      });
    }, true);
    document.addEventListener("selectionchange", () => recordEvent("selectionchange"));

    window.__reasonixSelectionSmoke = {
      async snapshot(label, frames = 0, delay = 0) {
        for (let index = 0; index < frames; index += 1) await frame();
        if (delay > 0) await wait(delay);
        post({ type: "snapshot", geometry: geometry(label), eventSamples });
      },
      async reset(iteration) {
        document.getSelection()?.removeAllRanges();
        document.dispatchEvent(new Event("selectionchange"));
        await frame();
        await frame();
        eventSamples.length = 0;
        post({ type: "reset", iteration, geometry: geometry(`reset-${iteration}`) });
      },
      async settle() {
        target = await settleSelectionTarget(target);
        const targetRect = target.getBoundingClientRect();
        post({
          type: "settled",
          point: {
            x: Math.round(targetRect.left + targetRect.width / 2),
            y: Math.round(targetRect.top + targetRect.height / 2),
          },
          geometry: geometry("settled"),
        });
      },
    };

    const targetRect = target.getBoundingClientRect();
    post({
      type: "ready",
      point: {
        x: Math.round(targetRect.left + targetRect.width / 2),
        y: Math.round(targetRect.top + targetRect.height / 2),
      },
      geometry: geometry("ready"),
      platform: document.documentElement.dataset.platform ?? "",
    });
  };

  start().catch((error) => post({ type: "error", message: error?.stack || String(error) }));
})();
