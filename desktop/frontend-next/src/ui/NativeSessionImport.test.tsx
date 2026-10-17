// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { MockHub } from "../port/mock_hub";
import { NativeSessionImport } from "./NativeSessionImport";

afterEach(cleanup);

it("passes the complete native envelope to the backend and refreshes history", async () => {
  const hub = new MockHub();
  const imported = vi.spyOn(hub,"importNativeSession");
  const refresh = vi.fn();
  const archive = {format:"reasonix-native-session",version:2,records:[{role:"system",agentComposition:{digest:"unchanged"}}]};
  const file = new File([JSON.stringify(archive)],"session.json",{type:"application/json"});
  Object.defineProperty(file,"text",{value:async()=>JSON.stringify(archive)});
  render(<NativeSessionImport hub={hub} workspace="/workspace" onImported={refresh}/>);
  await userEvent.upload(screen.getByLabelText("选择会话 JSON 文件"),file);
  expect((await screen.findByRole("status")).textContent).toContain("绑定版本保持不变");
  expect(imported).toHaveBeenCalledWith(archive,"/workspace");
  expect(refresh).toHaveBeenCalledOnce();
});
