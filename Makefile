# Registry / image config — override on the command line or via env.
#   make deploy REGISTRY=ghcr.io/yourorg
#   make deploy REGISTRY=docker.io/yourname VERSION=1.2.3
REGISTRY  ?= ghcr.io/dsandor
IMAGE     ?= tribal-knowledge
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: web build test clean image deploy

web:
	cd web && npm install && npm run build

build: web
	CGO_ENABLED=1 go build \
		-ldflags "-X github.com/dsandor/memory/internal/web.buildVersion=$(VERSION)" \
		-o tribal-knowledge \
		./cmd/server/

test:
	CGO_ENABLED=1 go test ./...

clean:
	rm -f tribal-knowledge
	rm -rf internal/web/dist
	mkdir -p internal/web/dist
	echo '<!DOCTYPE html><html><body>Run make web</body></html>' > internal/web/dist/index.html

# Build and tag the Docker image locally without pushing.
image:
	docker build \
		--build-arg VERSION=$(VERSION) \
		-t $(REGISTRY)/$(IMAGE):$(VERSION) \
		-t $(REGISTRY)/$(IMAGE):latest \
		.
	@echo "Built $(REGISTRY)/$(IMAGE):$(VERSION)"

# Build, tag, and push to the registry.
# Requires: docker login $(REGISTRY)
deploy: image
	docker push $(REGISTRY)/$(IMAGE):$(VERSION)
	docker push $(REGISTRY)/$(IMAGE):latest
	@echo "Pushed $(REGISTRY)/$(IMAGE):$(VERSION) and :latest"
