.PHONY: build run clean test

build:
	go build -o gohome ./cmd/agent

run: build
	./gohome

test:
	go test ./...

clean:
	rm -f gohome
