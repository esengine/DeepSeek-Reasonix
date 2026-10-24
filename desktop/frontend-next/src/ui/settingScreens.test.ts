import { describe, expect, it } from "vitest";
import { SETTING_SCREEN, settingScreen, unknownScreens } from "./settingScreens";

describe("where the settings sheet draws a setting the model names", () => {
  it("opens the file delivery switch for its canonical setting id", () => {
    expect(settingScreen("tools.present_files")).toBe("tools:present-files");
  });
  it("only names rows the sheet really has", () => {
    expect(unknownScreens()).toEqual([]);
  });

  it("answers nothing for a setting with no row, so the sheet opens at its top", () => {
    expect(settingScreen("agent.max_steps")).toBeUndefined();
    expect(Object.keys(SETTING_SCREEN).length).toBeGreaterThan(30);
  });
});
