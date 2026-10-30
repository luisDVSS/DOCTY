BINARY      := docty
INSTALL_DIR ?= $(HOME)/.local/bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/luisdvss/docty/internal/cli.version=$(VERSION) \
           -X github.com/luisdvss/docty/internal/cli.commit=$(COMMIT) \
           -X github.com/luisdvss/docty/internal/cli.date=$(DATE)

.PHONY: build test vet install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/docty

test:
	go test ./...

vet:
	go vet ./...

install:
	@mkdir -p "$(INSTALL_DIR)"
	GOBIN="$(INSTALL_DIR)" go install -ldflags "$(LDFLAGS)" ./cmd/docty
	@case ":$$PATH:" in \
		*":$(INSTALL_DIR):"*) \
			echo "instalado en $(INSTALL_DIR)/$(BINARY)";; \
		*) \
			echo "aviso: $(INSTALL_DIR) no está en PATH"; \
			echo "       añade a tu shell: export PATH=\"$(INSTALL_DIR):\$$PATH\"";; \
	esac

clean:
	rm -rf bin
