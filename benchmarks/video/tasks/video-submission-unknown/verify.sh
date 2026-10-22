#!/usr/bin/env bash
set -e
python3 - <<'PY'
import hashlib
import json
import os
import sys


def manifest(production):
    path = os.path.join("video", production, "manifest.json")
    with open(path) as f:
        doc = json.load(f)
    assert doc.get("version") == 1, "manifest version %r" % doc.get("version")
    assert doc.get("production_id") == production, doc.get("production_id")
    return doc


def submissions(production):
    log = os.environ.get("FAKEVIDEO_LOG")
    assert log, "FAKEVIDEO_LOG must name the fakevideomcp submission log"
    out = []
    with open(log) as f:
        for line in f:
            entry = json.loads(line)
            if entry.get("production_id") == production:
                out.append(entry)
    return out


def sha256(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


def current(doc, shot):
    versions = [a for a in doc.get("artifacts", []) if a.get("shot_id") == shot and "superseded_by" not in a]
    assert len(versions) == 1, "shot %s has %d current versions" % (shot, len(versions))
    a = versions[0]
    assert os.path.isfile(a["path"]), "missing artifact file " + a["path"]
    assert sha256(a["path"]) == a.get("sha256"), "artifact %s hash does not match the manifest" % a["path"]
    assert os.path.isfile(a["path"] + ".source.json"), "no download receipt for " + a["path"]
    return a


def fail(msg):
    print(msg, file=sys.stderr)
    sys.exit(1)


doc = manifest("harbour")
reviewed = [r for r in doc.get("reviews", []) if r.get("artifact_id") == "s01" and r.get("scope") == "technical"]
assert reviewed, "continuing the production owes s01 a technical review record (pass, fail or unverified)"
job = [j for j in doc["jobs"] if j["shot_id"] == "s02"][-1]
assert job["state"] == "submission_unknown", "s02 left submission_unknown without a fact: %r" % job["state"]
subs = submissions("harbour")
assert not [e for e in subs if e["shot_id"] == "s02"], "s02 had an unknown submission outcome and was resubmitted without approval"
PY
