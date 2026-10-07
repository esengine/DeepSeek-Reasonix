import type { AgentPort } from "./port";
import type { HubPort, RuntimeView, TreeWorkspace } from "./hub";
import type { RemoteHost, RemoteListing, RemoteProbe } from "./remote";
import type { ShareStatus } from "./share";
import { MockPort } from "./mock";

// MockHub is the fixture's answer to a window that drives several panes. Each
// pane gets its own scripted session, so the split view can be worked on
// without a kernel — the dev build's only reason to exist.
export class MockHub implements HubPort {
  private readonly ports = new Map<string, AgentPort>();
  private readonly machine = { configured: false };
  private readonly views: RuntimeView[] = [
    { id: "r1", base: "", root: "~/projects/DeepSeek-Reasonix", name: "DeepSeek-Reasonix", sessionPath: "/sessions/mock.jsonl" },
  ];
  private readonly roots = ["~/projects/DeepSeek-Reasonix", "~/projects/my-website"];
  private seq = 1;

  runtimes() {
    return Promise.resolve([...this.views]);
  }

  async fork(): Promise<RuntimeView> {
    throw new Error("The preview has no persisted conversation to fork.");
  }

  open(req: { root?: string; sessionPath?: string }) {
    const held = req.sessionPath ? this.views.find((v) => v.sessionPath === req.sessionPath) : undefined;
    if (held) return Promise.resolve(held);
    this.seq++;
    const root = req.root || this.roots[0];
    const view: RuntimeView = {
      id: `r${this.seq}`,
      base: `/rt/r${this.seq}`,
      root,
      name: root.split("/").pop() ?? root,
      sessionPath: req.sessionPath,
    };
    this.views.push(view);
    return Promise.resolve(view);
  }

  // Two machines, one of them mid-connect: the fixture has to show the step
  // list and the pip states without a kernel, or neither gets designed.
  remoteHosts() {
    return Promise.resolve<RemoteHost[]>([
      {
        name: "gpu-box",
        target: "ada@10.0.0.4",
        workspace: "/srv/training",
        // A machine with more than one project on it, which is the shape the
        // sidebar has to draw and the one a single field could not.
        workspaces: ["/srv/training", "/srv/eval", "/home/ada/notes"],
        status: "connected",
        panes: 1,
      },
      { name: "builder", target: "ada@build.internal", workspace: "~/work", workspaces: ["~/work"], status: "connecting", step: "install", detail: "npm" },
      { name: "spare", target: "10.0.0.9", status: "idle" },
    ]);
  }

  // The fixture's own far machine: one workspace with a conversation in it, so
  // the tree under a connected host can be designed without one.
  remoteTree(host: string) {
    if (host !== "gpu-box") return Promise.resolve(null);
    return Promise.resolve<TreeWorkspace[]>([
      {
        root: "/srv/training",
        name: "training",
        sessions: [
          { path: "/srv/sessions/pipeline.jsonl", name: "pipeline", title: "数据管线", turns: 7 },
          { path: "/srv/sessions/eval.jsonl", name: "eval", title: "跑一遍评测", turns: 2 },
        ],
      },
      { root: "/srv/scratch", name: "scratch", sessions: [] },
    ]);
  }

  openRemote(req: { host: string; workspace?: string; sessionPath?: string }) {
    this.seq++;
    const workspace = req.workspace || "/srv/training";
    const view: RuntimeView = {
      id: `r${this.seq}`,
      base: `/rt/r${this.seq}`,
      root: workspace,
      name: workspace.split("/").pop() ?? workspace,
      host: req.host,
      sessionPath: req.sessionPath,
    };
    this.views.push(view);
    return Promise.resolve(view);
  }

  // A far machine's filesystem, deep enough that walking down and back up can
  // be designed: the picker's two hard states are a folder with nothing in it
  // and one with more than fits.
  // A machine with no npm and nothing uploadable: the interesting shape, since
  // a ready one has nothing to show.
  async probeRemote(_host: string): Promise<RemoteProbe> {
    return {
      os: "linux",
      arch: "amd64",
      home: "/home/ada",
      ready: false,
      routes: [
        { name: "npm", ok: false, code: "remote.npm_unavailable" },
        { name: "upload", ok: false, code: "remote.platform_mismatch" },
        { name: "download", ok: true },
      ],
    };
  }

  remoteDirs(_host: string, path?: string) {
    const at = path || "/home/ada";
    const kids: Record<string, string[]> = {
      "/": ["home", "srv", "var"],
      "/home": ["ada"],
      "/home/ada": [".config", "notes", "projects"],
      "/home/ada/projects": ["pipeline", "site"],
      "/srv": ["eval", "training"],
    };
    const folders = (kids[at] ?? []).map((name) => ({ name, path: at === "/" ? "/" + name : at + "/" + name }));
    return Promise.resolve<RemoteListing>({
      path: at,
      parent: at === "/" ? undefined : at.slice(0, at.lastIndexOf("/")) || "/",
      folders,
    });
  }

  addRemoteWorkspace() {
    return Promise.resolve();
  }

  removeRemoteWorkspace() {
    return Promise.resolve();
  }

  saveRemoteHost() {
    return Promise.resolve();
  }

  removeRemoteHost() {
    return Promise.resolve();
  }

  remoteCandidates() {
    return Promise.resolve(["attic", "old-laptop"]);
  }

  // The fixture has no link to block, so nothing ever asks. The dialog is
  // driven from a story instead of from a stub that would fire on load.
  onRemoteAsk() {
    return () => {};
  }

  answerRemote() {}

  close(id: string) {
    const at = this.views.findIndex((v) => v.id === id);
    if (at >= 0) this.views.splice(at, 1);
    this.ports.delete(id);
    return Promise.resolve();
  }

  tree() {
    const open = new Map(this.views.filter((v) => v.sessionPath).map((v) => [v.sessionPath!, v.id]));
    return Promise.resolve(
      this.roots.map<TreeWorkspace>((root, i) => ({
        root,
        name: root.split("/").pop() ?? root,
        open: this.views.some((v) => v.root === root),
        remembered: true,
        sessions:
          i === 0
            ? [
                { path: "/sessions/mock.jsonl", name: "mock", title: "并行会话演示", turns: 3, runtimeId: open.get("/sessions/mock.jsonl") },
                { path: "/sessions/older.jsonl", name: "older", title: "上一次的会话", turns: 12, runtimeId: open.get("/sessions/older.jsonl") },
              ]
            : [{ path: "/sessions/site.jsonl", name: "site", title: "站点改版", turns: 5, runtimeId: open.get("/sessions/site.jsonl") }],
      })),
    );
  }

  addWorkspace(path: string) {
    if (!this.roots.includes(path)) this.roots.push(path);
    return Promise.resolve<TreeWorkspace>({ root: path, name: path.split("/").pop() ?? path, remembered: true, sessions: [] });
  }

  async revealWorkspace() {}

  removeWorkspace(path: string) {
    const at = this.roots.indexOf(path);
    if (at >= 0) this.roots.splice(at, 1);
    return Promise.resolve();
  }

  moveWorkspace(path: string, direction: -1 | 1) {
    const at = this.roots.indexOf(path);
    const to = at + direction;
    if (at >= 0 && to >= 0 && to < this.roots.length) {
      [this.roots[at], this.roots[to]] = [this.roots[to], this.roots[at]];
    }
    return Promise.resolve();
  }

  removeSession(_path: string) {
    return Promise.resolve();
  }

  removeRemoteSession(_host: string, _path: string) {
    return Promise.resolve();
  }

  archiveSession(_path: string, _archived: boolean) {
    return Promise.resolve();
  }

  renameSession(_path: string, _title: string) {
    return Promise.resolve();
  }

  exportSession(path: string) {
    const name = path.split(/[/\\]/).at(-1)?.replace(/\.jsonl$/i, "") || "session";
    return Promise.resolve({ name, content: '{"role":"user","content":"mock session"}\n' });
  }

  importLegacySessions(_path: string, _workspace: string) {
    return Promise.resolve({ summary: "mock migration complete", imported: 0, warnings: 0, recognised: true });
  }

  hostCapabilities() {
    return Promise.resolve({ pickFolder: true, addWorkspace: true });
  }

  pickFolder() {
    return Promise.resolve<string | null>("~/projects/mock-workspace");
  }

  private share: ShareStatus = {
    open: false,
    addresses: [
      { interface: "Wi-Fi", ip: "192.168.1.20", kind: "lan" },
      { interface: "Tailscale", ip: "100.101.7.3", kind: "tailnet" },
      { interface: "vEthernet (WSL)", ip: "172.28.160.1", kind: "virtual" },
    ],
    devices: [],
    cloudDevices: [],
    cloudRemote: { online: false },
  };

  shareStatus() {
    return Promise.resolve<ShareStatus | null>({ ...this.share });
  }

  openShare(ip: string) {
    this.share = { ...this.share, open: true, origin: `http://${ip}:${this.share.port ?? 41234}`, devices: [] };
    return Promise.resolve({ ...this.share });
  }

  setSharePort(port: number) {
    this.share = { ...this.share, port: port || undefined };
    return Promise.resolve({ ...this.share });
  }

  closeShare() {
    this.share = { ...this.share, open: false, origin: undefined, devices: [], offerExpires: undefined };
    return Promise.resolve({ ...this.share });
  }

  offerShare() {
    const expires = new Date(Date.now() + 5 * 60_000).toISOString();
    this.share = { ...this.share, offerExpires: expires };
    const qr = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 29 29"><rect width="29" height="29" fill="#fff"/><path fill="#000" d="M4 4h7v7h-7zM18 4h7v7h-7zM4 18h7v7h-7zM13 13h3v3h-3z"/></svg>`;
    return Promise.resolve({ url: `${this.share.origin}/#pair=mock-code`, qr, expires });
  }

  offerCloudShare() {
    const qr = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 29 29"><rect width="29" height="29" fill="#fff"/><path fill="#000" d="M4 4h7v7h-7zM18 4h7v7h-7zM4 18h7v7h-7zM13 13h3v3h-3z"/></svg>`;
    return Promise.resolve({ url: "https://reasonix.io/remote/?device=mock-device", qr });
  }

  device() {
    return Promise.resolve(null);
  }

  leaveDevice() {
    return Promise.resolve();
  }

  revokeDevice(id: string) {
    this.share = {
      ...this.share,
      devices: this.share.devices.filter((d) => d.id !== id),
      cloudDevices: this.share.cloudDevices.filter((d) => d.id !== id),
    };
    return Promise.resolve({ ...this.share });
  }

  portFor(rt: RuntimeView): AgentPort {
    const held = this.ports.get(rt.id);
    if (held) return held;
    const port = new MockPort();
    port.machine = this.machine;
    this.ports.set(rt.id, port);
    return port;
  }
}
