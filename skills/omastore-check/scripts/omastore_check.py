#!/usr/bin/env python3
"""Audita se um repositório do GitHub é um app instalável pela OmaStore.

Uso:
    omastore_check.py owner/repo [--manifest omastore.toml] [--install] [--json]

Roda a própria CLI `omastore` num HOME temporário (nada é gravado no seu
~/.local), então o resultado é exatamente o que a loja faria:

1. valida o omastore.toml publicado (ou o local passado em --manifest);
2. indexa o repositório e lê o que a loja entendeu (omastore show --json);
3. com --install, instala de verdade no HOME temporário, confere o .desktop,
   o executável (sem executá-lo) e desinstala.

Precisa de: `omastore` (PATH ou $OMASTORE), `gh` autenticado ou GITHUB_TOKEN.
Saída: relatório em markdown (ou JSON com --json). Código 1 se houver falha.
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

PASS, WARN, FAIL = "ok", "aviso", "falha"
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


def fetch_manifest(repo: str, dest: str) -> bool:
    """Baixa o omastore.toml publicado; False se não existir."""
    if not shutil.which("gh"):
        return False
    p = run(["gh", "api", f"repos/{repo}/contents/omastore.toml",
             "-H", "Accept: application/vnd.github.raw"], timeout=30)
    if p.returncode != 0:
        return False
    with open(dest, "w") as f:
        f.write(p.stdout)
    return True


def file_kind(path: str) -> str:
    try:
        with open(path, "rb") as f:
            head = f.read(4)
    except OSError:
        return "ausente"
    if head.startswith(b"\x7fELF"):
        return "elf"
    if head.startswith(b"#!"):
        return "script"
    return "outro"


def absolute_usr_refs(path: str, name: str) -> list[str]:
    """Procura no binário referências a /usr/share/<app> e /usr/lib/<app>
    (leitura apenas; nada é executado)."""
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
        a.add(WARN, "Token do GitHub", "sem GITHUB_TOKEN nem `gh auth token`: a API limita a 60 requisições/hora",
              "`gh auth login`")
    env = isolated_env(os.path.join(work, "home"), token)
    os.makedirs(env["HOME"], exist_ok=True)

    # 1. Manifesto
    mpath = manifest_file
    if not mpath:
        mpath = os.path.join(work, "omastore.toml")
        if not fetch_manifest(repo, mpath):
            a.add(FAIL, "omastore.toml", "não existe na raiz do branch padrão: a loja ignora o repositório",
                  "crie o manifesto (skill omastore-manifest) e faça push; para testar antes, use --manifest")
            return a
    lint = run([omastore, "lint-manifest", mpath], env=env)
    lint_out = (lint.stdout + lint.stderr).strip()
    if lint.returncode != 0:
        a.add(FAIL, "omastore.toml válido", lint_out, "corrija com `omastore lint-manifest` (skill omastore-manifest)")
        return a
    if "OmaStore only indexes apps" in lint_out:
        a.add(FAIL, "É um app", lint_out, "a OmaStore não distribui plugins nem temas")
        return a
    a.add(PASS, "omastore.toml válido", "publicado" if not manifest_file else f"local: {manifest_file}")

    # 2. Indexação
    idx_cmd = [omastore, "index", repo]
    if manifest_file:
        idx_cmd[2:2] = ["--manifest", manifest_file]
    idx = run(idx_cmd, env=env)
    if idx.returncode != 0:
        a.add(FAIL, "Indexação", (idx.stdout + idx.stderr).strip())
        return a
    show = run([omastore, "show", "--json", repo], env=env)
    if show.returncode != 0:
        a.add(FAIL, "Entrou no catálogo", (show.stdout + show.stderr).strip() or idx.stdout.strip(),
              "confira se o repositório existe, não está arquivado e o manifesto declara um app")
        return a
    d = json.loads(show.stdout)
    a.add(PASS, "Entrou no catálogo", f'{d["Name"]} · categoria {d["Category"]}')

    a.add(PASS if d.get("Summary") else WARN, "Resumo", d.get("Summary") or "vazio",
          "" if d.get("Summary") else "preencha a descrição do repo ou `summary` no manifesto")
    a.add(PASS if d.get("IconURL") else WARN, "Ícone", d.get("IconURL") or "nenhum encontrado",
          "" if d.get("IconURL") else "adicione um SVG/PNG e declare `icon` no manifesto")
    shots = d.get("Screenshots") or []
    a.add(PASS if shots else WARN, "Screenshots", f"{len(shots)} encontrada(s)",
          "" if shots else "adicione imagens ao README ou `screenshots` no manifesto")
    lic = (d.get("Repo") or {}).get("License", "")
    a.add(PASS if lic else WARN, "Licença", lic or "não detectada", "" if lic else "adicione um arquivo LICENSE")

    tag = (d.get("Repo") or {}).get("LatestTag", "")
    if not tag:
        a.add(FAIL, "Release", "nenhuma release estável (pre-releases e drafts não contam)",
              "publique uma release com binário Linux (skill omastore-release)")
        return a
    assets = d.get("Assets") or []
    by_arch: dict[str, list[dict]] = {}
    for x in assets:
        by_arch.setdefault(x.get("Arch") or "genérico", []).append(x)
    names = ", ".join(f'{x["Name"]} [{x["Format"]} {x.get("Arch") or "?"}]' for x in assets) or "nenhum"
    machine = GOARCH.get(platform.machine(), platform.machine())
    if not d.get("Installable"):
        a.add(FAIL, f"Instalável em {machine}", f"release {tag}: assets reconhecidos: {names}",
              "publique <app>-<versão>-<arq>-linux.tar.gz ou declare `[linux.<arq>] asset` (skills omastore-release/omastore-manifest)")
    else:
        a.add(PASS, f"Instalável em {machine}", f"release {tag}: {names}")
    for arch, label in (("amd64", "x86_64"), ("arm64", "aarch64")):
        if arch != machine and arch not in by_arch and "genérico" not in by_arch:
            a.add(WARN, f"Build {label}", "nenhum asset para essa arquitetura",
                  "adicione um runner ARM no workflow (skill omastore-release)" if arch == "arm64" else "")
    unverified = [x["Name"] for x in assets if not x.get("Digest") and not x.get("ChecksumURL")]
    a.add(WARN if unverified else PASS, "Checksum",
          "sem sha256: " + ", ".join(unverified) if unverified else "todos os assets têm sha256 verificável",
          "publique checksums.txt" if unverified else "")

    if not install or not d.get("Installable"):
        return a

    # 3. Instalação de teste (nada é executado)
    inst = run([omastore, "install", repo], env=env, timeout=1800)
    if inst.returncode != 0:
        a.add(FAIL, "Instalação", (inst.stdout + inst.stderr).strip(),
              "veja a mensagem: executável não encontrado, conflito de nome, checksum...")
        return a
    info = json.loads(run([omastore, "show", "--json", repo], env=env).stdout).get("Install") or {}
    exe = info.get("ExecPath", "")
    kind = file_kind(exe)
    a.add(PASS if kind in ("elf", "script") else FAIL, "Executável",
          f"{os.path.relpath(exe, env['HOME']) if exe else '?'} ({kind})",
          "" if kind in ("elf", "script") else "declare `exec` no manifesto apontando para o binário certo")
    if kind == "elf":
        refs = absolute_usr_refs(exe, repo.split("/")[1])
        if refs:
            a.add(WARN, "Caminhos absolutos", "o binário cita " + ", ".join(refs) +
                  ", que não existem fora de uma instalação em /usr",
                  "resolva dados relativos ao executável (a loja instala em ~/.local/share/omastore/apps)")
    desktop = info.get("DesktopPath", "")
    if desktop and shutil.which("desktop-file-validate"):
        v = run(["desktop-file-validate", desktop])
        a.add(PASS if v.returncode == 0 and not v.stdout.strip() else WARN, ".desktop",
              v.stdout.strip() or os.path.basename(desktop))
    elif desktop:
        a.add(PASS, ".desktop", os.path.basename(desktop))
    un = run([omastore, "uninstall", repo], env=env)
    leftover = [p for p in (exe, desktop) if p and os.path.exists(p)]
    a.add(PASS if un.returncode == 0 and not leftover else WARN, "Desinstalação",
          "limpa" if not leftover else "sobrou: " + ", ".join(leftover))
    return a


def report(repo: str, a: Audit) -> str:
    lines = [f"# OmaStore: {repo}", "", "| | Item | Detalhe |", "|---|---|---|"]
    for c in a.checks:
        detail = c["detail"].replace("\n", " ").replace("|", "\\|")
        lines.append(f'| {ICON[c["status"]]} | {c["item"]} | {detail} |')
    fixes = [c for c in a.checks if c["fix"] and c["status"] != PASS]
    if fixes:
        lines += ["", "## O que fazer", ""]
        lines += [f'- **{c["item"]}:** {c["fix"]}' for c in fixes]
    lines += ["", "Resultado: " + ("**não instalável pela OmaStore**" if a.failed else "**compatível com a OmaStore**")]
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("repo", help="owner/repo")
    ap.add_argument("--manifest", help="omastore.toml local a usar no lugar do publicado")
    ap.add_argument("--install", action="store_true", help="instala num HOME temporário e desinstala")
    ap.add_argument("--omastore", help="caminho do binário omastore")
    ap.add_argument("--json", action="store_true", help="saída em JSON")
    ap.add_argument("--keep", action="store_true", help="não apagar o HOME temporário")
    args = ap.parse_args()

    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+", args.repo):
        print("repo deve ser owner/repo", file=sys.stderr)
        return 2
    omastore = find_omastore(args.omastore)
    if not omastore:
        print("omastore não encontrado: instale a OmaStore ou passe --omastore /caminho/omastore", file=sys.stderr)
        return 2
    manifest_file = os.path.abspath(args.manifest) if args.manifest else None

    work = tempfile.mkdtemp(prefix="omastore-check-")
    try:
        a = audit(args.repo, omastore, manifest_file, args.install, work)
    finally:
        if args.keep:
            print(f"(HOME temporário mantido em {work})", file=sys.stderr)
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
