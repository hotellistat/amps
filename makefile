GOCMD=go
GOBUILD=$(GOCMD) build
BINARY_NAME=batchable
DIST_DIR=./dist/


dev:
	go run -race ./cmd/batchable/batchable.go

server:
	deno run --watch --allow-net --unstable hack/webserver.ts

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(DIST_DIR)$(BINARY_NAME) ./cmd/batchable/batchable.go

runTests:
	go test ./test/... -v --timeout 20s
