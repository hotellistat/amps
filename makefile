GOCMD=go
GOBUILD=$(GOCMD) build
BINARY_NAME=amps
DIST_DIR=./dist/


dev:
	go run -race ./cmd/amps/amps.go

server:
	go run ./hack/webserver.go

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(DIST_DIR)$(BINARY_NAME) ./cmd/amps/amps.go

runTests:
	go test ./test/... -v --timeout 20s
