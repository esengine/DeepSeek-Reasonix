import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { chromium } from "playwright";

const sheets = await Promise.all(["tokens", "app", "studio", "sessionquick", "sessionpin", "treerows"].map(
  (name) => readFile(new URL(`../src/styles/${name}.css`, import.meta.url), "utf8"),
));
const title = "这是什么？ @.reasonix/attachments/clipboard-" + "long-title".repeat(20);
const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    for (const width of [220, 320]) {
      const page = await browser.newPage({ colorScheme: scheme });
      await page.setContent(`<div class="rail" style="width:${width}px">
        <div class="kids">
          <div class="sessrow sessrow-context" tabindex="0" aria-selected="true">
            <button class="sesspin-action" data-action="session.pin">置顶</button>
            <span class="sesstitle" title="${title}"><span>${title}</span><em class="sessarch">已归档</em></span>
            <span class="sessquick"><button data-action="session.archive">归档</button></span>
          </div>
          <div class="sessrow sessrow-context" data-unread tabindex="0">
            <span class="sesstitle" title="短标题"><span>短标题</span></span><span class="unread-dot"></span>
          </div>
          <div class="sessrow sessrow-remote">
            <span class="sesstitle" title="${title}"><span>${title}</span></span><span class="sessmeta">3 轮</span>
          </div>
          <div class="sessrow sessrow-context" data-aged>
            <button class="sesspin-action" data-action="session.pin">置顶</button>
            <span class="sesstitle" title="${title}"><span>${title}</span></span>
            <time class="sessmeta sessage">9 天前</time><button class="sesscopies">+1</button>
            <span class="sessquick"><button data-action="session.archive">归档</button></span>
          </div>
          <div class="sessrow sessrow-context" data-aged-only>
            <button class="sesspin-action" data-action="session.pin">置顶</button>
            <span class="sesstitle" title="${title}"><span>${title}</span></span>
            <time class="sessmeta sessage">9 天前</time>
            <span class="sessquick"><button data-action="session.archive">归档</button></span>
          </div>
          <div class="sessrow sesscopy" tabindex="0">
            <span class="sesstitle" title="早先版本 · 4 轮"><span>早先版本 · 4 轮</span></span>
            <button class="wsdel" data-action="session.delete" aria-label="删除该会话"><svg viewBox="0 0 16 16"><path d="M4 4l8 8M12 4l-8 8" /></svg></button>
          </div>
        </div>
      </div>`);
      for (const content of sheets) await page.addStyleTag({ content });
      const measure = () => page.evaluate(() => [...document.querySelectorAll(".sessrow")].map((row) => {
        const text = row.querySelector(".sesstitle > span");
        const box = row.getBoundingClientRect();
        const style = getComputedStyle(text);
        return {
          height: box.height, top: box.top, bottom: box.bottom,
          textHeight: text.getBoundingClientRect().height, lineHeight: parseFloat(style.lineHeight),
          nowrap: style.whiteSpace, ellipsis: style.textOverflow,
          truncated: text.scrollWidth > text.clientWidth,
          fullTitle: row.querySelector(".sesstitle").title === text.textContent,
        };
      }));
      for (const state of ["idle", "hover", "focus"]) {
        if (state === "hover") await page.locator(".sessrow").first().hover();
        if (state === "focus") await page.locator(".sessrow").first().focus();
        const rows = await measure();
        for (const row of rows) {
          assert.equal(row.height, 34, `${scheme}/${width}/${state}: row height`);
          assert.equal(row.nowrap, "nowrap");
          assert.equal(row.ellipsis, "ellipsis");
          assert.ok(row.textHeight <= row.lineHeight + 1);
          assert.ok(row.fullTitle);
        }
        assert.equal(rows[1].top - rows[0].bottom, 1, "only the compact row margin separates sessions");
        assert.equal(rows[2].top - rows[1].bottom, 1, "the remote row has the same compact spacing");
        assert.equal(rows[3].top - rows[2].bottom, 1, "the aged row has the same compact spacing");
        assert.equal(rows[4].top - rows[3].bottom, 1, "the age-only row has the same compact spacing");
        assert.equal(rows[5].top - rows[4].bottom, 1, "the earlier version has the same compact spacing");
        assert.ok(rows[0].truncated && rows[2].truncated, "long local and remote titles truncate");
        if (state === "idle") {
          for (const [selector, allowed] of [[".sessrow[data-aged]", 8], [".sessrow[data-aged-only]", 2]]) {
            const gap = await page.evaluate((sel) => {
              const row = document.querySelector(sel);
              const box = row.getBoundingClientRect();
              const inner = box.right - parseFloat(getComputedStyle(row).paddingRight);
              const shown = [...row.children].filter((el) => {
                const cs = getComputedStyle(el);
                return cs.position !== "absolute" && cs.visibility !== "hidden" && el.getBoundingClientRect().width > 0 && !el.matches(".sessquick");
              });
              return { free: inner - Math.max(...shown.map((el) => el.getBoundingClientRect().right)), quick: row.querySelector(".sessquick").getBoundingClientRect().width };
            }, selector);
            assert.ok(gap.free <= allowed, `${scheme}/${width} ${selector}: ${gap.free}px of the row's right side is held back while no button is showing; the title should have it`);
            assert.equal(gap.quick, 0, `${scheme}/${width}: the hover buttons take no room until the row is hovered or focused`);
          }
        }
        if (state !== "idle") {
          const contained = await page.locator(".sessquick > button, .sesspin-action").evaluateAll((buttons) => buttons.filter((button) => (button.parentElement.matches(".sessquick") ? button.parentElement.getBoundingClientRect().width > 0 : getComputedStyle(button).opacity !== "0")).every((button) => {
            const rect = button.getBoundingClientRect();
            const row = button.closest(".sessrow").getBoundingClientRect();
            return rect.top >= row.top && rect.bottom <= row.bottom && rect.left >= row.left && rect.right <= row.right;
          }));
          assert.ok(contained, "quick actions fit inside the compact row");
        }
      }
      // An earlier version has no row menu: its × is the only way a pointer deletes it.
      for (const how of ["hover", "focus"]) {
        await page.mouse.move(width + 200, 400);
        if (how === "hover") await page.locator(".sesscopy").hover();
        else await page.locator(".sesscopy").focus();
        await page.waitForFunction(() => getComputedStyle(document.querySelector(".sesscopy .wsdel")).opacity === "1", null, { timeout: 1000 }).catch(() => {});
        const del = await page.evaluate(() => {
          const row = document.querySelector(".sesscopy");
          const button = row.querySelector(".wsdel");
          const box = button.getBoundingClientRect();
          const edge = row.getBoundingClientRect();
          return {
            shown: box.width > 0 && box.height > 0 && getComputedStyle(button).opacity === "1",
            inside: box.top >= edge.top && box.bottom <= edge.bottom && box.left >= edge.left && box.right <= edge.right,
          };
        });
        assert.ok(del.shown, `${scheme}/${width}/${how}: an earlier version shows its delete button`);
        assert.ok(del.inside, `${scheme}/${width}/${how}: the delete button sits inside its row`);
      }
      console.log(`${scheme}/${width}: 34px rows, 1px spacing, single-line ellipsis, hover/focus stable, an earlier version's × shows`);
      await page.close();
    }
  }
} finally {
  await browser.close();
}
