# Distribuição: releases, AUR e o próprio OmaStore no catálogo

> **Status:** itens 1 e 2 implementados na Fase 14 (`make dist`,
> `.github/workflows/release.yml`, `packaging/arch-bin/`). O item 3 continua
> em aberto: o tarball `omastore-<versão>-x86_64-linux.tar.gz` segue as
> regras da própria loja, mas a instalação dela por ela mesma é recusada de
> propósito (os comandos `omastore*` são reservados); a loja deve ser
> gerenciada pelo pacman.

## Evidência

- O `PKGBUILD` (`packaging/arch/`) gera `omastore-git`, que compila do código
  e exige `go`, `cmake` e `ninja` na máquina do usuário. Com a simulação das
  etapas de build e check, o pacote levou alguns minutos para compilar.
- O projeto segue as próprias regras de publicação ([`../autores.md`](../autores.md)),
  mas ainda não publica releases: a OmaStore não conseguiria se instalar
  (nem se atualizar) por ela mesma.

## Proposta

1. **Workflow de release** (`.github/workflows/release.yml`, disparado por tag
   `v*`): compila backend e frontend para `x86_64` e `aarch64` e publica
   `omastore-<versão>-<arch>-linux.tar.gz` com a árvore de `make install`.
   Adicionar `actions/attest-build-provenance` (ver [confianca.md](confianca.md)).
2. **Dois pacotes no AUR:** `omastore-git` (atual) e `omastore-bin`, que baixa
   o tarball da release e confere o sha256. Instala em segundos.
3. **Topic `omarchy` no próprio repositório:** a loja aparece no catálogo e
   pode se atualizar como qualquer app. Cuidado: o lançador `omastore` é nome
   reservado (o instalador recusa), então a atualização da loja por ela mesma
   precisa de tratamento especial ou deve ficar a cargo do pacman.

## Custo

Pequeno para o workflow e o `-bin`. O item 3 exige decidir quem gerencia a
instalação da própria loja (pacman ou OmaStore) para não haver duas cópias.

## Riscos

- Cross-compilar o frontend Qt para `aarch64` no CI exige um runner ARM ou
  QEMU; começar só com `x86_64`.
