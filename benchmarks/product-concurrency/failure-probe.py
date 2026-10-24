import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile


def probe(binary):
    if os.name != "posix":
        raise RuntimeError("argv[0] fault injection requires a POSIX host")
    binary = Path(binary).resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="reasonix-product-failure-probe-") as temporary:
        root = Path(temporary)
        shim, output = root / "worker-probe", root / "partial.json"
        shim.write_text(
            "#!" + sys.executable + "\nimport json,subprocess,sys\n"
            'request=json.loads(sys.stdin.readline());request["Mode"]="invalid-fixture-policy"\n'
            "result=subprocess.run([" + repr(str(binary)) + ',"-worker"],'
            'input=json.dumps(request)+"\\n",text=True)\nraise SystemExit(result.returncode)\n'
        )
        shim.chmod(0o700)
        args = [str(shim), "-trials", "1", "-workers", "1", "-steps", "1",
                "-think", "1ms", "-output", str(output)]
        process = subprocess.Popen(args, executable=str(binary), text=True,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   start_new_session=True)
        try:
            stdout, stderr = process.communicate(timeout=30)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate()
            raise
        if process.returncode != 1:
            raise RuntimeError(f"expected preparation failure: {process.returncode}: {stdout} {stderr}")
        report = json.loads(output.read_text())
        if report.get("completed") is not False:
            raise RuntimeError("failed preparation was reported complete")
        for expected in ("worker preparation", "process exit: exit status 1", "invalid-fixture-policy"):
            if expected not in report.get("failure", ""):
                raise RuntimeError("preparation diagnostics missing: " + report.get("failure", ""))
        return {"preserved_preparation_error": True, "completed": False, "exit": process.returncode}


if __name__ == "__main__":
    print(json.dumps(probe(sys.argv[1])))
