// Windows groups taskbar buttons by AppUserModelID. The pinned shortcut points
// at the permanent reasonix-launcher.exe while the window belongs to this
// process, so both need the launcher's explicit ID; the implicit identity
// Windows derives from each executable path splits them into two icons.
// Guard: internal/appidentity/electron_identity_test.go ties it to the Go ID.

export const APP_USER_MODEL_ID = "Reasonix";

// Applied at module load: the ID must be in place before the first window
// exists, and Windows ignores the call on other platforms.
export function applyAppUserModelId(target: Pick<typeof import("electron").app, "setAppUserModelId">, platform: NodeJS.Platform): void {
  if (platform === "win32") target.setAppUserModelId(APP_USER_MODEL_ID);
}
