export interface IsolationResult {
  id: string;
  kind: string;
  resultId?: string;
  owner?: string;
  parentId?: string;
  targetWorkspace?: string;
  generation?: string;
  state: string;
  activity: string;
  candidateRoot?: string;
  baselineTree?: string;
  tree?: string;
  changes: { Status: string; Path: string }[];
  verification: string;
  cleanupComplete: boolean;
  reason?: string;
  actions: string[];
  recovery?: IsolationRecoveryTarget;
}

export interface IsolationDiagnostic {
  record: string;
  reason: string;
}

export interface IsolationCatalog {
  rootScene?: RootSceneSummary;
  enabled: boolean;
  results: IsolationResult[];
  diagnostics: IsolationDiagnostic[];
  recoveryRequired: boolean;
  recoveryReason?: string;
  recovery?: IsolationRecoveryTarget;
}

export interface RootSceneSummary {
  recovery?: IsolationRecoveryTarget;
  environmentId: string;
  generation: string;
  runtime: string;
  owner: string;
  sourceWorkspace: string;
  liveWorkspace: string;
  publicationId: string;
  resultId: string;
  state: string;
  verification: string;
  actions: string[];
}

export interface RootSceneRequest {
  revision?: string;
  environmentId: string;
  generation: string;
  runtime: string;
  owner: string;
  publicationId: string;
  action: string;
}

export interface IsolationRecoveryTarget {
  resultId?: string;
  workspace: string;
  runtime: string;
  revision: string;
  paths: string[];
}

export interface IsolationRecoveryRequest {
  resultId?: string;
  workspace: string;
  runtime: string;
  revision: string;
}

export interface IsolatedDiscardPreview {
  id: string;
  revision: string;
  candidateRoot: string;
  entries: number;
  state: string;
}

export interface IsolatedDiscardRequest {
  id: string;
  revision: string;
}

export interface IsolationPort {
  manageRootScene(request: RootSceneRequest): Promise<void>;
  isolatedResults(): Promise<IsolationCatalog>;
  previewIsolatedDiscard(id: string): Promise<IsolatedDiscardPreview>;
  discardIsolated(request: IsolatedDiscardRequest): Promise<void>;
  recoverIsolatedIntegration(target: IsolationRecoveryRequest): Promise<void>;
}
