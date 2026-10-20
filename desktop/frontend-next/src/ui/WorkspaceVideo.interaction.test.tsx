// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import "./testkit";
import { WorkspaceVideo, isWorkspaceVideo } from "./WorkspaceVideo";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const port = { workspaceVideoURL: (path: string) => `/workspace/video?path=${encodeURIComponent(path)}` } as unknown as AgentPort;

describe("WorkspaceVideo", () => {
  it("recognises the containers the kernel serves and nothing else", () => {
    for (const p of ["a.mp4", "b.M4V", "c.mov", "d.webm"]) expect(isWorkspaceVideo(p)).toBe(true);
    for (const p of ["a.mkv", "manifest.json", "a.mp4.source.json", "a.png"]) expect(isWorkspaceVideo(p)).toBe(false);
  });

  it("plays the workspace file through the range-serving address", () => {
    const { container } = render(<WorkspaceVideo port={port} path="video/teaser/s01-v1.mp4" />);
    const video = container.querySelector("video")!;
    expect(video.getAttribute("src")).toBe("/workspace/video?path=video%2Fteaser%2Fs01-v1.mp4");
    expect(video.hasAttribute("controls")).toBe(true);
  });

  it("says the video failed instead of leaving an empty player", () => {
    const { container, getByRole } = render(<WorkspaceVideo port={port} path="gone.mp4" />);
    fireEvent.error(container.querySelector("video")!);
    expect(getByRole("alert").textContent).not.toBe("");
    expect(container.querySelector("video")).toBeNull();
  });
});
