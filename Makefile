.PHONY: frontend build run clean test

frontend:
	cd web && npx esbuild src/app.tsx --bundle --outdir=dist --minify \
		--loader:.css=css --jsx-factory=h --jsx-fragment=Fragment
	cp web/src/index.html web/dist/index.html

build: frontend
	go build -o agent-chat ./cmd/agent

run: build
	./agent-chat

test:
	go test ./...

clean:
	rm -rf agent-chat web/dist web/node_modules
