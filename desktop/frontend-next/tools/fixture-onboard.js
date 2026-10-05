// Clicks the fixture through its connect card. The demo opens on that card and
// advances on what the probe found, so a sweep that wants the workbench behind
// it has to answer the form first. Fed to audit-sweep.mjs as ONB=this file.
//
// Every wait here is on a condition rather than a duration: the opening plays
// for ten seconds before the card can be answered, and the probe takes as long
// as it takes. A sweep that walked past a card still on screen reported the
// whole workbench as skipped rather than as failed.
(async () => {
  const set = (el, v) => {
    Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), "value").set.call(el, v);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  };
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const until = async (fn, ms = 20000) => {
    const end = Date.now() + ms;
    for (;;) {
      const v = fn();
      if (v) return v;
      if (Date.now() > end) return null;
      await sleep(120);
    }
  };
  const card = () => document.querySelector(".onb");
  const go = () => {
    const b = document.querySelector("button.onb-go");
    return b && !b.disabled ? b : null;
  };
  const done = () => !!document.querySelector(".app");

  if (!(await until(card))) return { reached: "no connect card", app: done() };
  for (const el of document.querySelectorAll("input")) {
    const h = el.name + el.id + (el.placeholder || "") + (el.type || "");
    if (/password|key/i.test(h)) set(el, "sk-audit-mock-key");
    else if (/url|address|http/i.test(h)) set(el, "https://api.deepseek.com");
  }
  // The card advances in place — connect, then start — so the step is counted
  // by what the button does, not by what it says: a label is the one thing here
  // that changes with the interface language.
  const steps = [];
  for (let i = 0; i < 4 && !done(); i++) {
    const b = await until(go);
    if (!b) break;
    steps.push(b.textContent.trim().slice(0, 24));
    b.click();
    await until(() => done() || (go() && go().textContent.trim() !== steps[steps.length - 1]));
  }
  await until(done);
  return { steps, app: done(), left: (card()?.innerText || "").split("\n")[0] };
})()
