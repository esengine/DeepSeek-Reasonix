import type { SandboxView, ShellCapabilityView } from "./types";

type PreviewPlatform = "darwin" | "windows" | "linux" | "";

// Browser-preview stand-ins for the backend shell inventory and repair policy.
// Keeping this mock owner outside bridge.ts preserves that generated-style
// boundary's file-size ratchet as the Settings contract grows.
export function browserPreviewShellSupport(platform: PreviewPlatform): Pick<SandboxView, "shellCapabilities" | "gitCapability" | "shellInstallAction" | "shellRepairGuidance" | "gitRepairGuidance"> {
  const shellCapabilities: ShellCapabilityView[] = platform !== "windows" ? [
    { id: "bash", variant: "system", available: true, path: "/bin/bash", source: "path" },
    { id: "zsh", variant: "system", available: platform === "darwin", ...(platform === "darwin" ? { path: "/bin/zsh", source: "standard-path" } : { reason: "not-found" }) },
    { id: "sh", variant: "system", available: true, path: "/bin/sh", source: "standard-path" },
  ] : [
    { id: "git-bash", variant: "git-for-windows", available: true, path: "C:\\Program Files\\Git\\bin\\bash.exe", source: "standard-path" },
    { id: "powershell", available: true, path: "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe", source: "standard-path" },
    { id: "pwsh", available: false, reason: "not-installed" },
  ];
  return {
    shellCapabilities,
    gitCapability: { id: "git", available: true, path: platform === "windows" ? "C:\\Program Files\\Git\\cmd\\git.exe" : "/usr/bin/git", source: "path" },
    shellInstallAction: platform === "windows" ? { id: "git-for-windows", mode: "manual", available: false, manualUrl: "https://git-scm.com/download/win" } : null,
    shellRepairGuidance: platform === "linux" ? { manager: "apt", command: "apt-get install bash" } : null,
    gitRepairGuidance: platform === "darwin"
      ? { manager: "homebrew", command: "brew install git" }
      : platform === "linux" ? { manager: "apt", command: "apt-get install git" } : null,
  };
}
