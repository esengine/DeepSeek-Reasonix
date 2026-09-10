import { readFileSync } from "node:fs";
import type { ContractInfo } from "../shared/ipc.js";

export interface LoadedContract extends ContractInfo {
  readonly commandSet: ReadonlySet<string>;
}

function commandNames(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.flatMap((entry) => {
      if (typeof entry === "string") return [entry];
      if (entry && typeof entry === "object" && typeof (entry as { name?: unknown }).name === "string") {
        return [(entry as { name: string }).name];
      }
      return [];
    });
  }
  if (value && typeof value === "object") return Object.keys(value as Record<string, unknown>);
  return [];
}

export function parseContract(json: unknown): LoadedContract {
  if (!json || typeof json !== "object") throw new Error("contract must be a JSON object");
  const record = json as Record<string, unknown>;
  const digest = typeof record.digest === "string" ? record.digest : "";
  if (digest === "") throw new Error("contract has no digest");
  const commands = commandNames(record.commands).filter((name) => name !== "");
  if (commands.length === 0) throw new Error("contract lists no commands");
  const protocolVersion = typeof record.protocolVersion === "number" ? record.protocolVersion : 1;
  return { protocolVersion, digest, commands: Object.freeze([...commands]), commandSet: new Set(commands) };
}

export function emptyContract(): LoadedContract {
  return { protocolVersion: 1, digest: "", commands: Object.freeze([]), commandSet: new Set() };
}

export function loadContract(path: string): LoadedContract {
  return parseContract(JSON.parse(readFileSync(path, "utf8")) as unknown);
}

export function isAllowedCommand(contract: LoadedContract, method: unknown): method is string {
  return typeof method === "string" && method !== "" && contract.commandSet.has(method);
}
