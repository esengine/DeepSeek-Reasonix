// What is installed, what could be, and how far a download has got.
export interface VersionEntry {
  version: string;
  tag: string;
  publishedAt: string;
  current: boolean;
  older: boolean;
}

// err rides alongside the data: an unreachable catalog must not hide which
// version is running.
export interface VersionHub {
  current: string;
  pinned: string;
  stalePin: boolean;
  latest: string;
  newer: boolean;
  versions: VersionEntry[];
  err?: string;
}

// One report from an install in flight. received/total are meaningful only
// while downloading; verifying is the pause after the last byte, which is long
// enough on a large artifact that not naming it reads as a hang. ready is a
// verified release waiting for the user to allow the restart, applying the
// moment after they did. authorizing is the Linux package prompt, and idle is
// what a kernel that has installed nothing this launch answers rather than an
// empty body. code says why an error happened, as a dotted code.
export interface UpdateProgress {
  version: string;
  phase: "idle" | "downloading" | "verifying" | "downloaded" | "ready" | "applying" | "authorizing" | "relaunching" | "error";
  received: number;
  total: number;
  err?: string;
  code?: string;
  delta_skipped?: string;
}
