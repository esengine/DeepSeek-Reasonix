// Run: tsx src/__tests__/composer-profile.test.ts

import {
  composerProfileMode,
  controllerComposerProfileCollaborationMode,
  displayedComposerProfileCollaborationMode,
  hydrateComposerProfileFromMeta,
  hydrateComposerProfilesFromTabs,
  patchComposerProfile,
  type ComposerProfilesByTab,
} from "../lib/composerProfile";
import type { Meta, TabMeta } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function tab(overrides: Partial<TabMeta> = {}): TabMeta {
  return {
    id: "tab-1",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    topicId: "topic-1",
    topicTitle: "Topic",
    label: "DeepSeek-R1",
    ready: true,
    running: false,
    mode: "normal",
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
    active: true,
    cwd: "/repo",
    ...overrides,
  };
}

function meta(overrides: Partial<Meta> = {}): Meta {
  return {
    label: "DeepSeek-R1",
    ready: true,
    eventChannel: "events",
    cwd: "/repo",
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
    ...overrides,
  };
}

console.log("\ncomposer profile");

{
  let profiles: ComposerProfilesByTab = {};
  profiles = hydrateComposerProfilesFromTabs(profiles, [tab({ tokenMode: "economy" })]);
  profiles = patchComposerProfile(
    profiles,
    "tab-1",
    profiles["tab-1"],
    { collaborationMode: "normal", goalDraftMode: true, goal: "" },
    ["collaborationMode", "goal"],
  );
  profiles = patchComposerProfile(
    profiles,
    "tab-1",
    profiles["tab-1"],
    { collaborationMode: "plan", goalDraftMode: false, goal: "" },
    ["collaborationMode", "goal"],
  );

  profiles = hydrateComposerProfilesFromTabs(profiles, [tab({ tokenMode: "economy" })]);

  eq(displayedComposerProfileCollaborationMode(profiles["tab-1"]), "plan", "stale tab hydration keeps locally selected plan mode");
  eq(profiles["tab-1"].tokenMode, "economy", "token saver remains independent of collaboration mode changes");
  eq(composerProfileMode(profiles["tab-1"]), "plan", "compat mode keeps the plan axis enabled");
  eq(Boolean(profiles["tab-1"].pending.collaborationMode), true, "pending plan stays pending until backend acknowledges it");

  profiles = hydrateComposerProfilesFromTabs(profiles, [tab({ mode: "plan", collaborationMode: "plan", tokenMode: "economy" })]);

  eq(displayedComposerProfileCollaborationMode(profiles["tab-1"]), "plan", "acknowledged tab hydration keeps plan visible");
  eq(Boolean(profiles["tab-1"].pending.collaborationMode), false, "backend acknowledgement clears pending plan");
}

{
  let profiles: ComposerProfilesByTab = {};
  profiles = hydrateComposerProfilesFromTabs(profiles, [tab()]);
  profiles = patchComposerProfile(profiles, "tab-1", profiles["tab-1"], { tokenMode: "economy" }, ["tokenMode"]);
  profiles = hydrateComposerProfileFromMeta(profiles, "tab-1", meta({ tokenMode: "full" }));

  eq(profiles["tab-1"].tokenMode, "economy", "stale meta cannot erase a pending token saver selection");
  eq(Boolean(profiles["tab-1"].pending.tokenMode), true, "token saver stays pending while meta is stale");

  profiles = hydrateComposerProfileFromMeta(profiles, "tab-1", meta({ tokenMode: "economy" }));

  eq(profiles["tab-1"].tokenMode, "economy", "acknowledged token saver remains enabled");
  eq(Boolean(profiles["tab-1"].pending.tokenMode), false, "token saver pending clears after matching meta");
}

{
  let profiles: ComposerProfilesByTab = {};
  profiles = hydrateComposerProfilesFromTabs(profiles, [tab()]);
  profiles = patchComposerProfile(
    profiles,
    "tab-1",
    profiles["tab-1"],
    { collaborationMode: "normal", goalDraftMode: true, goal: "" },
    ["collaborationMode", "goal"],
  );
  profiles = hydrateComposerProfilesFromTabs(profiles, [tab()]);

  eq(displayedComposerProfileCollaborationMode(profiles["tab-1"]), "goal", "empty goal draft remains visible through stale tab hydration");
  eq(controllerComposerProfileCollaborationMode(profiles["tab-1"]), "normal", "empty goal draft syncs to controller as normal");
  eq(composerProfileMode(profiles["tab-1"]), "normal", "empty goal draft does not enable plan compatibility mode");
}

{
  let profiles: ComposerProfilesByTab = {};
  profiles = hydrateComposerProfilesFromTabs(profiles, [tab(), tab({ id: "tab-2" })]);
  profiles = patchComposerProfile(profiles, "tab-2", profiles["tab-2"], { tokenMode: "economy" }, ["tokenMode"]);
  profiles = hydrateComposerProfilesFromTabs(profiles, [tab()]);

  eq(Boolean(profiles["tab-2"]), false, "tab hydration removes profiles for closed tabs");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
