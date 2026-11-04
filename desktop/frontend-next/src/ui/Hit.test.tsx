// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { Hit } from "./Hit";

afterEach(cleanup);

it("marks the first match whatever its case and keeps the surrounding text", () => {
  const { container } = render(<span><Hit text="Fix the Login bug" needle="login" /></span>);
  expect(container.querySelector("mark.hit")?.textContent).toBe("Login");
  expect(container.textContent).toBe("Fix the Login bug");
});

it("renders plain text without a needle or a match", () => {
  expect(render(<span><Hit text="abc" needle="" /></span>).container.querySelector("mark")).toBeNull();
  cleanup();
  expect(render(<span><Hit text="abc" needle="z" /></span>).container.querySelector("mark")).toBeNull();
});
