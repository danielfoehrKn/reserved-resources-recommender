DATE=$(shell date -u +%Y-%m-%d)
VERSION=$(shell cat VERSION | sed 's/-dev//g')
EFFECTIVE_VERSION:=$(VERSION)-$(shell git rev-parse HEAD)

REGISTRY                               := eu.gcr.io/gardener-project/gardener
IMAGE_REPOSITORY             := $(REGISTRY)/better-resource-reservations

.PHONY: format
format:
	@./hack/format.sh main.go

.PHONY: build
build:
	@env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o reserved_linux_amd64 -ldflags="-w -s" main.go

.PHONY: all
all: format build

.PHONY: revendor
revendor:
	@GO111MODULE=on go mod vendor
	@GO111MODULE=on go mod tidy

.PHONY: docker-images
docker-images:
	@echo "Building docker image with version and tag $(EFFECTIVE_VERSION)"
	@docker build --build-arg EFFECTIVE_VERSION=$(EFFECTIVE_VERSION)  -t $(IMAGE_REPOSITORY):$(EFFECTIVE_VERSION) -t $(IMAGE_REPOSITORY):latest  -f Dockerfile .
