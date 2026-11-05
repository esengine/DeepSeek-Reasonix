// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Hero } from "./Hero";

afterEach(cleanup);

describe("the empty conversation's hero", () => {
  it("offers three ways to start and hands the chosen sentence to the composer", async () => {
    const onSuggest = vi.fn();
    render(<Hero needsProject={false} onOpen={() => {}} onKeep={() => {}} onSuggest={onSuggest} />);
    const chips = screen.getAllByRole("button");
    expect(chips.map((b) => b.textContent)).toEqual(["解释这个项目的结构", "找出最近的回归", "给改动写测试"]);
    await userEvent.click(chips[1]!);
    expect(onSuggest).toHaveBeenCalledWith("找出最近的回归");
  });

  it("offers none of them before a project is open", () => {
    render(<Hero needsProject onOpen={() => {}} onKeep={() => {}} onSuggest={() => {}} />);
    expect(screen.queryByText("解释这个项目的结构")).toBeNull();
  });
});
