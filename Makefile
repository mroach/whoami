IMAGE_TAG ?= ghcr.io/mroach/whoami:latest
APP_VERSION ?= $(shell git rev-parse --short HEAD)

.PHONY: image
image:
	docker build -t $(IMAGE_TAG) --build-arg app_version=$(APP_VERSION) .

.PHONY: push-image
push-image:
	docker push $(IMAGE_TAG)

.PHONY: release
release: image push-image
