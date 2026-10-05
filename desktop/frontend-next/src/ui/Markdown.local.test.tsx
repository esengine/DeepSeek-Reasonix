// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { Markdown, type LocalRefs } from "./Markdown";

afterEach(cleanup);

const doc = "[guide](docs/guide.md) [web](https://example.com) [top](#top)\n\n![logo](img/logo.png) ![remote](https://example.com/x.png)";

it("opens a workspace link beside the document and loads its images from the tree", () => {
  const opened = vi.fn();
  const local: LocalRefs = {
    link: (href) => (href.startsWith("docs/") ? () => opened(href) : null),
    image: (src) => (src.startsWith("img/") ? `/rt/r1/workspace/image?path=${src}` : null),
  };
  const { container, getByText } = render(<Markdown text={doc} local={local} />);

  const guide = getByText("guide");
  const click = new MouseEvent("click", { bubbles: true, cancelable: true });
  guide.dispatchEvent(click);
  expect(opened).toHaveBeenCalledWith("docs/guide.md");
  expect(click.defaultPrevented).toBe(true);

  expect(getByText("web").getAttribute("target")).toBe("_blank");
  expect(getByText("top").hasAttribute("href")).toBe(false);

  const [logo, remote] = [...container.querySelectorAll("img")];
  expect(logo.getAttribute("src")).toBe("/rt/r1/workspace/image?path=img/logo.png");
  expect(remote.getAttribute("src")).toBe("https://example.com/x.png");
});

it("leaves a reply's links as they were when no document is being read", () => {
  const { getByText } = render(<Markdown text="[guide](docs/guide.md)" />);
  const link = getByText("guide");
  fireEvent.click(link);
  expect(link.getAttribute("target")).toBe("_blank");
  expect(link.getAttribute("href")).toBe("docs/guide.md");
});
