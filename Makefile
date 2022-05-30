DATE=$(shell date -u +%Y-%m-%d)
VERSION=$(shell cat VERSION | sed 's/-dev//g')
EFFECTIVE_VERSION:=$(VERSION)-$(shell git rev-parse HEAD)

REGISTRY                     := eu.gcr.io/gardener-project/gardener
IMAGE_REPOSITORY             := $(REGISTRY)/reserved-resources-recommender

.PHONY: format
format:
	@go fmt

.PHONY: build
build:
	@env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o reserved_linux_amd64 -ldflags="-w -s" main.go

.PHONY: all
all: format build

.PHONY: revendor
revendor:
	@GO111MODULE=on go mod vendor
	@GO111MODULE=on go mod tidy

.PHONY: images
images:
	@echo "Building OCI image with version and tag $(EFFECTIVE_VERSION)"
	@echo "nerdctl build --build-arg EFFECTIVE_VERSION=$(EFFECTIVE_VERSION) -t $(IMAGE_REPOSITORY):$(EFFECTIVE_VERSION) -f Dockerfile . && nerdctl tag eu.gcr.io/gardener-project/gardener/reserved-resources-recommender:$(EFFECTIVE_VERSION) eu.gcr.io/gardener-project/gardener/reserved-resources-recommender:latest"
	@nerdctl build --build-arg EFFECTIVE_VERSION=$(EFFECTIVE_VERSION) -t $(IMAGE_REPOSITORY):$(EFFECTIVE_VERSION) -f Dockerfile .
	@nerdctl tag eu.gcr.io/gardener-project/gardener/reserved-resources-recommender:$(EFFECTIVE_VERSION) eu.gcr.io/gardener-project/gardener/reserved-resources-recommender:latest

.PHONY: push-images
push-images:
	@echo "Pushing OCI image with version and tag $(EFFECTIVE_VERSION)"
	@nerdctl push eu.gcr.io/gardener-project/gardener/reserved-resources-recommender:latest
