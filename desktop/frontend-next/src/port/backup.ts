// Configuration backups kept in the signed-in account. The kernel seals each
// one with the person's passphrase before it leaves; the service stores only
// the envelope and the metadata listed here.
export type BackupCategory = "settings" | "extensions" | "memory" | "automation" | "secrets";

export interface BackupCategoryInfo {
  id: BackupCategory;
  defaultOn: boolean;
  consent: boolean;
}

export interface BackupEntry {
  id: string;
  label: string;
  format: number;
  appVersion: string;
  platform: string;
  categories: BackupCategory[];
  ciphertextBytes: number;
  createdAt: string;
}

export interface BackupCatalog {
  categories: BackupCategoryInfo[];
  backups: BackupEntry[];
  limits: { maxCount: number; maxBytes: number };
  minPassphrase: number;
}

export interface BackupOmission {
  kind: string;
  name: string;
  reason: string;
}

export interface BackupCreateRequest {
  label: string;
  categories: BackupCategory[];
  passphrase: string;
  appVersion?: string;
}

export interface BackupCreated {
  backup: BackupEntry;
  omitted?: BackupOmission[];
}

export interface BackupPathRef {
  path: string;
  exists: boolean;
}

// consent is set when restoring the item runs code on this machine, sends
// conversations or a stored key somewhere new, replaces a stored key, or
// imports other files into the standing instructions. The kernel refuses such an item unless it is
// named again in consented, whatever the selection said.
export interface BackupPlanItem {
  id: string;
  category: BackupCategory;
  kind: string;
  name: string;
  status: "new" | "changed" | "same" | "install";
  consent?: "executes" | "endpoint" | "replaces_secret" | "imports";
  summary?: string;
  previous?: string;
  paths?: BackupPathRef[];
  crossPlatform?: boolean;
  files?: number;
  details?: string[];
  content?: string;
  recommended: boolean;
}

export interface BackupPlan {
  planId: string;
  createdAt: string;
  appVersion?: string;
  platform: string;
  samePlatform: boolean;
  categories: BackupCategory[];
  items: BackupPlanItem[];
  omitted?: BackupOmission[];
}

export interface BackupPluginRef {
  name: string;
  source: string;
  version?: string;
  commit?: string;
}

export interface BackupApplyResult {
  applied: string[];
  plugins?: BackupPluginRef[];
  failed?: { id: string; error: string }[];
  reloadError?: string;
}
