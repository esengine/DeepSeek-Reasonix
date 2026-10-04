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
      "if: github.ref == 'refs/heads/platform' && inputs.firebase_data_action == 'none'",
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
    expect(migrationJob).toContain('"refs/heads/platform"');
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

describe("feedback triage migration workflow", () => {
  it("applies and verifies the triage schema before the Worker deploys", () => {
    const step = workflow.indexOf("Apply and verify feedback triage D1 migration");
    expect(step).toBeGreaterThan(workflow.indexOf("migrate-feedback.sql"));
    expect(step).toBeLessThan(workflow.indexOf("npx wrangler deploy"));
    const body = workflow.slice(step, workflow.indexOf("- name: Apply Studio telemetry schema"));
    expect(body).toContain("--file=migrate-feedback-triage.sql");
    for (const name of ["feedback_releases", "feedback_releases_install", "feedback_replies", "feedback_blocks", "feedback_trust", "feedback_public_images", "feedback_install_status_updated", "feedback_replies_unhandled"]) {
      expect(body).toContain(name);
    }
    expect(body).toContain("missing $name after migration");
    expect(body).toContain("substr(install_hash,1,8)");
  });
});
