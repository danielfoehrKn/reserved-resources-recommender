DATE=$(shell date -u +%Y-%m-%d)
VERSION=$(shell cat VERSION | sed 's/-dev//g')

.PHONY: format
format:
	@./hack/format.sh main.go

.PHONY: build
build:
	@env GOOS=linux GOARCH=amd64 go build -o kube_reserved_linux_amd64 main.go

.PHONY: all
all: format build

.PHONY: revendor
revendor:
	@GO111MODULE=on go mod vendor
	@GO111MODULE=on go mod tidy
