import { MockTheme } from "./mock_theme";
import type { BackupApplyResult, BackupCatalog, BackupCreated, BackupCreateRequest, BackupEntry, BackupPlan } from "./backup";

const CATEGORIES: BackupCatalog["categories"] = [
  { id: "settings", defaultOn: true, consent: false },
  { id: "extensions", defaultOn: true, consent: false },
  { id: "memory", defaultOn: true, consent: false },
  { id: "automation", defaultOn: true, consent: true },
  { id: "secrets", defaultOn: false, consent: false },
];

// The fixture keeps what it was handed in memory, so the settings section can
// be driven end to end with no account service behind it.
export class MockBackup extends MockTheme {
  private backupList: BackupEntry[] = [];

  async backups(): Promise<BackupCatalog> {
    return { categories: CATEGORIES, backups: this.backupList, limits: { maxCount: 10, maxBytes: 4 << 20 }, minPassphrase: 10 };
  }

  async createBackup(req: BackupCreateRequest): Promise<BackupCreated> {
    const backup: BackupEntry = {
      id: "b" + (this.backupList.length + 1), label: req.label, format: 1, appVersion: "demo",
      platform: "darwin/arm64", categories: req.categories, ciphertextBytes: 18432, createdAt: new Date().toISOString(),
    };
    this.backupList = [backup, ...this.backupList];
    return { backup };
  }

  async deleteBackup(id: string): Promise<void> {
    this.backupList = this.backupList.filter((b) => b.id !== id);
  }

  async previewBackup(): Promise<BackupPlan> {
    return {
      planId: "demo", createdAt: new Date().toISOString(), platform: "darwin/arm64", samePlatform: true,
      categories: ["settings", "automation"],
      items: [
        { id: "provider:deepseek", category: "settings", kind: "provider", name: "deepseek", status: "same", summary: "openai https://api.deepseek.com", recommended: false },
        { id: "hook:PreToolUse#1", category: "automation", kind: "hook", name: "PreToolUse#1", status: "new", consent: "executes", summary: "npx prettier --check .", details: ["event: PreToolUse", "match: edit_file"], recommended: false },
        { id: "memory:docs/REASONIX.md", category: "memory", kind: "memory", name: "docs/REASONIX.md", status: "changed", summary: "REASONIX.md", content: "回答保持简洁。\n提交信息用英文。", recommended: true },
      ],
    };
  }

  async applyBackup(_planId: string, items: string[]): Promise<BackupApplyResult> {
    return { applied: items };
  }
}
