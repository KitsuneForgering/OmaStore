# OmaStore — compilação e testes.
# `make help` lista os alvos.

BACKEND   := backend
FRONTEND  := frontend
BIN       := bin
BUILD_DIR := $(FRONTEND)/build

GO         ?= go
CMAKE      ?= cmake
CTEST      ?= ctest
BUILD_TYPE ?= Debug
GENERATOR  ?= Ninja

export CGO_ENABLED := 1

# Argumentos extras: make test-backend TESTFLAGS='-run TestNome ./internal/index'
TESTFLAGS ?= -race ./...
# Argumentos da CLI: make run ARGS='index --force'
ARGS ?=

GO_CMDS := omastore omastored

# Instalação (usada pelo PKGBUILD): make install DESTDIR=... PREFIX=/usr
PREFIX  ?= /usr/local
DESTDIR ?=
# Flags extras do go build (o PKGBUILD passa -trimpath etc. via GOFLAGS).
GOLDFLAGS ?=

.DEFAULT_GOAL := all
.PHONY: all help build backend frontend test test-backend test-frontend check \
        vet fmt fmt-check tidy run run-daemon run-gui clean check-cmake install uninstall release dist \
        pkgbuild-bin test-skills

all: build ## Compila backend e frontend

help: ## Lista os alvos
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: backend frontend ## Compila tudo

# --- Backend (Go) -----------------------------------------------------------

backend: $(addprefix $(BIN)/,$(GO_CMDS)) ## Compila os binários Go em bin/

# FORCE: deixa o cache do Go decidir se precisa recompilar.
$(BIN)/%: FORCE
	@mkdir -p $(BIN)
	cd $(BACKEND) && $(GO) build -ldflags '$(GOLDFLAGS)' -o ../$@ ./cmd/$*

FORCE:

test-backend: ## Testes do backend (TESTFLAGS='-run X ./pkg' para filtrar)
	cd $(BACKEND) && $(GO) test $(TESTFLAGS)

vet: ## go vet
	cd $(BACKEND) && $(GO) vet ./...

fmt: ## Formata o código Go
	cd $(BACKEND) && gofmt -w .

fmt-check: ## Falha se houver código Go sem gofmt
	@cd $(BACKEND) && out="$$(gofmt -l .)"; \
		if [ -n "$$out" ]; then echo "arquivos sem gofmt:"; echo "$$out"; exit 1; fi

tidy: ## go mod tidy
	cd $(BACKEND) && $(GO) mod tidy

run: $(BIN)/omastore ## Roda a CLI (ARGS='index')
	./$(BIN)/omastore $(ARGS)

run-daemon: $(BIN)/omastored ## Roda o daemon
	./$(BIN)/omastored $(ARGS)

# --- Frontend (C++/Qt) ------------------------------------------------------

check-cmake:
	@command -v $(CMAKE) >/dev/null || { echo "cmake não encontrado (sudo pacman -S cmake ninja)"; exit 1; }

# A marca .type-<BUILD_TYPE> força reconfigurar quando o tipo de build muda
# (o CMake guarda CMAKE_BUILD_TYPE no cache).
$(BUILD_DIR)/.type-$(BUILD_TYPE): $(FRONTEND)/CMakeLists.txt | check-cmake
	$(CMAKE) -S $(FRONTEND) -B $(BUILD_DIR) -G $(GENERATOR) -DCMAKE_BUILD_TYPE=$(BUILD_TYPE)
	rm -f $(BUILD_DIR)/.type-*
	touch $@

frontend: $(BUILD_DIR)/.type-$(BUILD_TYPE) ## Compila o frontend
	$(CMAKE) --build $(BUILD_DIR)

run-gui: frontend $(BIN)/omastored ## Compila e abre a interface (o daemon já compilado fica em bin/)
	OMASTORED="$(CURDIR)/$(BIN)/omastored" ./$(BUILD_DIR)/omastore-gui $(ARGS)

test-frontend: frontend ## Testes do frontend (ctest)
	QT_QPA_PLATFORM=offscreen $(CTEST) --test-dir $(BUILD_DIR) --output-on-failure

# --- Instalação -------------------------------------------------------------

# O release usa um diretório de build próprio: o CMake guarda o
# CMAKE_BUILD_TYPE no cache e não reconfiguraria o frontend/build de Debug.
RELEASE_DIR := $(FRONTEND)/build-release

release: ## Compila tudo em modo Release (frontend em frontend/build-release)
	$(MAKE) backend
	$(MAKE) frontend BUILD_TYPE=Release BUILD_DIR=$(RELEASE_DIR)

install: ## Instala em $(DESTDIR)$(PREFIX) (após make release)
	install -Dm755 $(BIN)/omastore  $(DESTDIR)$(PREFIX)/bin/omastore
	install -Dm755 $(BIN)/omastored $(DESTDIR)$(PREFIX)/bin/omastored
	DESTDIR=$(DESTDIR) $(CMAKE) --install $(RELEASE_DIR) --prefix $(PREFIX)
	install -Dm644 packaging/desktop/omastore.desktop $(DESTDIR)$(PREFIX)/share/applications/omastore.desktop
	install -Dm644 packaging/desktop/omastore.svg $(DESTDIR)$(PREFIX)/share/icons/hicolor/scalable/apps/omastore.svg
	install -Dm644 packaging/systemd/omastored.service $(DESTDIR)$(PREFIX)/lib/systemd/user/omastored.service
	install -Dm644 packaging/systemd/omastored.socket  $(DESTDIR)$(PREFIX)/lib/systemd/user/omastored.socket
	install -Dm644 packaging/systemd/omastore-index.service $(DESTDIR)$(PREFIX)/lib/systemd/user/omastore-index.service
	install -Dm644 packaging/systemd/omastore-index.timer   $(DESTDIR)$(PREFIX)/lib/systemd/user/omastore-index.timer
	install -Dm644 LICENSE $(DESTDIR)$(PREFIX)/share/licenses/omastore/LICENSE
	install -Dm644 docs/ipc.md $(DESTDIR)$(PREFIX)/share/doc/omastore/ipc.md

uninstall: ## Remove o que o install instalou
	rm -f $(DESTDIR)$(PREFIX)/bin/omastore $(DESTDIR)$(PREFIX)/bin/omastored $(DESTDIR)$(PREFIX)/bin/omastore-gui
	rm -f $(DESTDIR)$(PREFIX)/share/applications/omastore.desktop
	rm -f $(DESTDIR)$(PREFIX)/share/icons/hicolor/scalable/apps/omastore.svg
	rm -f $(DESTDIR)$(PREFIX)/lib/systemd/user/omastored.service $(DESTDIR)$(PREFIX)/lib/systemd/user/omastored.socket
	rm -f $(DESTDIR)$(PREFIX)/lib/systemd/user/omastore-index.service $(DESTDIR)$(PREFIX)/lib/systemd/user/omastore-index.timer
	rm -rf $(DESTDIR)$(PREFIX)/share/licenses/omastore $(DESTDIR)$(PREFIX)/share/doc/omastore

# --- Distribuição -----------------------------------------------------------

# Versão do tarball: VERSION=v1.2.3 (o "v" é removido do nome do arquivo).
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
DIST_VERSION := $(patsubst v%,%,$(VERSION))
DIST_ARCH ?= $(shell uname -m)
DIST_DIR := dist
DIST_NAME := omastore-$(DIST_VERSION)-$(DIST_ARCH)-linux.tar.gz
# Datas fixas nos arquivos do tarball (reprodutível): data do último commit.
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)

dist: ## Tarball da release em dist/ (VERSION=v1.2.3), com .sha256
	rm -rf $(DIST_DIR)/root
	$(MAKE) release GOFLAGS='-trimpath -buildvcs=false' GOLDFLAGS='-s -w'
	$(MAKE) install DESTDIR=$(CURDIR)/$(DIST_DIR)/root PREFIX=/usr
	tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@$(SOURCE_DATE_EPOCH) \
		-C $(DIST_DIR)/root -cf - usr | gzip -n9 > $(DIST_DIR)/$(DIST_NAME)
	cd $(DIST_DIR) && sha256sum $(DIST_NAME) > $(DIST_NAME).sha256
	rm -rf $(DIST_DIR)/root
	@echo "$(DIST_DIR)/$(DIST_NAME)"

pkgbuild-bin: ## Gera packaging/arch-bin/PKGBUILD a partir do tarball do dist/
	@test -f $(DIST_DIR)/$(DIST_NAME).sha256 || { echo "rode make dist VERSION=$(VERSION) antes"; exit 1; }
	sed -e 's/@VERSION@/$(DIST_VERSION)/' \
	    -e "s/@SHA256@/$$(cut -d' ' -f1 $(DIST_DIR)/$(DIST_NAME).sha256)/" \
	    packaging/arch-bin/PKGBUILD.in > packaging/arch-bin/PKGBUILD
	cd packaging/arch-bin && makepkg --printsrcinfo > .SRCINFO

# --- Agregados --------------------------------------------------------------

test-skills: $(BIN)/omastore ## Validador Python das skills == omastore lint-manifest
	python3 skills/tests/compare_validators.py $(BIN)/omastore

test: test-backend test-frontend test-skills ## Todos os testes

check: fmt-check vet test-backend ## O que a CI do backend roda

clean: ## Remove artefatos de build
	rm -rf $(BIN) $(BUILD_DIR) $(RELEASE_DIR) $(DIST_DIR)
