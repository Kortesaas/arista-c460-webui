VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO ?= go

.PHONY: build web backend dev clean check

build: web backend

web:
	cd web && npm ci --no-audit --no-fund && npm run build

# Static ARM64 binary for the C-460 (no libc dependency).
backend:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o build/c460-webui .

check:
	$(GO) vet ./...
	cd web && npm run typecheck

# Frontend dev server; proxies /api to C460_BACKEND (default http://127.0.0.1:18099,
# e.g. an SSH tunnel: ssh -L 18099:127.0.0.1:80 root@<ap>).
dev:
	cd web && npm run dev

clean:
	rm -rf build web/dist/assets web/dist/index.html web/dist/favicon.svg
