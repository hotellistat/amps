GOCMD=go
GOBUILD=$(GOCMD) build
BINARY_NAME=batchable
DIST_DIR=./dist/


start: so
	go run ./main.go

so:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -buildmode=plugin -o shims/nats/shim.so shims/nats/shim.go

build:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(DIST_DIR)$(BINARY_NAME) ./main.go
	GOOS=linux GOARCH=amd64 $(GOBUILD) -buildmode=plugin -o $(DIST_DIR)shims/nats/shim.so shims/nats/shim.go

