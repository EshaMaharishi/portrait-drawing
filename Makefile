BIN := bin/portrait-drawing

.PHONY: build module.tar.gz clean

build:
	go build -o $(BIN) .

module.tar.gz: build
	tar czf module.tar.gz $(BIN) meta.json

clean:
	rm -rf bin module.tar.gz
