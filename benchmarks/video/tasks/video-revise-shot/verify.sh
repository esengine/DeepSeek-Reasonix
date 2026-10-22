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
s02 = current(doc, "s02")
assert s02["version"] == 2, "s02 current version %r, want 2" % s02["version"]
assert any(a.get("shot_id") == "s02" and a.get("version") == 1 for a in doc["artifacts"]), "the superseded s02 v1 was dropped"
assert doc.get("x_seed_note") == "must survive every edit", "an unknown manifest field was dropped"
for s, want in {'s01': '5ff207395e93b5ef1a5ab5fda3112617689c772ee82f830e51c790900b321260', 's03': 'f1cbf3c1c48b2367a81c3abc2fc8c607d1a37aea235af902f9258cb2a0377087'}.items():
    a = current(doc, s)
    assert a["version"] == 1 and sha256(a["path"]) == want, "shot %s changed" % s
subs = submissions("harbour")
assert [e["shot_id"] for e in subs] == ["s02"], "expected exactly one submission, for s02: %r" % subs
PY
