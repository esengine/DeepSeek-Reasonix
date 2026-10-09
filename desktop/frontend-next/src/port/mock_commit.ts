import { MockBackup } from "./mock_backup";
import type { CommitProposal, CommitRequest, CommitResult } from "./workspace";

export class MockCommit extends MockBackup {
  async proposeCommit(): Promise<CommitProposal> {
    await new Promise((r) => setTimeout(r, 600));
    return {
      message: "fix(provider): back off retries on 429 and drop credentials from logs\n\nHonour Retry-After before the next attempt and stop printing the key.",
      fingerprint: "mock",
      files: [
        { path: "internal/provider/retry.go", status: "M", insertions: 14, deletions: 3 },
        { path: "internal/config/credentials.go", status: "M", insertions: 6, deletions: 2 },
      ],
      truncated: false,
      contentSecrets: false,
    };
  }

  async commitStaged(req: CommitRequest): Promise<CommitResult> {
    return { hash: "0123456789abcdef0123456789abcdef01234567", subject: req.message.split("\n")[0] };
  }
}
