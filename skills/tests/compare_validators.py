#!/usr/bin/env python3
"""Compara o validador em Python da skill com o `omastore lint-manifest`.

Para cada manifesto em tests/manifests/, os dois precisam concordar no código
de saída e nos campos com erro. O prefixo do nome diz o esperado:
ok-* (sem erro nem aviso), aviso-* (só avisos), erro-* (ao menos um erro).

Uso: compare_validators.py <caminho do binário omastore>
"""
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
VALIDATOR = os.path.join(HERE, "..", "omastore-manifest", "scripts", "validate_manifest.py")
LINE = re.compile(r"^(erro|aviso): ([^:]+): ")


def run(cmd):
    p = subprocess.run(cmd, capture_output=True, text=True)
    out = p.stdout + p.stderr
    # A linha de resumo da CLI ("erro: manifesto com erros: N") não é um campo.
    fields = {(m.group(1), m.group(2)) for m in map(LINE.match, out.splitlines())
              if m and m.group(2) != "manifesto com erros"}
    return p.returncode, fields, out


def main():
    omastore = sys.argv[1]
    env_ok = True
    for name in sorted(os.listdir(os.path.join(HERE, "manifests"))):
        path = os.path.join(HERE, "manifests", name)
        go_code, go_fields, go_out = run([omastore, "lint-manifest", path])
        py_code, py_fields, py_out = run([sys.executable, VALIDATOR, path])
        problems = []
        if go_code != py_code:
            problems.append(f"código de saída: go={go_code} python={py_code}")
        if {f for f in go_fields if f[0] == "erro"} != {f for f in py_fields if f[0] == "erro"}:
            problems.append(f"campos com erro: go={sorted(go_fields)} python={sorted(py_fields)}")
        expect_err = name.startswith("erro-")
        if (go_code != 0) != expect_err:
            problems.append(f"esperado {'erro' if expect_err else 'sucesso'}, go saiu com {go_code}")
        if name.startswith("ok-") and ("aviso" in go_out or "aviso" in py_out):
            problems.append("ok-* não deveria ter avisos")
        if name.startswith("aviso-") and not ("aviso" in go_out and "aviso" in py_out):
            problems.append("aviso-* deveria ter aviso nos dois")
        status = "ok " if not problems else "FALHA"
        print(f"{status} {name}")
        for p in problems:
            env_ok = False
            print(f"      {p}\n      go:     {go_out.strip()}\n      python: {py_out.strip()}")
    return 0 if env_ok else 1


if __name__ == "__main__":
    sys.exit(main())
