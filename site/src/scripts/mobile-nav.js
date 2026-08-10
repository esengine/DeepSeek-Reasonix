// Shared responsive navigation behavior for the marketing and community headers.
export function initMobileNav() {
  document.querySelectorAll("[data-mobile-nav]").forEach((menu) => {
    const header = menu.closest(".nav");
    const toggle = header?.querySelector("[data-mobile-nav-toggle]");
    const panel = menu.querySelector(".mobile-nav-panel");
    if (!toggle || !panel) return;

    let closeTimer;
    let stateToken = 0;
    let returnFocus = toggle;
    const focusables = () => Array.from(panel.querySelectorAll(
      'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])',
    )).filter((element) => !element.closest("[hidden]"));

    const close = (restore = true) => {
      stateToken += 1;
      clearTimeout(closeTimer);
      toggle.setAttribute("aria-expanded", "false");
      menu.setAttribute("aria-hidden", "true");
      menu.classList.remove("is-open");
      document.body.classList.remove("mobile-nav-open");
      closeTimer = window.setTimeout(() => { menu.hidden = true; }, 220);
      if (restore) returnFocus.focus();
    };

    const open = () => {
      const token = ++stateToken;
      clearTimeout(closeTimer);
      returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : toggle;
      menu.hidden = false;
      menu.setAttribute("aria-hidden", "false");
      toggle.setAttribute("aria-expanded", "true");
      document.body.classList.add("mobile-nav-open");
      requestAnimationFrame(() => {
        if (token === stateToken && !menu.hidden) menu.classList.add("is-open");
      });
      requestAnimationFrame(() => {
        if (token === stateToken && !menu.hidden) focusables()[0]?.focus();
      });
    };

    toggle.addEventListener("click", () => (menu.hidden ? open() : close()));
    menu.querySelectorAll("[data-mobile-nav-close]").forEach((button) => button.addEventListener("click", () => close()));
    menu.querySelectorAll("a[href]").forEach((link) => link.addEventListener("click", () => close(false)));
    menu.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        event.preventDefault();
        close();
        return;
      }
      if (event.key !== "Tab") return;
      const items = focusables();
      if (!items.length) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    });
    menu.addEventListener("transitionend", (event) => {
      if (event.propertyName === "opacity" && !menu.classList.contains("is-open")) menu.hidden = true;
    });
    window.addEventListener("resize", () => {
      if (window.matchMedia("(min-width: 1101px)").matches && !menu.hidden) close(false);
    }, { passive: true });
  });
}
