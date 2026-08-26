BIN := bin/portrait-drawing
ENTRYPOINT := web/dist/index.html

.PHONY: build web module.tar.gz test clean setup

# The Go binary embeds web/dist (web/server.go), so the web app must be
# built first.
build: $(ENTRYPOINT)
	go build -o $(BIN) .

# Build the web app into web/dist/ (served at viamapplications.com and
# embedded in the binary for the local webapp component).
web $(ENTRYPOINT): $(wildcard web/src/* web/src/lib/* web/*.ts web/*.html web/package.json)
	cd web && npm ci && npm run build

module.tar.gz: build $(ENTRYPOINT)
	tar czf module.tar.gz $(BIN) meta.json web/dist

# Install Node.js in the Viam cloud build environment (Debian, root) when it
# is missing, so the web app can be built there.
setup:
	@if ! command -v npm >/dev/null 2>&1; then \
		apt-get update && apt-get install -y curl ca-certificates && \
		curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
		apt-get install -y nodejs; \
	fi
	node --version && npm --version

test: $(ENTRYPOINT)
	go test ./...

clean:
	rm -rf bin web/dist module.tar.gz
