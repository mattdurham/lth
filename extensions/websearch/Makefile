.PHONY: build test clean tidy

VERSION ?= v1.0.0
OUTPUT ?= main.wasm

# Ensure Go modules are up to date
tidy:
	go mod tidy

# Test the Go code (without WASM-specific features)
test: tidy
	go build -o test_build main.go

# Build the websearch extension for WASM using TinyGo
# Requires: docker and tinygo support in lth host
build: tidy
	docker run --rm -v "$$(pwd)":/app -w /app tinygo/tinygo:0.32.0 \
		tinygo build -o $(OUTPUT) -target wasip1 main.go

clean:
	rm -f $(OUTPUT) test_build

# Run the extension (requires lth host)
run: build
	@echo "Extension built successfully. Run via lth host."
