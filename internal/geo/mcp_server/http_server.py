"""极简 HTTP Server — 随机端口，仅 bind 127.0.0.1，serve 预览图 + GeoJSON。

目的：MCP 工具结果只返回 URL，数据不经过 LLM token 窗口。
参照迁移方案 4.2 节 — HTTP Server + URL 模式。
"""

import json
import logging
import os
import threading
import uuid
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)

# ── 全局状态 ──────────────────────────────────────────────────────

_server: Optional[HTTPServer] = None
_port: int = 0
_lock = threading.Lock()

# 内存缓存：uuid → (content_type, bytes_data)
_cache: dict[str, tuple[str, bytes]] = {}

# 最多缓存条目（LRU 淘汰最旧的）
_MAX_CACHE = 20
_cache_order: list[str] = []


def _add_cache(content_type: str, data: bytes) -> str:
    """存入缓存，返回 uuid。"""
    uid = uuid.uuid4().hex

    with _lock:
        if len(_cache) >= _MAX_CACHE:
            oldest = _cache_order.pop(0)
            _cache.pop(oldest, None)

        _cache[uid] = (content_type, data)
        _cache_order.append(uid)

    return uid


def _get_cache(uid: str) -> tuple[str, bytes] | None:
    """从缓存获取。不更新 LRU 顺序。"""
    with _lock:
        return _cache.get(uid)


# ── Request Handler ────────────────────────────────────────────────

class _Handler(BaseHTTPRequestHandler):
    """极简 handler — 不支持目录列表，只做内容 serve。"""

    def log_message(self, fmt, *args):
        logger.debug("HTTP %s", fmt % args)

    def do_GET(self):
        path = self.path.rstrip("/")

        # /preview/{uuid}.webp
        if path.startswith("/preview/") and path.endswith(".webp"):
            uid = path[len("/preview/"):-len(".webp")]
            entry = _get_cache(uid)
            if entry:
                content_type, data = entry
                self.send_response(200)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(data)))
                self.send_header("Cache-Control", "no-cache")
                self.send_header("Access-Control-Allow-Origin", "*")
                self.end_headers()
                self.wfile.write(data)
                return

        # /geojson/{uuid}
        elif path.startswith("/geojson/"):
            uid = path[len("/geojson/"):].split("/")[0]
            entry = _get_cache(uid)
            if entry:
                _, data = entry
                self.send_response(200)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(data)))
                self.send_header("Cache-Control", "no-cache")
                self.send_header("Access-Control-Allow-Origin", "*")
                self.end_headers()
                self.wfile.write(data)
                return

        # /health
        elif path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"ok")
            return

        # 404
        self.send_response(404)
        self.end_headers()
        self.wfile.write(b"not found")


# ── 生命周期 ──────────────────────────────────────────────────────

def start() -> int:
    """启动 HTTP Server（随机端口），返回实际端口号。幂等。"""
    global _server, _port

    if _server is not None:
        return _port

    server = HTTPServer(("127.0.0.1", 0), _Handler)
    _port = server.server_address[1]
    _server = server

    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    logger.info("preview HTTP server started on http://127.0.0.1:%d", _port)
    return _port


def stop():
    """停止 HTTP Server。"""
    global _server
    if _server:
        _server.shutdown()
        _server = None
        logger.info("preview HTTP server stopped")


def port() -> int:
    """返回当前端口号（0 表示未启动）。"""
    return _port


# ── 内容存储 ──────────────────────────────────────────────────────

def store_webp(data: bytes) -> str:
    """存储 WebP 预览图，返回 URL 路径片段。"""
    uid = _add_cache("image/webp", data)
    return f"/preview/{uid}.webp"


def store_geojson(data: dict | str) -> str:
    """存储 GeoJSON 内容，返回 URL 路径片段。"""
    if isinstance(data, dict):
        raw = json.dumps(data, ensure_ascii=False).encode("utf-8")
    else:
        raw = data.encode("utf-8")
    uid = _add_cache("application/json", raw)
    return f"/geojson/{uid}"


def url_for(path: str) -> str:
    """拼接完整 URL。"""
    return f"http://127.0.0.1:{_port}{path}"


def cache_size() -> int:
    """当前缓存条目数。"""
    with _lock:
        return len(_cache)
