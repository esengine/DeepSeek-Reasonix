import concurrent.futures
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time


def probe(executable):
    with tempfile.TemporaryDirectory(prefix="reasonix-product-cancel-") as directory:
        root = Path(directory)
        home, workspace, sessions = [root / name for name in ("home", "workspace", "sessions")]
        for path in (home, workspace, sessions):
            path.mkdir()
        (home / "config.toml").write_text("[agent]\ncollaboration='shared'\n[codegraph]\nenabled=false\n")
        environment = dict(os.environ, REASONIX_HOME=str(home), REASONIX_STATE_HOME=str(home),
                           REASONIX_CACHE_HOME=str(home / "cache"))
        process = subprocess.Popen([str(Path(executable).resolve()), "-worker"], cwd=workspace,
                                   env=environment, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            request = dict(Home=str(home), Root=str(workspace), Sessions=str(sessions), Scope="named", Mode="shared", ID=0,
                           Load=dict(workers=1, steps=1, think_ns=60_000_000_000, record_locks=False))
            process.stdin.write((json.dumps(request) + "\n").encode())
            process.stdin.flush()
            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
                pending = executor.submit(process.stdout.readline)
                try:
                    ready = pending.result(timeout=120)
                except concurrent.futures.TimeoutError:
                    process.kill()
                    raise RuntimeError("worker did not finish actual Controller preparation")
            if json.loads(ready) != "ready":
                raise RuntimeError("missing Controller admission barrier")
            process.stdin.write(b'"start"\n')
            process.stdin.flush()
            started = time.monotonic()
            process.stdin.close()
            try:
                code = process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                raise RuntimeError("control-pipe EOF did not cancel a worker before its 60-second provider wait")
            if code == 0 or (workspace / "worker-0-step-0.txt").exists():
                raise RuntimeError("cancelled worker completed or executed its queued mutation")
            return dict(cancelled=True, exit_code=code, cleanup_ms=(time.monotonic() - started) * 1000)
        finally:
            if process.poll() is None:
                process.kill()
            process.wait()
            process.stdout.close()
            process.stderr.close()


if __name__ == "__main__":
    print(json.dumps(probe(sys.argv[1])))
