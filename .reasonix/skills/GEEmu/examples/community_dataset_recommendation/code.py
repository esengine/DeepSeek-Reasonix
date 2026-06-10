from __future__ import annotations

import csv
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CSV_PATH = ROOT / "awesome-gee-community-datasets" / "community_datasets.csv"
OUTPUT_PATH = Path(__file__).with_name("community_candidates.csv")

SEARCH_TERMS = ["precipitation", "rainfall", "climate"]
SEARCH_FIELDS = ["title", "tags", "thematic_group", "provider", "type"]
RETURN_FIELDS = [
    "id",
    "provider",
    "title",
    "type",
    "tags",
    "sample_code",
    "license",
    "docs_page",
    "thematic_group",
]
LIMIT = 15


def score_row(row: dict[str, str], terms: list[str]) -> int:
    score = 0
    for field in SEARCH_FIELDS:
        text = row.get(field, "").lower()
        for term in terms:
            if term.lower() in text:
                score += 3 if field in {"title", "tags"} else 1
    return score


def find_candidates() -> list[dict[str, str]]:
    if not CSV_PATH.exists():
        raise FileNotFoundError(f"Missing community dataset table: {CSV_PATH}")

    terms = [term.lower() for term in SEARCH_TERMS]
    candidates: list[tuple[int, dict[str, str]]] = []

    with CSV_PATH.open("r", encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            score = score_row(row, terms)
            if score > 0:
                candidates.append((score, row))

    candidates.sort(key=lambda item: (-item[0], item[1].get("title", "")))
    return [
        {"score": str(score), **{field: row.get(field, "") for field in RETURN_FIELDS}}
        for score, row in candidates[:LIMIT]
    ]


def write_candidates(rows: list[dict[str, str]]) -> None:
    fieldnames = ["score", *RETURN_FIELDS]
    with OUTPUT_PATH.open("w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)


def main() -> None:
    rows = find_candidates()
    write_candidates(rows)

    print(f"Wrote {len(rows)} candidates to {OUTPUT_PATH}")
    for row in rows[:5]:
        print(f"[{row['score']}] {row['id']} - {row['title']} ({row['license']})")


if __name__ == "__main__":
    main()
