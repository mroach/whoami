GOPKG ?= github.com/mroach/whoami
IMAGE_TAG ?= ghcr.io/mroach/whoami:latest
GIT_SHA ?= $(shell git log -1 --format="%h")
GIT_TS ?= $(shell git log -1 --format="%at")

.PHONY: image
image:
	docker build -t $(IMAGE_TAG) --build-arg git_sha=$(GIT_SHA) --build-arg git_ts=$(GIT_TS)  .

.PHONY: push-image
push-image:
	docker push $(IMAGE_TAG)

.PHONY: release
release: image push-image

clean:
	mkdir -p bin
	rm -f bin/whoami

bin/whoami:
	mkdir -p $(@D)
	CGO_ENABLED=0 GOOS=linux \
		go build -o $@ \
	  	-ldflags="-X $(GOPKG)/internal/version.CommitHash=$(GIT_SHA) -X $(GOPKG)/internal/version.commitTimestamp=$(GIT_TS)"
