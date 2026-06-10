from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path


def configure_stdout() -> None:
    try:
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except Exception:
        pass


def read_jsonl(path: Path) -> list[dict]:
    rows = []
    with path.open("r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def query_terms(query: str) -> list[str]:
    terms = []
    for term in re.findall(r"[\w./:-]+", query.lower(), flags=re.UNICODE):
        term = term.strip()
        if len(term) >= 2 and term not in terms:
            terms.append(term)
    return terms or [query.lower().strip()]


def keyword_score(chunk: dict, terms: list[str], query: str) -> float:
    title = str(chunk.get("title", "")).lower()
    source = str(chunk.get("relative_path", "")).lower()
    text = str(chunk.get("text", "")).lower()
    score = 0.0
    for term in terms:
        if not term:
            continue
        score += 8.0 * title.count(term)
        score += 3.0 * source.count(term)
        score += text.count(term)
    if query.lower() in text:
        score += 10.0
    return score


def search_knowledge_db(db_dir: Path, query: str, limit: int, snippet_chars: int) -> None:
    manifest = json.loads((db_dir / "manifest.json").read_text(encoding="utf-8"))
    chunks = read_jsonl(db_dir / manifest.get("chunk_file", "chunks.jsonl"))
    terms = query_terms(query)
    scored = [
        (keyword_score(chunk, terms, query), index)
        for index, chunk in enumerate(chunks)
    ]
    order = [
        index
        for score, index in sorted(scored, key=lambda item: item[0], reverse=True)[:limit]
        if score > 0
    ]

    print(f"query: {query}")
    print(f"db: {db_dir}")
    print("search: keyword-jsonl")
    print(f"documents: {manifest.get('document_count')}")
    print(f"chunks: {manifest.get('chunk_count')}")
    if not order:
        print("\nNo positive keyword matches.")
        return

    for rank, index in enumerate(order, 1):
        chunk = chunks[index]
        score = keyword_score(chunk, terms, query)
        snippet = re.sub(r"\s+", " ", chunk.get("text", ""))[:snippet_chars]
        print(f"\n{rank}. score={score:.2f} {chunk.get('title', '')}")
        print(f"   source={chunk.get('relative_path', '')}")
        if chunk.get("source_url"):
            print(f"   url={chunk.get('source_url')}")
        if chunk.get("origin_db"):
            print(f"   origin_db={chunk.get('origin_db')}")
        print(f"   {snippet}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Search the GEEMu local knowledge database.")
    sub = parser.add_subparsers(dest="command", required=True)
    search_parser = sub.add_parser("search")
    search_parser.add_argument("query")
    search_parser.add_argument("--db", default="gee_vector_db")
    search_parser.add_argument("--limit", type=int, default=8)
    search_parser.add_argument("--snippet-chars", type=int, default=320)
    search_parser.add_argument("--batch-size", type=int, default=16, help=argparse.SUPPRESS)
    return parser.parse_args()


def main() -> int:
    configure_stdout()
    args = parse_args()
    if args.command == "search":
        search_knowledge_db(Path(args.db).resolve(), args.query, args.limit, args.snippet_chars)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
