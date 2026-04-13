BIN := jcode
PKG := ./cmd/jcode/

export GOFLAGS := -buildvcs=false

.PHONY: build run doctor version install clean build-web

build-web:
	@echo "Building frontend..."
	cd web && (pnpm install --frozen-lockfile 2>/dev/null || pnpm install)
	cd web && npx vite build

build: build-web
	go build -o $(BIN) $(PKG)

install: build-web
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
