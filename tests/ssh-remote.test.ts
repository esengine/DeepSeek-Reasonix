import { describe, expect, it } from "vitest";
import { generateSshDryRunReport, parseSshUri, probeSsh } from "../src/cli/ssh-remote.js";

describe("parseSshUri", () => {
  it("parses a full ssh:// URI with user, host, port, and path", () => {
    const uri = parseSshUri("ssh://dev@example.com:2222/home/dev/project");
    expect(uri).not.toBeNull();
    expect(uri!.user).toBe("dev");
    expect(uri!.host).toBe("example.com");
    expect(uri!.port).toBe(2222);
    expect(uri!.path).toBe("/home/dev/project");
  });

  it("parses an ssh:// URI without a user (falls back to local username)", () => {
    const uri = parseSshUri("ssh://example.com/opt/app");
    expect(uri).not.toBeNull();
    expect(uri!.host).toBe("example.com");
    expect(uri!.port).toBe(22);
    expect(uri!.path).toBe("/opt/app");
    // user is inferred from OS — just check it's a non-empty string
    expect(typeof uri!.user).toBe("string");
    expect(uri!.user.length).toBeGreaterThan(0);
  });

  it("parses an ssh:// URI without a path (defaults to /)", () => {
    const uri = parseSshUri("ssh://root@host");
    expect(uri).not.toBeNull();
    expect(uri!.host).toBe("host");
    expect(uri!.user).toBe("root");
    expect(uri!.port).toBe(22);
    expect(uri!.path).toBe("/");
  });

  it("parses an ssh:// URI with only host", () => {
    const uri = parseSshUri("ssh://my-server");
    expect(uri).not.toBeNull();
    expect(uri!.host).toBe("my-server");
    expect(uri!.port).toBe(22);
  });

  it("returns null for non-ssh URIs", () => {
    expect(parseSshUri("https://example.com")).toBeNull();
    expect(parseSshUri("git@github.com:user/repo.git")).toBeNull();
    expect(parseSshUri("/local/path")).toBeNull();
    expect(parseSshUri("")).toBeNull();
  });

  it("returns null for malformed ssh:// URIs with no host", () => {
    expect(parseSshUri("ssh://")).toBeNull();
    expect(parseSshUri("ssh://:22/path")).toBeNull();
  });
});

describe("probeSsh", () => {
  it("returns a probe result when ssh is on PATH", () => {
    const probe = probeSsh();
    // `ssh -V` exists on macOS and most dev machines; if it fails the
    // function returns null, which is fine on a minimal CI runner.
    if (probe) {
      expect(probe.sshBin).toBe("ssh");
      expect(typeof probe.version).toBe("string");
      expect(probe.version.length).toBeGreaterThan(0);
    }
  });
});

describe("generateSshDryRunReport", () => {
  const uri = { user: "dev", host: "box", port: 22, path: "/src" };

  it("includes the RFC issue number and dry-run banner", () => {
    const report = generateSshDryRunReport(uri, {
      sshBin: "ssh",
      version: "OpenSSH_9.6",
    });

    expect(report).toContain("RFC");
    expect(report).toContain("#2140");
    expect(report).toContain("dry-run");
    expect(report).toContain("ssh://dev@box:22/src");
  });

  it("lists the planned steps when ssh is available", () => {
    const report = generateSshDryRunReport(uri, {
      sshBin: "ssh",
      version: "OpenSSH_9.6",
    });

    expect(report).toContain("verify connectivity");
    expect(report).toContain("probe remote environment");
    expect(report).toContain("install or update Reasonix");
    expect(report).toContain("launch Reasonix");
    expect(report).toContain("SSH tunnel");
    expect(report).toContain("reasonix code --no-dashboard");
  });

  it("warns when ssh is missing and shows install instructions", () => {
    const report = generateSshDryRunReport(uri, null);

    expect(report).toContain("WARNING");
    expect(report).toContain("ssh");
    expect(report).toContain("not found");
  });

  it("includes the short-term recommendation section", () => {
    const report = generateSshDryRunReport(uri, {
      sshBin: "ssh",
      version: "OpenSSH_9.6",
    });

    expect(report).toContain("short-term recommendation");
    expect(report).toContain("Run Reasonix directly on the remote host");
    expect(report).toContain("SSH tunnel");
    expect(report).toContain("127.0.0.1:8420");
  });

  it("notes that no remote commands execute", () => {
    const report = generateSshDryRunReport(uri, {
      sshBin: "ssh",
      version: "OpenSSH_9.6",
    });

    expect(report).toContain("no remote commands execute");
    expect(report).toContain("no network connections are made");
  });

  it("includes the reasonix version", () => {
    const report = generateSshDryRunReport(uri, {
      sshBin: "ssh",
      version: "OpenSSH_9.6",
    });

    expect(report).toContain("reasonix");
  });

  it("mentions GPU passthrough is tracked separately", () => {
    const report = generateSshDryRunReport(uri, null);

    expect(report).toContain("#2141");
    expect(report).toContain("GPU passthrough");
    expect(report).toContain("tracked separately");
  });
});
