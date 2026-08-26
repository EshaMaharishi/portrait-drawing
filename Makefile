BIN := bin/portrait-drawing
ENTRYPOINT := dist/index.html

.PHONY: build web module.tar.gz test clean setup

build:
	go build -o $(BIN) .

# Build the web app into dist/ (served at viamapplications.com).
web $(ENTRYPOINT): $(wildcard web/src/* web/src/lib/* web/*.ts web/*.html web/package.json)
	cd web && npm ci && npm run build

module.tar.gz: build $(ENTRYPOINT)
	tar czf module.tar.gz $(BIN) meta.json dist

# Install Node.js in the Viam cloud build environment (Debian, root) when it
# is missing, so the web app can be built there.
setup:
	@if ! command -v npm >/dev/null 2>&1; then \
		apt-get update && apt-get install -y curl ca-certificates && \
		curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
		apt-get install -y nodejs; \
	fi
	node --version && npm --version

test:
	go test ./...

clean:
	rm -rf bin dist module.tar.gz
