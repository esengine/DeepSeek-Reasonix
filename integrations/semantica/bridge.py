"""Bounded stdin/stdout bridge for intelifar's optional Semantica integration."""

from __future__ import annotations

import contextlib
import hashlib
import io
import json
import pathlib
import re
import sys
from typing import Any

CONTRACT_VERSION = 1
EXPECTED_VERSION = "0.6.0"
MAX_INPUT_BYTES = 1024 * 1024
MAX_ASSETS = 100
MAX_EVIDENCE = 20


def _text(value: Any, limit: int) -> str:
    return str(value or "").strip()[:limit]


def _score(value: Any) -> float:
    try:
        return max(0.0, min(1.0, float(value)))
    except (TypeError, ValueError):
        return 0.0


def _asset(value: Any) -> dict[str, Any] | None:
    if not isinstance(value, dict):
        return None
    asset_id = _text(value.get("id"), 120)
    title = _text(value.get("title"), 240)
    if not asset_id or not title:
        return None
    document = value.get("document") if isinstance(value.get("document"), dict) else {}
    evidence = value.get("evidence") if isinstance(value.get("evidence"), list) else []
    return {
        "id": asset_id,
        "title": title,
        "type": _text(value.get("type"), 100),
        "summary": _text(value.get("summary"), 1200),
        "owner": _text(value.get("owner"), 120),
        "sensitivity": _text(value.get("sensitivity"), 40),
        "confidence": _score(value.get("confidence")),
        "tags": [_text(tag, 80) for tag in (value.get("tags") or [])[:20] if _text(tag, 80)],
        "document": {
            "title": _text(document.get("title"), 240),
            "sourceName": _text(document.get("sourceName"), 240),
            "sha256": _text(document.get("sha256"), 64),
        },
        "evidence": [
            {
                "id": _text(item.get("id"), 120),
                "section": _text(item.get("section"), 200),
                "locator": _text(item.get("locator"), 200),
                "sha256": _text(item.get("sha256"), 64),
            }
            for item in evidence[:MAX_EVIDENCE]
            if isinstance(item, dict) and _text(item.get("id"), 120)
        ],
    }


def _identity(title: str) -> str:
    return re.sub(r"[^0-9a-z\u4e00-\u9fff]+", "", title.casefold())


def _reason_label(reason: str) -> str:
    if reason == "exact_name_match":
        return "标题完全一致"
    if reason == "same_type":
        return "资产类型一致"
    match = re.fullmatch(r"(\d+)_property_matches", reason)
    return f"{match.group(1)} 项属性相近" if match else "多项内容相近"


def _load_semantica():
    with contextlib.redirect_stdout(sys.stderr):
        import semantica
        from semantica.conflicts import ConflictDetector
        from semantica.deduplication import DuplicateDetector
        from semantica.provenance import ProvenanceManager

    if getattr(semantica, "__version__", None) != EXPECTED_VERSION:
        raise RuntimeError(f"Semantica version must be {EXPECTED_VERSION}")
    return semantica, DuplicateDetector, ConflictDetector, ProvenanceManager


def status() -> dict[str, Any]:
    semantica, _, _, _ = _load_semantica()
    return {
        "contractVersion": CONTRACT_VERSION,
        "status": "ready",
        "engine": "Semantica",
        "version": semantica.__version__,
        "capabilities": ["duplicates", "conflicts", "provenance"],
    }


def enrich(raw_assets: Any) -> dict[str, Any]:
    semantica, DuplicateDetector, ConflictDetector, ProvenanceManager = _load_semantica()
    assets = [item for item in (_asset(value) for value in (raw_assets if isinstance(raw_assets, list) else [])[:MAX_ASSETS]) if item]
    entities = [
        {
            "id": asset["id"],
            "name": asset["title"],
            "type": asset["type"] or "未分类",
            "properties": {"summary": asset["summary"], "owner": asset["owner"], "sensitivity": asset["sensitivity"]},
            "relationships": asset["tags"],
        }
        for asset in assets
    ]
    with contextlib.redirect_stdout(sys.stderr):
        detector = DuplicateDetector(similarity_threshold=0.78, confidence_threshold=0.68, max_results=50, top_k_per_entity=5, min_similarity=0.78)
        candidates = detector.detect_duplicates(entities)
    duplicates = [
        {
            "assetIds": [str(candidate.entity1.get("id")), str(candidate.entity2.get("id"))],
            "similarity": round(float(candidate.similarity_score), 6),
            "confidence": round(min(1.0, float(candidate.confidence)), 6),
            "reasons": [_reason_label(str(reason)) for reason in candidate.reasons[:8]],
        }
        for candidate in candidates
    ]

    exact_groups: dict[str, list[dict[str, Any]]] = {}
    for asset in assets:
        key = _identity(asset["title"])
        if key:
            exact_groups.setdefault(key, []).append(asset)
    conflict_entities: list[dict[str, Any]] = []
    group_titles: dict[str, str] = {}
    for key, group in exact_groups.items():
        if len(group) < 2:
            continue
        group_id = "GROUP-" + hashlib.sha256(key.encode("utf-8")).hexdigest()[:16]
        group_titles[group_id] = group[0]["title"]
        for asset in group:
            conflict_entities.append({
                "id": group_id,
                "owner": asset["owner"],
                "sensitivity": asset["sensitivity"],
                "type": asset["type"],
                "source": asset["document"]["sourceName"] or asset["document"]["title"] or asset["id"],
                "confidence": asset["confidence"] or 1.0,
                "metadata": {"assetId": asset["id"]},
            })
    conflicts: list[dict[str, Any]] = []
    if conflict_entities:
        with contextlib.redirect_stdout(sys.stderr):
            conflict_detector = ConflictDetector(confidence_threshold=0.7, auto_resolve=False)
            detected = []
            for field in ("owner", "sensitivity", "type"):
                detected.extend(conflict_detector.detect_value_conflicts(conflict_entities, field))
        for conflict in detected:
            values = [str(value) for value in conflict.conflicting_values if value is not None]
            sources = []
            for index, source in enumerate(conflict.sources):
                metadata = source.get("metadata") if isinstance(source.get("metadata"), dict) else {}
                sources.append({
                    "assetId": _text(metadata.get("assetId"), 120),
                    "document": _text(source.get("document"), 240),
                    "value": values[index] if index < len(values) else "",
                })
            conflicts.append({
                "group": conflict.entity_id,
                "title": group_titles.get(conflict.entity_id or "", "同名资产"),
                "field": conflict.property_name,
                "severity": conflict.severity,
                "confidence": round(float(conflict.confidence), 6),
                "values": list(dict.fromkeys(values)),
                "sources": sources,
            })

    with contextlib.redirect_stdout(sys.stderr):
        provenance_manager = ProvenanceManager()
        provenance_entries = []
        evidence_total = 0
        for asset in assets:
            source = asset["document"]["sourceName"] or asset["document"]["title"] or asset["id"]
            entry = provenance_manager.track_entity(
                asset["id"],
                source,
                metadata={"assetId": asset["id"], "evidenceCount": len(asset["evidence"])},
                source_location="已发布资产记录",
                confidence=asset["confidence"] or 1.0,
            )
            for evidence in asset["evidence"]:
                provenance_manager.track_entity(
                    evidence["id"],
                    asset["id"],
                    metadata={"assetId": asset["id"], "evidenceId": evidence["id"]},
                    parent_entity_id=asset["id"],
                    source_location=evidence["locator"] or evidence["section"],
                    confidence=asset["confidence"] or 1.0,
                )
            evidence_total += len(asset["evidence"])
            provenance_entries.append({
                "assetId": asset["id"],
                "source": source,
                "checksum": entry.checksum or "",
                "evidenceCount": len(asset["evidence"]),
            })

    return {
        "contractVersion": CONTRACT_VERSION,
        "status": "complete",
        "engine": "Semantica",
        "version": semantica.__version__,
        "checkedAssets": len(assets),
        "duplicates": duplicates,
        "conflicts": conflicts,
        "provenance": {"assets": len(assets), "evidence": evidence_total, "entries": provenance_entries},
    }


def main() -> int:
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8")
    if len(sys.argv) != 2:
        raise ValueError("A single Semantica source path is required")
    source_path = pathlib.Path(sys.argv[1]).resolve()
    if not source_path.is_dir():
        raise ValueError("Semantica source path is unavailable")
    sys.path.insert(0, str(source_path))
    raw = sys.stdin.buffer.read(MAX_INPUT_BYTES + 1)
    if len(raw) > MAX_INPUT_BYTES:
        raise ValueError("Bridge input exceeds limit")
    request = json.loads(raw.decode("utf-8") or "{}")
    if request.get("contractVersion") != CONTRACT_VERSION:
        raise ValueError("Unsupported bridge contract")
    action = request.get("action")
    result = status() if action == "status" else enrich(request.get("assets")) if action == "enrich" else None
    if result is None:
        raise ValueError("Unsupported bridge action")
    sys.stdout.write(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:  # Keep failures on stderr; stdout remains a single valid result channel.
        sys.stderr.write(f"Semantica bridge failed: {type(error).__name__}\n")
        raise SystemExit(1)
