#!/usr/bin/env python3
"""Validate an omastore.toml with the same rules as `omastore lint-manifest`.

Usage:
    validate_manifest.py [file-or-directory] [--tag v1.2.0 --asset name ...]

- With a directory, reads <dir>/omastore.toml and checks that the icon and
  screenshots exist in the repository.
- With --tag and one or more --asset (the release file names), checks that
  each architecture's `asset` pattern matches one of them.

Output: one "error: field: message" or "warning: field: message" line per
problem and, at the end, "ok: ..." if there are no errors. Exit code 1 if
there is an error. Standard library only (Python 3.11+).

The reference is the Go validator (backend/internal/manifest). If the two
disagree, Go wins; `make test-skills` compares them.
"""
from __future__ import annotations

import argparse
import os
import posixpath
import re
import sys
import tomllib

FILE_NAME = "omastore.toml"
MAX_SIZE = 64 << 10
MAX_NAME, MAX_SUMMARY, MAX_CATEGORIES, MAX_SCREENSHOTS, MAX_PATH = 80, 300, 4, 8, 256

TOP_KEYS = {"kind", "name", "summary", "categories", "icon", "screenshots", "terminal", "linux"}
TARGET_KEYS = {"asset", "exec"}
MAIN_CATEGORIES = {
    "AudioVideo", "Audio", "Video", "Development", "Education", "Game", "Graphics",
    "Network", "Office", "Science", "Settings", "System", "Utility",
}
RE_ADDITIONAL = re.compile(r"^[A-Z][A-Za-z0-9]{1,39}$")
ARCH = {"x86_64": "amd64", "amd64": "amd64", "x64": "amd64", "aarch64": "arm64", "arm64": "arm64"}
IMAGE_EXT = {".png", ".svg", ".jpg", ".jpeg", ".webp", ".gif"}
RE_PLACEHOLDER = re.compile(r"\{[^}]*\}")


class Report:
    def __init__(self) -> None:
        self.lines: list[tuple[str, str, str]] = []

    def err(self, field: str, msg: str) -> None:
        self.lines.append(("error", field, msg))

    def warn(self, field: str, msg: str) -> None:
        self.lines.append(("warning", field, msg))

    @property
    def errors(self) -> int:
        return sum(1 for k, _, _ in self.lines if k == "error")


def rel_path(p: str) -> tuple[str | None, str | None]:
    """Validate a relative path; return (clean path, error)."""
    if not p:
        return None, "empty"
    if len(p) > MAX_PATH:
        return None, "too long"
    if "\\" in p or p.startswith("/") or "\x00" in p:
        return None, "must be relative, with /"
    c = posixpath.normpath(p)
    if c in (".", "..") or c.startswith("../"):
        return None, "leaves the repository"
    return c, None


def clean_text(s: str, limit: int) -> tuple[str, bool]:
    s = " ".join(s.split())
    if len(s) > limit:
        return s[:limit], False
    return s, True


def check_pattern(p: str) -> str | None:
    if len(p) > MAX_PATH:
        return "pattern too long"
    if "/" in p or "\\" in p:
        return "the asset name cannot contain /"
    e = check_placeholders(p)
    if e:
        return e
    if p.strip("*") == "":
        return "pattern too generic"
    return None


def check_placeholders(p: str) -> str | None:
    for ph in RE_PLACEHOLDER.findall(p):
        if ph not in ("{version}", "{tag}"):
            return f"unknown placeholder {ph} (use {{version}} or {{tag}})"
    return None


def match_asset(pattern: str, tag: str, name: str) -> bool:
    version = tag[1:] if tag[:1] in ("v", "V") else tag
    p = pattern.replace("{version}", version).replace("{tag}", tag)
    parts = p.split("*")
    if len(parts) == 1:
        return p == name
    if not name.startswith(parts[0]):
        return False
    rest = name[len(parts[0]):]
    for mid in parts[1:-1]:
        i = rest.find(mid)
        if i < 0:
            return False
        rest = rest[i + len(mid):]
    return rest.endswith(parts[-1])


def validate(data: dict, rep: Report) -> dict:
    """Apply the rules and return the normalized manifest."""
    unknown = sorted(set(data) - TOP_KEYS)
    for arch_key, target in (data.get("linux") or {}).items() if isinstance(data.get("linux"), dict) else []:
        if isinstance(target, dict):
            unknown += [f"linux.{arch_key}.{k}" for k in sorted(set(target) - TARGET_KEYS)]
    if unknown:
        rep.err("(file)", "unknown fields: " + ", ".join(unknown))
        return {}

    types = {"kind": str, "name": str, "summary": str, "icon": str, "terminal": bool,
             "categories": list, "screenshots": list, "linux": dict}
    for k, t in types.items():
        if k in data and not isinstance(data[k], t):
            rep.err("(file)", f"{k}: invalid type")
            return {}

    m: dict = {}
    kind = str(data.get("kind", "")).strip().lower()
    if kind in ("", "app"):
        m["kind"] = "app"
    elif kind in ("plugin", "theme"):
        m["kind"] = kind
        rep.warn("kind", f'"{kind}": OmaStore only indexes apps; this repository is not added to the catalog')
    else:
        m["kind"] = kind
        rep.err("kind", f'"{data.get("kind")}" is unknown (use "app")')

    for field, limit in (("name", MAX_NAME), ("summary", MAX_SUMMARY)):
        if data.get(field):
            text, ok = clean_text(data[field], limit)
            if not ok:
                rep.warn(field, f"truncated to {limit} characters")
            m[field] = text

    cats: list[str] = []
    for c in data.get("categories", []):
        c = str(c).strip()
        if c in cats:
            continue
        if c not in MAIN_CATEGORIES and not RE_ADDITIONAL.match(c):
            rep.err("categories", f'invalid category "{c}"')
            continue
        if len(cats) == MAX_CATEGORIES:
            rep.warn("categories", f"only the first {MAX_CATEGORIES} are used")
            continue
        cats.append(c)
    m["categories"] = cats
    if cats and not any(c in MAIN_CATEGORIES for c in cats):
        rep.warn("categories", "no freedesktop main category (e.g. Graphics, System)")

    if data.get("icon"):
        p, e = rel_path(data["icon"])
        if e:
            rep.err("icon", f'"{data["icon"]}": {e}')
        elif posixpath.splitext(p)[1] not in (".png", ".svg"):
            rep.err("icon", f'"{data["icon"]}": use PNG or SVG')
        else:
            m["icon"] = p

    shots: list[str] = []
    for s in data.get("screenshots", []):
        s = str(s)
        if s.startswith("https://"):
            shots.append(s)
        else:
            p, e = rel_path(s)
            if e:
                rep.err("screenshots", f'"{s}": {e}')
                continue
            if posixpath.splitext(p)[1].lower() not in IMAGE_EXT:
                rep.err("screenshots", f'"{s}" is not an image')
                continue
            shots.append(p)
        if len(shots) == MAX_SCREENSHOTS:
            if len(data.get("screenshots", [])) > MAX_SCREENSHOTS:
                rep.warn("screenshots", f"only the first {MAX_SCREENSHOTS} are used")
            break
    m["screenshots"] = shots

    targets: dict[str, dict] = {}
    for key in sorted(data.get("linux", {})):
        t = data["linux"][key]
        field = f"linux.{key}"
        arch = ARCH.get(key.lower())
        if not arch:
            rep.err(field, "unknown architecture (use x86_64 or aarch64)")
            continue
        if arch in targets:
            rep.err(field, "duplicated architecture")
            continue
        if not isinstance(t, dict):
            rep.err(field, "must be a [linux.<arch>] table")
            continue
        tgt = {"asset": t.get("asset", ""), "exec": t.get("exec", "")}
        if tgt["asset"]:
            e = check_pattern(tgt["asset"])
            if e:
                rep.err(field + ".asset", e)
                tgt["asset"] = ""
        if tgt["exec"]:
            p, e = rel_path(tgt["exec"])
            if not e:
                e = check_placeholders(tgt["exec"])
            if e:
                rep.err(field + ".exec", f'"{tgt["exec"]}": {e}')
                p = ""
            tgt["exec"] = p
        if tgt["asset"] or tgt["exec"]:
            targets[arch] = tgt
    m["linux"] = targets
    return m


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("target", nargs="?", default=".")
    ap.add_argument("--tag", help="release tag, to check the asset patterns")
    ap.add_argument("--asset", action="append", default=[], help="name of a release asset (repeat)")
    args = ap.parse_args()

    path, repo_dir = args.target, None
    if os.path.isdir(path):
        repo_dir, path = path, os.path.join(path, FILE_NAME)
    try:
        with open(path, "rb") as f:
            raw = f.read(MAX_SIZE + 1)
    except OSError as e:
        print(f"error: {e}")
        return 1
    if len(raw) > MAX_SIZE:
        print(f"error: (file): larger than {MAX_SIZE} bytes")
        return 1
    try:
        data = tomllib.loads(raw.decode("utf-8"))
    except (tomllib.TOMLDecodeError, UnicodeDecodeError) as e:
        print(f"error: (file): invalid TOML: {e}")
        return 1

    rep = Report()
    m = validate(data, rep)

    if m and repo_dir:
        for p in ([m["icon"]] if m.get("icon") else []) + m.get("screenshots", []):
            if not p.startswith("https://") and not os.path.exists(os.path.join(repo_dir, p)):
                rep.err(p, "file does not exist in the repository")

    if m and args.tag and args.asset:
        for arch, t in m.get("linux", {}).items():
            if t.get("asset") and not any(match_asset(t["asset"], args.tag, a) for a in args.asset):
                rep.err(f"linux.{arch}.asset", f'"{t["asset"]}" matches no asset of release {args.tag}')

    for kind, field, msg in rep.lines:
        print(f"{kind}: {field}: {msg}")
    if rep.errors:
        return 1
    if m.get("kind") != "app":
        return 0  # the kind warning was already printed with the problems
    print(f"ok: {path}")
    for arch, t in sorted(m.get("linux", {}).items()):
        print(f'  {arch:<6} asset="{t.get("asset", "")}" exec="{t.get("exec", "")}"')
    return 0


if __name__ == "__main__":
    sys.exit(main())
