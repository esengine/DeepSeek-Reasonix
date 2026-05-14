#!/usr/bin/env node
// Pushes assets in $ASSETS_DIR to a Gitee Release matching $RELEASE_TAG, creating it if needed.

import { readFile, readdir, stat } from "node:fs/promises";
import path from "node:path";

const GITEE_API = "https://gitee.com/api/v5";

function envOrDie(name) {
  const v = process.env[name];
  if (!v) throw new Error(`missing env: ${name}`);
  return v;
}

const TOKEN = envOrDie("GITEE_TOKEN");
const REPO = envOrDie("GITEE_REPO");
const TAG = envOrDie("RELEASE_TAG");
const ASSETS_DIR = envOrDie("ASSETS_DIR");
const RELEASE_NAME = process.env.RELEASE_NAME ?? `Reasonix ${TAG}`;
const RELEASE_BODY = process.env.RELEASE_BODY ?? `Mirror of GitHub Release ${TAG}.`;
const TARGET_BRANCH = process.env.GITEE_TARGET_BRANCH ?? "main";

async function giteeFetch(p, init = {}) {
  const res = await fetch(`${GITEE_API}${p}`, {
    ...init,
    headers: { ...(init.headers ?? {}), Authorization: `token ${TOKEN}` },
  });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`Gitee ${init.method ?? "GET"} ${p} → ${res.status}: ${body}`);
  }
  return res;
}

async function getOrCreateRelease() {
  const lookup = await fetch(
    `${GITEE_API}/repos/${REPO}/releases/tags/${encodeURIComponent(TAG)}`,
    { headers: { Authorization: `token ${TOKEN}` } },
  );
  if (lookup.status === 200) {
    // Gitee returns 200 + literal `null` body when the tag has no release yet,
    // not 404 like a sane API would. Fall through to create in that case.
    const data = await lookup.json().catch(() => null);
    if (data && data.id) {
      console.log(`Gitee: release ${TAG} exists (id=${data.id}), reusing.`);
      return data;
    }
  } else if (lookup.status !== 404) {
    const body = await lookup.text().catch(() => "");
    throw new Error(`Gitee lookup failed: ${lookup.status}: ${body}`);
  }
  const res = await giteeFetch(`/repos/${REPO}/releases`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      tag_name: TAG,
      name: RELEASE_NAME,
      body: RELEASE_BODY,
      prerelease: false,
      target_commitish: TARGET_BRANCH,
    }),
  });
  const data = await res.json();
  console.log(`Gitee: created release ${TAG} (id=${data.id}).`);
  return data;
}

async function existingAssetNames(releaseId) {
  const res = await fetch(`${GITEE_API}/repos/${REPO}/releases/${releaseId}`, {
    headers: { Authorization: `token ${TOKEN}` },
  });
  if (!res.ok) return new Set();
  const data = await res.json();
  return new Set((data.assets ?? []).map((a) => a.name));
}

async function uploadAsset(releaseId, filePath) {
  const filename = path.basename(filePath);
  const buf = await readFile(filePath);
  const form = new FormData();
  form.append("file", new Blob([buf]), filename);
  const res = await fetch(`${GITEE_API}/repos/${REPO}/releases/${releaseId}/attach_files`, {
    method: "POST",
    headers: { Authorization: `token ${TOKEN}` },
    body: form,
  });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`upload ${filename} → ${res.status}: ${body}`);
  }
  console.log(`Gitee: uploaded ${filename} (${buf.byteLength} bytes)`);
}

const release = await getOrCreateRelease();
const skip = await existingAssetNames(release.id);
for (const name of await readdir(ASSETS_DIR)) {
  const fp = path.join(ASSETS_DIR, name);
  const st = await stat(fp);
  if (!st.isFile()) continue;
  if (skip.has(name)) {
    console.log(`Gitee: skip ${name} (already present)`);
    continue;
  }
  await uploadAsset(release.id, fp);
}
console.log("Gitee mirror complete.");
