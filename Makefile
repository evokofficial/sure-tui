.PHONY: build run test

build:
	go build -o bin/sure-tui ./cmd/sure-tui

run: build
	./bin/sure-tui

test:
	go test ./...
