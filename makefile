GOCMD=go
GOBUILD=$(GOCMD) build
BINARY_NAME=amps
DIST_DIR=./dist/
GOARCH ?= amd64


dev:
	go run -race ./cmd/amps/amps.go

server:
	go run ./hack/webserver.go

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) $(GOBUILD) -o $(DIST_DIR)$(BINARY_NAME) ./cmd/amps/amps.go

runTests:
	go test ./test/... -v --timeout 20s
