.PHONY: build run clean test

build:
	go build -o agent-chat ./cmd/agent

run: build
	./agent-chat

test:
	go test ./...

clean:
	rm -f agent-chat
