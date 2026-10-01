#!/usr/bin/env python3
"""Audits whether a GitHub repository is an app OmaStore can install.

Usage:
    omastore_check.py owner/repo [--manifest omastore.toml] [--install] [--json]

Runs the `omastore` CLI itself in a temporary HOME (nothing is written to your
~/.local), so the result is exactly what the store would do:

1. validates the published omastore.toml (or the local one given in --manifest);
2. indexes the repository and reads what the store understood (omastore show --json);
3. with --install, really installs into the temporary HOME, checks the .desktop
   and the executable (without running it) and uninstalls.

Requires: `omastore` (PATH or $OMASTORE), an authenticated `gh` or GITHUB_TOKEN.
Output: markdown report (or JSON with --json). Exit code 1 if anything failed.
"""
from __future__ import annotations

import argparse
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request

PASS, WARN, FAIL = "ok", "warning", "fail"
ICON = {PASS: "✅", WARN: "⚠️", FAIL: "❌"}
GOARCH = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64", "amd64": "amd64"}


class Audit:
    def __init__(self) -> None:
        self.checks: list[dict] = []

    def add(self, status: str, item: str, detail: str = "", fix: str = "") -> None:
        self.checks.append({"status": status, "item": item, "detail": detail, "fix": fix})

    @property
    def failed(self) -> bool:
        return any(c["status"] == FAIL for c in self.checks)


def run(cmd: list[str], env: dict | None = None, timeout: int = 600) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, capture_output=True, text=True, env=env, timeout=timeout)


def find_omastore(explicit: str | None) -> str | None:
    for c in (explicit, os.environ.get("OMASTORE"), shutil.which("omastore")):
        if c and os.path.isfile(c) and os.access(c, os.X_OK):
            return c
    return None


def github_token() -> str:
    tok = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if tok:
        return tok
    if shutil.which("gh"):
        p = run(["gh", "auth", "token"], timeout=10)
        if p.returncode == 0:
            return p.stdout.strip()
    return ""


def isolated_env(home: str, token: str) -> dict:
    env = {k: v for k, v in os.environ.items() if not k.startswith("XDG_")}
    env["HOME"] = home
    env["OMASTORE_LOG"] = "error"
    if token:
        env["GITHUB_TOKEN"] = token
    return env


# Outcomes of fetch_manifest.
FOUND, MISSING, NO_REPO, DENIED, UNREACHABLE = "found", "missing", "no-repo", "denied", "unreachable"


def github_get(path: str, token: str, raw: bool = False) -> tuple[int, bytes, str]:
    """GET on the GitHub API: (HTTP status, body, error). Status 0 means the
    request did not complete (network, DNS, timeout)."""
    req = urllib.request.Request("https://api.github.com/" + path, headers={
        "Accept": "application/vnd.github.raw" if raw else "application/vnd.github+json",
        "User-Agent": "omastore-check",
    })
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status, resp.read(), ""
    except urllib.error.HTTPError as e:
        return e.code, e.read(), e.reason
    except (urllib.error.URLError, OSError) as e:
        return 0, b"", str(getattr(e, "reason", e))


def fetch_manifest(repo: str, dest: str, token: str) -> tuple[str, str]:
    """Downloads the published omastore.toml into dest. Returns (outcome,
    detail): a missing file is told apart from a missing repository, a
    refused request (bad token, rate limit) and a network failure."""
    status, body, err = github_get(f"repos/{repo}/contents/omastore.toml", token, raw=True)
    if status == 200:
        with open(dest, "wb") as f:
            f.write(body)
        return FOUND, ""
    if status == 404:
        rstatus, _, rerr = github_get(f"repos/{repo}", token)
        if rstatus == 404:
            return NO_REPO, "repository not found, or private"
        if rstatus == 200:
            return MISSING, ""
        status, err = rstatus, rerr  # could not tell: report why
    if status in (401, 403, 429):
        why = "rate limit or bad token" if token else "rate limit (no token)"
        return DENIED, f"GitHub refused the request ({status} {err}: {why})"
    if status == 0:
        return UNREACHABLE, f"GitHub is unreachable: {err}"
    return UNREACHABLE, f"GitHub answered {status} {err}"


def file_kind(path: str) -> str:
    try:
        with open(path, "rb") as f:
            head = f.read(4)
    except OSError:
        return "missing"
    if head.startswith(b"\x7fELF"):
        return "elf"
    if head.startswith(b"#!"):
        return "script"
    return "other"


def absolute_usr_refs(path: str, name: str) -> list[str]:
    """Looks for references to /usr/share/<app> and /usr/lib/<app> in the binary
    (read only; nothing is executed)."""
    try:
        with open(path, "rb") as f:
            data = f.read(64 << 20)
    except OSError:
        return []
    pat = re.compile(rb"/usr/(?:local/)?(?:share|lib)/" + re.escape(name.lower().encode()) + rb"[\w./-]*")
    found = {m.group().decode(errors="replace") for m in pat.finditer(data.lower())}
    return sorted(r for r in found if not os.path.exists(r))[:5]


def audit(repo: str, omastore: str, manifest_file: str | None, install: bool, work: str) -> Audit:
    a = Audit()
    token = github_token()
    if not token:
        a.add(WARN, "GitHub token", "no GITHUB_TOKEN nor `gh auth token`: the API is limited to 60 requests/hour",
              "`gh auth login`")
    env = isolated_env(os.path.join(work, "home"), token)
    os.makedirs(env["HOME"], exist_ok=True)

    # 1. Manifest
    mpath = manifest_file
    if not mpath:
        mpath = os.path.join(work, "omastore.toml")
        outcome, detail = fetch_manifest(repo, mpath, token)
        if outcome == MISSING:
            a.add(FAIL, "omastore.toml", "missing from the root of the default branch: the store ignores the repository",
                  "create the manifest (skill omastore-manifest) and push it; to test first, use --manifest")
            return a
        if outcome == NO_REPO:
            a.add(FAIL, "Repository", detail, "check the owner/repo name; the store only lists public repositories")
            return a
        if outcome != FOUND:
            # Not a verdict on the repository: the audit could not look.
            a.add(FAIL, "Could not read omastore.toml", detail,
                  "`gh auth login` (or GITHUB_TOKEN) and try again" if outcome == DENIED
                  else "check the network connection and try again")
            return a
    lint = run([omastore, "lint-manifest", mpath], env=env)
    lint_out = (lint.stdout + lint.stderr).strip()
    if lint.returncode != 0:
        a.add(FAIL, "Valid omastore.toml", lint_out, "fix it with `omastore lint-manifest` (skill omastore-manifest)")
        return a
    if "OmaStore only indexes apps" in lint_out:
        a.add(FAIL, "Is an app", lint_out, "OmaStore does not distribute plugins or themes")
        return a
    a.add(PASS, "Valid omastore.toml", "published" if not manifest_file else f"local: {manifest_file}")

    # 2. Indexing
    idx_cmd = [omastore, "index", repo]
    if manifest_file:
        idx_cmd[2:2] = ["--manifest", manifest_file]
    idx = run(idx_cmd, env=env)
    if idx.returncode != 0:
        a.add(FAIL, "Indexing", (idx.stdout + idx.stderr).strip())
        return a
    show = run([omastore, "show", "--json", repo], env=env)
    if show.returncode != 0:
        a.add(FAIL, "Listed in the catalog", (show.stdout + show.stderr).strip() or idx.stdout.strip(),
              "check that the repository exists, is not archived and the manifest declares an app")
        return a
    d = json.loads(show.stdout)
    a.add(PASS, "Listed in the catalog", f'{d["Name"]} · category {d["Category"]}')

    a.add(PASS if d.get("Summary") else WARN, "Summary", d.get("Summary") or "empty",
          "" if d.get("Summary") else "fill in the repo description or `summary` in the manifest")
    a.add(PASS if d.get("IconURL") else WARN, "Icon", d.get("IconURL") or "none found",
          "" if d.get("IconURL") else "add an SVG/PNG and declare `icon` in the manifest")
    shots = d.get("Screenshots") or []
    a.add(PASS if shots else WARN, "Screenshots", f"{len(shots)} found",
          "" if shots else "add images to the README or `screenshots` in the manifest")
    lic = (d.get("Repo") or {}).get("License", "")
    a.add(PASS if lic else WARN, "License", lic or "not detected", "" if lic else "add a LICENSE file")

    tag = (d.get("Repo") or {}).get("LatestTag", "")
    if not tag:
        a.add(FAIL, "Release", "no stable release (pre-releases and drafts do not count)",
              "publish a release with a Linux binary (skill omastore-release)")
        return a
    assets = d.get("Assets") or []
    by_arch: dict[str, list[dict]] = {}
    for x in assets:
        by_arch.setdefault(x.get("Arch") or "generic", []).append(x)
    names = ", ".join(f'{x["Name"]} [{x["Format"]} {x.get("Arch") or "?"}]' for x in assets) or "none"
    machine = GOARCH.get(platform.machine(), platform.machine())
    if not d.get("Installable"):
        a.add(FAIL, f"Installable on {machine}", f"release {tag}: recognized assets: {names}",
              "publish <app>-<version>-<arch>-linux.tar.gz or declare `[linux.<arch>] asset` (skills omastore-release/omastore-manifest)")
    else:
        a.add(PASS, f"Installable on {machine}", f"release {tag}: {names}")
    for arch, label in (("amd64", "x86_64"), ("arm64", "aarch64")):
        if arch != machine and arch not in by_arch and "generic" not in by_arch:
            a.add(WARN, f"Build {label}", "no asset for this architecture",
                  "add an ARM runner to the workflow (skill omastore-release)" if arch == "arm64" else "")
    unverified = [x["Name"] for x in assets if not x.get("Digest") and not x.get("ChecksumURL")]
    a.add(WARN if unverified else PASS, "Checksum",
          "no sha256: " + ", ".join(unverified) if unverified else "all assets have a verifiable sha256",
          "publish checksums.txt" if unverified else "")

    if not install or not d.get("Installable"):
        return a

    # 3. Test installation (nothing is executed)
    inst = run([omastore, "install", repo], env=env, timeout=1800)
    if inst.returncode != 0:
        a.add(FAIL, "Installation", (inst.stdout + inst.stderr).strip(),
              "see the message: executable not found, name conflict, checksum...")
        return a
    info = json.loads(run([omastore, "show", "--json", repo], env=env).stdout).get("Install") or {}
    exe = info.get("ExecPath", "")
    kind = file_kind(exe)
    a.add(PASS if kind in ("elf", "script") else FAIL, "Executable",
          f"{os.path.relpath(exe, env['HOME']) if exe else '?'} ({kind})",
          "" if kind in ("elf", "script") else "declare `exec` in the manifest pointing to the right binary")
    if kind == "elf":
        refs = absolute_usr_refs(exe, repo.split("/")[1])
        if refs:
            a.add(WARN, "Absolute paths", "the binary mentions " + ", ".join(refs) +
                  ", which do not exist outside an installation into /usr",
                  "resolve data relative to the executable (the store installs into ~/.local/share/omastore/apps)")
    desktop = info.get("DesktopPath", "")
    if desktop and shutil.which("desktop-file-validate"):
        v = run(["desktop-file-validate", desktop])
        a.add(PASS if v.returncode == 0 and not v.stdout.strip() else WARN, ".desktop",
              v.stdout.strip() or os.path.basename(desktop))
    elif desktop:
        a.add(PASS, ".desktop", os.path.basename(desktop))
    un = run([omastore, "uninstall", repo], env=env)
    leftover = [p for p in (exe, desktop) if p and os.path.exists(p)]
    a.add(PASS if un.returncode == 0 and not leftover else WARN, "Uninstallation",
          "clean" if not leftover else "left behind: " + ", ".join(leftover))
    return a


def report(repo: str, a: Audit) -> str:
    lines = [f"# OmaStore: {repo}", "", "| | Item | Detail |", "|---|---|---|"]
    for c in a.checks:
        detail = c["detail"].replace("\n", " ").replace("|", "\\|")
        lines.append(f'| {ICON[c["status"]]} | {c["item"]} | {detail} |')
    fixes = [c for c in a.checks if c["fix"] and c["status"] != PASS]
    if fixes:
        lines += ["", "## What to do", ""]
        lines += [f'- **{c["item"]}:** {c["fix"]}' for c in fixes]
    lines += ["", "Result: " + ("**not installable by OmaStore**" if a.failed else "**compatible with OmaStore**")]
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("repo", help="owner/repo")
    ap.add_argument("--manifest", help="local omastore.toml to use instead of the published one")
    ap.add_argument("--install", action="store_true", help="install into a temporary HOME and uninstall")
    ap.add_argument("--omastore", help="path to the omastore binary")
    ap.add_argument("--json", action="store_true", help="JSON output")
    ap.add_argument("--keep", action="store_true", help="do not delete the temporary HOME")
    args = ap.parse_args()

    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+", args.repo):
        print("repo must be owner/repo", file=sys.stderr)
        return 2
    omastore = find_omastore(args.omastore)
    if not omastore:
        print("omastore not found: install OmaStore or pass --omastore /path/to/omastore", file=sys.stderr)
        return 2
    manifest_file = os.path.abspath(args.manifest) if args.manifest else None

    work = tempfile.mkdtemp(prefix="omastore-check-")
    try:
        a = audit(args.repo, omastore, manifest_file, args.install, work)
    finally:
        if args.keep:
            print(f"(temporary HOME kept at {work})", file=sys.stderr)
        else:
            shutil.rmtree(work, ignore_errors=True)
    if args.json:
        print(json.dumps({"repo": args.repo, "compatible": not a.failed, "checks": a.checks},
                         ensure_ascii=False, indent=2))
    else:
        print(report(args.repo, a))
    return 1 if a.failed else 0


if __name__ == "__main__":
    sys.exit(main())
