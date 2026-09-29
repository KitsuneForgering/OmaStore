#!/usr/bin/env python3
"""Compare the skill's Python validator with `omastore lint-manifest`.

For each manifest in tests/manifests/, both must agree on the exit code and on
the fields with errors. The file name prefix states the expectation:
ok-* (no error nor warning), warning-* (warnings only), error-* (at least one
error).

Usage: compare_validators.py <path to the omastore binary>
"""
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
VALIDATOR = os.path.join(HERE, "..", "omastore-manifest", "scripts", "validate_manifest.py")
LINE = re.compile(r"^(error|warning): ([^:]+): ")


def run(cmd):
    p = subprocess.run(cmd, capture_output=True, text=True)
    out = p.stdout + p.stderr
    # The CLI summary line ("error: manifest has errors: N") is not a field.
    fields = {(m.group(1), m.group(2)) for m in map(LINE.match, out.splitlines())
              if m and m.group(2) != "manifest has errors"}
    return p.returncode, fields, out


def main():
    omastore = sys.argv[1]
    all_ok = True
    for name in sorted(os.listdir(os.path.join(HERE, "manifests"))):
        path = os.path.join(HERE, "manifests", name)
        go_code, go_fields, go_out = run([omastore, "lint-manifest", path])
        py_code, py_fields, py_out = run([sys.executable, VALIDATOR, path])
        problems = []
        if go_code != py_code:
            problems.append(f"exit code: go={go_code} python={py_code}")
        if {f for f in go_fields if f[0] == "error"} != {f for f in py_fields if f[0] == "error"}:
            problems.append(f"fields with errors: go={sorted(go_fields)} python={sorted(py_fields)}")
        expect_err = name.startswith("error-")
        if (go_code != 0) != expect_err:
            problems.append(f"expected {'error' if expect_err else 'success'}, go exited with {go_code}")
        if name.startswith("ok-") and ("warning" in go_out or "warning" in py_out):
            problems.append("ok-* should have no warnings")
        if name.startswith("warning-") and not ("warning" in go_out and "warning" in py_out):
            problems.append("warning-* should have a warning in both")
        status = "ok  " if not problems else "FAIL"
        print(f"{status} {name}")
        for p in problems:
            all_ok = False
            print(f"      {p}\n      go:     {go_out.strip()}\n      python: {py_out.strip()}")
    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())
