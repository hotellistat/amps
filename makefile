GOCMD=go
GOBUILD=$(GOCMD) build
BINARY_NAME=batchable
DIST_DIR=./dist/


build:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(DIST_DIR)$(BINARY_NAME) ./main.go
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(GOBUILD) -buildmode=plugin -o $(DIST_DIR)shims/nats/shim.so shims/nats/shim.go

so:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -buildmode=plugin -o shims/nats/shim.so shims/nats/shim.go