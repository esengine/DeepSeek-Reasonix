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

  const start = async () => {
    const topic = await waitFor(
      () => [...document.querySelectorAll(".project-tree__topic-main")]
        .find((element) => element.textContent?.includes("bench:selection-table")),
      "selection table topic",
    );
    topic.click();
    const target = await waitFor(
      () => [...document.querySelectorAll("strong")]
        .find((element) => element.textContent?.includes("SELECTION REPAINT TARGET")),
      "selection repaint target",
    );
    await waitFor(
      () => document.querySelector(".project-tree__topic--active .project-tree__topic-label")
        ?.textContent?.includes("bench:selection-table"),
      "active selection table topic",
    );
    await waitFor(() => !document.querySelector(".transcript-navigation-overlay"), "settled transcript navigation");
    target.scrollIntoView({ block: "center", inline: "nearest" });
    await document.fonts?.ready;
    await frame();
    await frame();
    await wait(200);

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
        table: rect(table.getBoundingClientRect()),
        row: rect(row.getBoundingClientRect()),
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
