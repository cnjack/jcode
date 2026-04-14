BIN := jcode
PKG := ./cmd/jcode/

export GOFLAGS := -buildvcs=false

.PHONY: build run doctor version install clean build-web lint lint-go lint-web generate

lint: lint-go lint-web

lint-go:
	@echo "Linting Go..."
	golangci-lint run

lint-web:
	@echo "Linting frontend..."
	cd web && (pnpm install --frozen-lockfile 2>/dev/null || pnpm install)
	cd web && pnpm lint

generate:
	@echo "Generating code..."
	go generate ./internal/model/...

build-web:
	@echo "Building frontend..."
	cd web && (pnpm install --frozen-lockfile 2>/dev/null || pnpm install)
	cd web && npx vite build

build: generate build-web
	go build -o $(BIN) $(PKG)

install: generate build-web
	go install $(PKG)

run:
	go run $(PKG)

doctor:
	go run $(PKG) --doctor

version:
	go run $(PKG) --version

clean:
	rm -f $(BIN)
	rm -rf internal/web/dist
