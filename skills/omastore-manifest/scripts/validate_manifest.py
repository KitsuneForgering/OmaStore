#!/usr/bin/env python3
"""Valida um omastore.toml com as mesmas regras do `omastore lint-manifest`.

Uso:
    validate_manifest.py [arquivo-ou-diretório] [--tag v1.2.0 --asset nome ...]

- Com um diretório, lê <dir>/omastore.toml e confere se ícone e screenshots
  existem no repositório.
- Com --tag e um ou mais --asset (nomes dos arquivos da release), confere se
  os padrões `asset` de cada arquitetura casam com algum deles.

Saída: uma linha "erro: campo: mensagem" ou "aviso: campo: mensagem" por
problema e, no fim, "ok: ..." se não houver erros. Código de saída 1 se houver
erro. Só usa a biblioteca padrão (Python 3.11+).

A referência é o validador em Go (backend/internal/manifest). Se os dois
divergirem, vale o do Go; `make test-skills` compara os dois.
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
        self.lines.append(("erro", field, msg))

    def warn(self, field: str, msg: str) -> None:
        self.lines.append(("aviso", field, msg))

    @property
    def errors(self) -> int:
        return sum(1 for k, _, _ in self.lines if k == "erro")


def rel_path(p: str) -> tuple[str | None, str | None]:
    """Valida um caminho relativo; devolve (caminho limpo, erro)."""
    if not p:
        return None, "vazio"
    if len(p) > MAX_PATH:
        return None, "longo demais"
    if "\\" in p or p.startswith("/") or "\x00" in p:
        return None, "precisa ser relativo, com /"
    c = posixpath.normpath(p)
    if c in (".", "..") or c.startswith("../"):
        return None, "sai do repositório"
    return c, None


def clean_text(s: str, limit: int) -> tuple[str, bool]:
    s = " ".join(s.split())
    if len(s) > limit:
        return s[:limit], False
    return s, True


def check_pattern(p: str) -> str | None:
    if len(p) > MAX_PATH:
        return "padrão longo demais"
    if "/" in p or "\\" in p:
        return "o nome do asset não pode conter /"
    e = check_placeholders(p)
    if e:
        return e
    if p.strip("*") == "":
        return "padrão genérico demais"
    return None


def check_placeholders(p: str) -> str | None:
    for ph in RE_PLACEHOLDER.findall(p):
        if ph not in ("{version}", "{tag}"):
            return f"marcador desconhecido {ph} (use {{version}} ou {{tag}})"
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
    """Aplica as regras e devolve o manifesto normalizado."""
    unknown = sorted(set(data) - TOP_KEYS)
    for arch_key, target in (data.get("linux") or {}).items() if isinstance(data.get("linux"), dict) else []:
        if isinstance(target, dict):
            unknown += [f"linux.{arch_key}.{k}" for k in sorted(set(target) - TARGET_KEYS)]
    if unknown:
        rep.err("(arquivo)", "campos desconhecidos: " + ", ".join(unknown))
        return {}

    types = {"kind": str, "name": str, "summary": str, "icon": str, "terminal": bool,
             "categories": list, "screenshots": list, "linux": dict}
    for k, t in types.items():
        if k in data and not isinstance(data[k], t):
            rep.err("(arquivo)", f"{k}: tipo inválido")
            return {}

    m: dict = {}
    kind = str(data.get("kind", "")).strip().lower()
    if kind in ("", "app"):
        m["kind"] = "app"
    elif kind in ("plugin", "theme"):
        m["kind"] = kind
        rep.warn("kind", f'"{kind}": a OmaStore só indexa apps; este repositório não entra no catálogo')
    else:
        m["kind"] = kind
        rep.err("kind", f'"{data.get("kind")}" desconhecido (use "app")')

    for field, limit in (("name", MAX_NAME), ("summary", MAX_SUMMARY)):
        if data.get(field):
            text, ok = clean_text(data[field], limit)
            if not ok:
                rep.warn(field, f"cortado em {limit} caracteres")
            m[field] = text

    cats: list[str] = []
    for c in data.get("categories", []):
        c = str(c).strip()
        if c in cats:
            continue
        if c not in MAIN_CATEGORIES and not RE_ADDITIONAL.match(c):
            rep.err("categories", f'categoria inválida "{c}"')
            continue
        if len(cats) == MAX_CATEGORIES:
            rep.warn("categories", f"só as {MAX_CATEGORIES} primeiras são usadas")
            continue
        cats.append(c)
    m["categories"] = cats
    if cats and not any(c in MAIN_CATEGORIES for c in cats):
        rep.warn("categories", "nenhuma categoria principal da freedesktop (ex.: Graphics, System)")

    if data.get("icon"):
        p, e = rel_path(data["icon"])
        if e:
            rep.err("icon", f'"{data["icon"]}": {e}')
        elif posixpath.splitext(p)[1] not in (".png", ".svg"):
            rep.err("icon", f'"{data["icon"]}": use PNG ou SVG')
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
                rep.err("screenshots", f'"{s}" não é imagem')
                continue
            shots.append(p)
        if len(shots) == MAX_SCREENSHOTS:
            if len(data.get("screenshots", [])) > MAX_SCREENSHOTS:
                rep.warn("screenshots", f"só as {MAX_SCREENSHOTS} primeiras são usadas")
            break
    m["screenshots"] = shots

    targets: dict[str, dict] = {}
    for key in sorted(data.get("linux", {})):
        t = data["linux"][key]
        field = f"linux.{key}"
        arch = ARCH.get(key.lower())
        if not arch:
            rep.err(field, "arquitetura desconhecida (use x86_64 ou aarch64)")
            continue
        if arch in targets:
            rep.err(field, "arquitetura repetida")
            continue
        if not isinstance(t, dict):
            rep.err(field, "precisa ser uma tabela [linux.<arq>]")
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
    ap.add_argument("--tag", help="tag da release, para conferir os padrões de asset")
    ap.add_argument("--asset", action="append", default=[], help="nome de um asset da release (repita)")
    args = ap.parse_args()

    path, repo_dir = args.target, None
    if os.path.isdir(path):
        repo_dir, path = path, os.path.join(path, FILE_NAME)
    try:
        with open(path, "rb") as f:
            raw = f.read(MAX_SIZE + 1)
    except OSError as e:
        print(f"erro: {e}")
        return 1
    if len(raw) > MAX_SIZE:
        print(f"erro: (arquivo): maior que {MAX_SIZE} bytes")
        return 1
    try:
        data = tomllib.loads(raw.decode("utf-8"))
    except (tomllib.TOMLDecodeError, UnicodeDecodeError) as e:
        print(f"erro: (arquivo): TOML inválido: {e}")
        return 1

    rep = Report()
    m = validate(data, rep)

    if m and repo_dir:
        for p in ([m["icon"]] if m.get("icon") else []) + m.get("screenshots", []):
            if not p.startswith("https://") and not os.path.exists(os.path.join(repo_dir, p)):
                rep.err(p, "arquivo não existe no repositório")

    if m and args.tag and args.asset:
        for arch, t in m.get("linux", {}).items():
            if t.get("asset") and not any(match_asset(t["asset"], args.tag, a) for a in args.asset):
                rep.err(f"linux.{arch}.asset", f'"{t["asset"]}" não casa com nenhum asset da release {args.tag}')

    for kind, field, msg in rep.lines:
        print(f"{kind}: {field}: {msg}")
    if rep.errors:
        return 1
    if m.get("kind") != "app":
        return 0  # o aviso sobre kind já foi impresso entre os problemas
    print(f"ok: {path}")
    for arch, t in sorted(m.get("linux", {}).items()):
        print(f'  {arch:<6} asset="{t.get("asset", "")}" exec="{t.get("exec", "")}"')
    return 0


if __name__ == "__main__":
    sys.exit(main())
