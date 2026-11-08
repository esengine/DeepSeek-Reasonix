import http.server
import json
import os
from pathlib import Path
import queue
import subprocess
import tempfile
import threading
import urllib.error
import urllib.parse
import urllib.request


class Provider(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"data":[{"id":"smoke-model","object":"model"}]}')

    def log_message(self, *_args):
        pass


android = Path(__file__).resolve().parents[1]
kernel = Path(os.environ.get("KERNEL_BIN", android.parent / "reasonix-verify.exe")).resolve()
page = android / "app/src/main/assets/page"
if not kernel.is_file() or not (page / "index.html").is_file():
    raise SystemExit("Build ./cmd/reasonix-studio-host as KERNEL_BIN and run sync-page.sh first.")

with tempfile.TemporaryDirectory(prefix="reasonix-android-host-") as temporary:
    home = Path(temporary) / "home"
    workspace = Path(temporary) / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / "config.toml").write_text("providers = []\n", encoding="utf-8")
    env = {**os.environ, "REASONIX_HOME": str(home), "REASONIX_STATE_HOME": str(home),
           "REASONIX_CREDENTIALS_STORE": "file", "HOME": str(home)}
    logs = open(Path(temporary) / "host.log", "w", encoding="utf-8")
    child = subprocess.Popen([str(kernel), "-page", str(page)], cwd=workspace, env=env,
                             stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=logs, text=True)
    provider = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Provider)
    threading.Thread(target=provider.serve_forever, daemon=True).start()
    try:
        announced = queue.Queue()
        threading.Thread(target=lambda: announced.put(child.stdout.readline()), daemon=True).start()
        connection = json.loads(announced.get(timeout=30))
        origin = connection["origin"]
        address = urllib.parse.urlsplit(origin)
        assert connection["version"] == 1 and address.hostname == "127.0.0.1" and address.scheme == "http"

        def request(path, body=None, authenticated=True, request_origin=None):
            headers = {"Content-Type": "application/json", "Origin": request_origin or origin}
            if authenticated:
                headers["Cookie"] = "reasonix_token=" + connection["token"]
            payload = None if body is None else json.dumps(body).encode()
            req = urllib.request.Request(origin + path, data=payload, headers=headers)
            try:
                response = urllib.request.urlopen(req, timeout=30)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                raw = response.read()
                return response.status, json.loads(raw) if "json" in response.headers.get("Content-Type", "") else raw

        assert request("/runtimes", authenticated=False)[0] == 403
        assert request("/runtimes", request_origin="https://untrusted.invalid")[0] == 403
        assert request("/")[0] == 200
        status, runtimes = request("/runtimes")
        assert status == 200
        entries = runtimes if isinstance(runtimes, list) else runtimes["runtimes"]
        route = "/rt/" + entries[0]["id"]
        assert request(route + "/provider-setup")[1]["required"]
        endpoint = "http://127.0.0.1:" + str(provider.server_port)
        status, probe = request(route + "/providers/probe", {"baseUrl": endpoint, "apiKey": "sk-smoke"})
        assert status == 200 and "smoke-model" in probe["models"], (status, probe)
        status, saved = request(route + "/providers", {
            "name": "smoke", "kind": "openai", "baseUrl": endpoint, "apiKey": "sk-smoke",
            "models": ["smoke-model"], "default": "smoke-model",
        })
        assert status == 200, (status, saved)
        status, setup = request(route + "/provider-setup")
        assert status == 404 or not setup.get("required"), (status, setup)
        print("Android host: page, authentication, origin guard, provider probe, save and activation passed.")
    finally:
        child.stdin.close()
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
        child.stdout.close()
        logs.close()
        provider.shutdown()
        provider.server_close()
