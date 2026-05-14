.PHONY: build test lint ci clean

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

ci: build test lint

clean:
	rm -rf bin/
