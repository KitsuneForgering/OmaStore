# OmaStore — build and tests.
# `make help` lists the targets.

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

# Extra arguments: make test-backend TESTFLAGS='-run TestName ./internal/index'
TESTFLAGS ?= -race ./...
# CLI arguments: make run ARGS='index --force'
ARGS ?=

GO_CMDS := omastore omastored

# The version the interface reports. A release build gets the tag
# (`make dist VERSION=v1.2.3`); a development build, the last tag of the
# checkout. GUI_VERSION=1.2.3 builds a tree that says so.
GUI_VERSION ?= $(patsubst v%,%,$(shell git describe --tags --abbrev=0 2>/dev/null || echo dev))

# Installation (used by the PKGBUILD): make install DESTDIR=... PREFIX=/usr
PREFIX  ?= /usr/local
DESTDIR ?=
# Extra go build flags (the PKGBUILD passes -trimpath etc. via GOFLAGS).
GOLDFLAGS ?=

.DEFAULT_GOAL := all
.PHONY: all help build backend frontend test test-backend test-frontend check \
        vet fmt fmt-check tidy run run-daemon run-gui clean check-cmake install uninstall release dist \
        pkgbuild-bin test-skills test-installer

all: build ## Build backend and frontend

help: ## List the targets
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: backend frontend ## Build everything

# --- Backend (Go) -----------------------------------------------------------

backend: $(addprefix $(BIN)/,$(GO_CMDS)) ## Build the Go binaries into bin/

# FORCE: let the Go cache decide whether to rebuild.
$(BIN)/%: FORCE
	@mkdir -p $(BIN)
	cd $(BACKEND) && $(GO) build -ldflags '$(GOLDFLAGS)' -o ../$@ ./cmd/$*

FORCE:

test-backend: ## Backend tests (TESTFLAGS='-run X ./pkg' to filter)
	cd $(BACKEND) && $(GO) test $(TESTFLAGS)

vet: ## go vet
	cd $(BACKEND) && $(GO) vet ./...

fmt: ## Format the Go code
	cd $(BACKEND) && gofmt -w .

fmt-check: ## Fail if any Go code is not gofmt'ed
	@cd $(BACKEND) && out="$$(gofmt -l .)"; \
		if [ -n "$$out" ]; then echo "files not gofmt'ed:"; echo "$$out"; exit 1; fi

tidy: ## go mod tidy
	cd $(BACKEND) && $(GO) mod tidy

run: $(BIN)/omastore ## Run the CLI (ARGS='index')
	./$(BIN)/omastore $(ARGS)

run-daemon: $(BIN)/omastored ## Run the daemon
	./$(BIN)/omastored $(ARGS)

# --- Frontend (C++/Qt) ------------------------------------------------------

check-cmake:
	@command -v $(CMAKE) >/dev/null || { echo "cmake not found (sudo pacman -S cmake ninja)"; exit 1; }

# The .type-<BUILD_TYPE>-<version> stamp forces a reconfigure when the build
# type or the reported version changes (CMake keeps both in its cache).
$(BUILD_DIR)/.type-$(BUILD_TYPE)-$(GUI_VERSION): $(FRONTEND)/CMakeLists.txt | check-cmake
	$(CMAKE) -S $(FRONTEND) -B $(BUILD_DIR) -G $(GENERATOR) -DCMAKE_BUILD_TYPE=$(BUILD_TYPE) -DOMASTORE_VERSION=$(GUI_VERSION)
	rm -f $(BUILD_DIR)/.type-*
	touch $@

frontend: $(BUILD_DIR)/.type-$(BUILD_TYPE)-$(GUI_VERSION) ## Build the frontend
	$(CMAKE) --build $(BUILD_DIR)

run-gui: frontend $(BIN)/omastored ## Build and open the interface (the built daemon stays in bin/)
	OMASTORED="$(CURDIR)/$(BIN)/omastored" ./$(BUILD_DIR)/omastore-gui $(ARGS)

test-frontend: frontend ## Frontend tests (ctest)
	QT_QPA_PLATFORM=offscreen $(CTEST) --test-dir $(BUILD_DIR) --output-on-failure

# --- Installation ----------------------------------------------------------

# The release uses its own build directory: CMake keeps
# CMAKE_BUILD_TYPE in its cache and would not reconfigure the Debug frontend/build.
RELEASE_DIR := $(FRONTEND)/build-release

release: ## Build everything in Release mode (frontend in frontend/build-release)
	$(MAKE) backend
	$(MAKE) frontend BUILD_TYPE=Release BUILD_DIR=$(RELEASE_DIR)

install: ## Install into $(DESTDIR)$(PREFIX) (after make release)
	install -Dm755 $(BIN)/omastore  $(DESTDIR)$(PREFIX)/bin/omastore
	install -Dm755 $(BIN)/omastored $(DESTDIR)$(PREFIX)/bin/omastored
	DESTDIR=$(DESTDIR) $(CMAKE) --install $(RELEASE_DIR) --prefix $(PREFIX)
	install -Dm644 packaging/desktop/omastore.desktop $(DESTDIR)$(PREFIX)/share/applications/omastore.desktop
	install -Dm644 packaging/desktop/omastore-mark.svg $(DESTDIR)$(PREFIX)/share/icons/hicolor/scalable/apps/omastore.svg
	@# The units name the binaries by absolute path: point them at this PREFIX
	@# (with PREFIX=/usr/local they ran a /usr/bin/omastored that does not exist).
	@install -d $(DESTDIR)$(PREFIX)/lib/systemd/user
	for unit in omastored.service omastored.socket omastore-index.service omastore-index.timer; do \
		sed 's|/usr/bin/|$(PREFIX)/bin/|g' packaging/systemd/$$unit > $(DESTDIR)$(PREFIX)/lib/systemd/user/$$unit || exit 1; \
		chmod 644 $(DESTDIR)$(PREFIX)/lib/systemd/user/$$unit; \
	done
	install -Dm644 LICENSE $(DESTDIR)$(PREFIX)/share/licenses/omastore/LICENSE
	install -Dm644 docs/ipc.md $(DESTDIR)$(PREFIX)/share/doc/omastore/ipc.md
	@# Omarchy post-update hook; install.sh writes its own copy, a package cannot write to $$HOME.
	@install -d $(DESTDIR)$(PREFIX)/share/omastore/omarchy
	sed -e 's|@BINDIR@|$(PREFIX)/bin|g' -e 's|@DATADIR@|$(PREFIX)/share|g' packaging/omarchy/omastore.hook.in > $(DESTDIR)$(PREFIX)/share/omastore/omarchy/omastore.hook
	chmod 644 $(DESTDIR)$(PREFIX)/share/omastore/omarchy/omastore.hook
	@# Skills for app authors; install.sh copies them into the coding agents' skill directories.
	cd skills && find omastore-* -type f ! -path '*/__pycache__/*' | LC_ALL=C sort | while IFS= read -r f; do \
		case "$$f" in */scripts/*) mode=755 ;; *) mode=644 ;; esac; \
		install -Dm$$mode "$$f" "$(DESTDIR)$(PREFIX)/share/omastore/skills/$$f" || exit 1; \
	done
	@# Over a running older OmaStore: reload the units, stop the daemon the old
	@# binary still runs, warn about a per-user install that shadows this one.
	@# Only on a live system; packages (DESTDIR) are left to their manager.
	@if [ -z "$(DESTDIR)" ]; then sh packaging/post-install.sh $(PREFIX)/bin $(PREFIX)/share; fi

uninstall: ## Remove the system install, plus the user's install.sh install and cache
	@# The per-user part (install.sh, ~/.local) runs as the invoking user, also under sudo.
	@if [ -z "$(DESTDIR)" ]; then \
		if [ "$$(id -u)" -ne 0 ]; then \
			sh packaging/install.sh --uninstall; \
		elif [ -n "$${SUDO_USER:-}" ] && [ "$$SUDO_USER" != root ]; then \
			sudo -u "$$SUDO_USER" -H env XDG_RUNTIME_DIR=/run/user/$$(id -u "$$SUDO_USER") \
				sh packaging/install.sh --uninstall; \
		fi; \
	fi
	rm -f $(DESTDIR)$(PREFIX)/bin/omastore $(DESTDIR)$(PREFIX)/bin/omastored $(DESTDIR)$(PREFIX)/bin/omastore-gui
	rm -f $(DESTDIR)$(PREFIX)/share/applications/omastore.desktop
	rm -f $(DESTDIR)$(PREFIX)/share/icons/hicolor/scalable/apps/omastore.svg
	rm -f $(DESTDIR)$(PREFIX)/lib/systemd/user/omastored.service $(DESTDIR)$(PREFIX)/lib/systemd/user/omastored.socket
	rm -f $(DESTDIR)$(PREFIX)/lib/systemd/user/omastore-index.service $(DESTDIR)$(PREFIX)/lib/systemd/user/omastore-index.timer
	rm -rf $(DESTDIR)$(PREFIX)/share/licenses/omastore $(DESTDIR)$(PREFIX)/share/doc/omastore $(DESTDIR)$(PREFIX)/share/omastore
	@if [ -z "$(DESTDIR)" ]; then \
		if command -v update-desktop-database >/dev/null 2>&1 && [ -d $(PREFIX)/share/applications ]; then \
			update-desktop-database -q $(PREFIX)/share/applications 2>/dev/null || :; fi; \
		if command -v gtk-update-icon-cache >/dev/null 2>&1 && [ -f $(PREFIX)/share/icons/hicolor/index.theme ]; then \
			gtk-update-icon-cache -q -t -f $(PREFIX)/share/icons/hicolor 2>/dev/null || :; fi; \
	fi

# --- Distribution -----------------------------------------------------------

# Tarball version: VERSION=v1.2.3 (the "v" is dropped from the file name).
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
DIST_VERSION := $(patsubst v%,%,$(VERSION))
DIST_ARCH ?= $(shell uname -m)
DIST_DIR := dist
DIST_NAME := omastore-$(DIST_VERSION)-$(DIST_ARCH)-linux.tar.gz
# Fixed dates on the tarball files (reproducible): date of the last commit.
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)

dist: ## Release tarball in dist/ (VERSION=v1.2.3), with .sha256
	rm -rf $(DIST_DIR)/root
	$(MAKE) release GUI_VERSION=$(DIST_VERSION) GOFLAGS='-trimpath -buildvcs=false' GOLDFLAGS='-s -w'
	$(MAKE) install DESTDIR=$(CURDIR)/$(DIST_DIR)/root PREFIX=/usr
	tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@$(SOURCE_DATE_EPOCH) \
		-C $(DIST_DIR)/root -cf - usr | gzip -n9 > $(DIST_DIR)/$(DIST_NAME)
	cd $(DIST_DIR) && sha256sum $(DIST_NAME) > $(DIST_NAME).sha256
	rm -rf $(DIST_DIR)/root
	@echo "$(DIST_DIR)/$(DIST_NAME)"

pkgbuild-bin: ## Generate packaging/arch-bin/PKGBUILD from the tarball in dist/
	@test -f $(DIST_DIR)/$(DIST_NAME).sha256 || { echo "run make dist VERSION=$(VERSION) first"; exit 1; }
	sed -e 's/@VERSION@/$(DIST_VERSION)/' \
	    -e "s/@SHA256@/$$(cut -d' ' -f1 $(DIST_DIR)/$(DIST_NAME).sha256)/" \
	    packaging/arch-bin/PKGBUILD.in > packaging/arch-bin/PKGBUILD
	cd packaging/arch-bin && makepkg --printsrcinfo > .SRCINFO

# --- Aggregates -------------------------------------------------------------

test-skills: $(BIN)/omastore ## The skills' Python validator == omastore lint-manifest
	python3 skills/tests/compare_validators.py $(BIN)/omastore

test-installer: ## Simulated local installation, no network
	sh tests/install-shell.sh
	sh tests/post-install.sh

test: test-backend test-frontend test-skills test-installer ## All tests

check: fmt-check vet test-backend test-installer ## What the backend CI runs

clean: ## Remove build artifacts
	rm -rf $(BIN) $(BUILD_DIR) $(RELEASE_DIR) $(DIST_DIR)
