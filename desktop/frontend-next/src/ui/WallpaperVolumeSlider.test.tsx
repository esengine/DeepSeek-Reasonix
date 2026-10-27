// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WallpaperVolumeSlider } from "./WallpaperVolumeSlider";

afterEach(cleanup);

describe("the scene volume slider", () => {
  it("uses the same filled track as the focus sliders", () => {
    const { container } = render(<WallpaperVolumeSlider volume={0.6} onChange={() => {}} />);
    const slider = screen.getByRole("slider", { name: /音量/ }) as HTMLInputElement;
    expect(slider.style.getPropertyValue("--at")).toBe("60%");
    expect(container.textContent).toContain("60%");
  });

  it("writes the level and treats zero as silent", () => {
    const onChange = vi.fn();
    render(<WallpaperVolumeSlider volume={0.6} onChange={onChange} />);
    const slider = screen.getByRole("slider", { name: /音量/ });
    fireEvent.change(slider, { target: { value: "0.25" } });
    expect(onChange).toHaveBeenCalledWith({ volume: 0.25, muted: false });
    fireEvent.change(slider, { target: { value: "0" } });
    expect(onChange).toHaveBeenLastCalledWith({ volume: 0, muted: true });
  });
});
