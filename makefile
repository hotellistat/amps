GOCMD=go
GOBUILD=$(GOCMD) build
BINARY_NAME=batchable
DIST_DIR=./dist/


dev:
	go run ./cmd/batchable/batchable.go

server:
	go run ./test/webserver/webserver.go

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(DIST_DIR)$(BINARY_NAME) ./cmd/batchable/batchable.go

test:
	go test -cover ./cmd/... -v --timeout 20s
