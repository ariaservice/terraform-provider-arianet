HOSTNAME=registry.terraform.io
NAMESPACE=ariaservice
NAME=arianet
BINARY=terraform-provider-$(NAME)
VERSION?=0.1.0-dev
OS_ARCH=$(shell go env GOOS)_$(shell go env GOARCH)

default: build

.PHONY: build
build:
	go build -o $(BINARY)

.PHONY: install
install:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY)
	mkdir -p ~/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS_ARCH)
	mv $(BINARY) ~/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS_ARCH)/$(BINARY)_v$(VERSION)

.PHONY: fmt
fmt:
	gofmt -s -w .
	terraform fmt -recursive examples

.PHONY: lint
lint:
	go vet ./...
	test -z "$$(gofmt -l .)"

.PHONY: test
test:
	go test -cover -timeout=10m ./...

# Acceptance tests create real, billed resources. Needs ARIANET_TOKEN.
.PHONY: testacc
testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./internal/provider/ -run '^TestAcc'

.PHONY: docs
docs:
	go generate ./...
