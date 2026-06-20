BINARY_NAME=terraform-provider-arubasdwan
INSTALL_DIR=~/.terraform.d/plugins/registry.terraform.io/florianschendel/arubasdwan/0.1.0/$(shell go env GOOS)_$(shell go env GOARCH)

.PHONY: build install clean fmt vet test

build:
	go build -o $(BINARY_NAME)

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY_NAME) $(INSTALL_DIR)/

clean:
	rm -f $(BINARY_NAME)

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./... -v

tidy:
	go mod tidy

all: fmt vet build
