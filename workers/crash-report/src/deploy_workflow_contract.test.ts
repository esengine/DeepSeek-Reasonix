import { describe, expect, it } from "vitest";
// @ts-expect-error Test code uses Node to inspect the repository workflow.
import { readFileSync } from "node:fs";

const workflow = readFileSync("../../.github/workflows/deploy-crash-worker.yml", "utf8");

describe("Firebase crash data migration workflow", () => {
  it("keeps manual migration separate from Worker deployment", () => {
    expect(workflow).toContain("firebase_data_action:");
    expect(workflow).toContain("- dry-run");
    expect(workflow).toContain("- apply");
    expect(workflow).toContain("- verify-only");
    expect(workflow).toContain(
      "if: github.event_name == 'push' || inputs.firebase_data_action == 'none'",
    );
    expect(workflow).toContain(
      "if: github.event_name == 'workflow_dispatch' && inputs.firebase_data_action != 'none'",
    );

    const migrationJob = workflow.slice(workflow.indexOf("  migrate-firebase-data:"));
    expect(migrationJob).not.toContain("wrangler deploy");
  });

  it("requires the protected branch, environment approval, and all secrets", () => {
    const migrationJob = workflow.slice(workflow.indexOf("  migrate-firebase-data:"));
    expect(migrationJob).toContain("environment: canary");
    expect(migrationJob).toContain('"refs/heads/main-v2"');
    expect(migrationJob).toContain("CLOUDFLARE_API_TOKEN: ${{ secrets.CLOUDFLARE_API_TOKEN }}");
    expect(migrationJob).toContain("FIREBASE_DATABASE_URL: ${{ secrets.FIREBASE_DATABASE_URL }}");
    expect(migrationJob).toContain("FIREBASE_CLIENT_EMAIL: ${{ secrets.FIREBASE_CLIENT_EMAIL }}");
    expect(migrationJob).toContain("FIREBASE_PRIVATE_KEY: ${{ secrets.FIREBASE_PRIVATE_KEY }}");
  });

  it("guards apply and verifies immediately on the same runner", () => {
    const migrationJob = workflow.slice(workflow.indexOf("  migrate-firebase-data:"));
    expect(migrationJob).toContain(
      'if [ "$FIREBASE_DATA_CONFIRMATION" != "APPLY_FIREBASE_CRASH_DATA" ]; then',
    );
    const apply = migrationJob.indexOf("npm run migrate:firebase-data -- --apply");
    const verify = migrationJob.indexOf("npm run migrate:firebase-data -- --verify-only");
    expect(apply).toBeGreaterThan(0);
    expect(verify).toBeGreaterThan(apply);
  });
});
