BINARY    := mw-ecs-instrument
MODULE    := github.com/middleware-labs/mw-ecs-instrumentation
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/cmd.version=$(VERSION) \
	-X $(MODULE)/cmd.commit=$(COMMIT) \
	-X $(MODULE)/cmd.buildDate=$(BUILD_DATE)

DIST_DIR := dist
RPM_VERSION := $(subst -,_,$(VERSION))
DOCKER_DIR := aws-ecs-auto-instrumentation
DOCKER_REGISTRY ?= ghcr.io/middleware-labs

.PHONY: build build-all clean test lint \
	build-linux build-linux-deb build-linux-rpm \
	build-linux-deb-amd64 build-linux-deb-arm64 \
	build-linux-rpm-amd64 build-linux-rpm-arm64 \
	build-linux-amd64 build-linux-arm64 \
	build-darwin-amd64 build-darwin-arm64 \
	build-windows-amd64 \
	docker-build docker-build-java docker-build-node docker-build-python docker-build-all

# Default: build for current platform
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

# All platforms
build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-windows-amd64
	@echo "Done. Binaries in $(DIST_DIR)/"

# Linux packages (deb + rpm, both arches)
build-linux: build-linux-deb build-linux-rpm
	@echo "Done. Linux packages in $(DIST_DIR)/"

build-linux-deb: build-linux-deb-amd64 build-linux-deb-arm64

build-linux-rpm: build-linux-rpm-amd64 build-linux-rpm-arm64

build-linux-deb-amd64: build-linux-amd64
	@echo "Building linux-deb-amd64..."
	@mkdir -p $(DIST_DIR)/deb-amd64/DEBIAN $(DIST_DIR)/deb-amd64/usr/local/bin
	@printf 'Package: $(BINARY)\nVersion: $(VERSION)\nArchitecture: amd64\nMaintainer: Middleware <dev@middleware.io>\nDescription: Middleware ECS auto-instrumentation CLI\n' \
		> $(DIST_DIR)/deb-amd64/DEBIAN/control
	@cp $(DIST_DIR)/$(BINARY)-linux-amd64 $(DIST_DIR)/deb-amd64/usr/local/bin/$(BINARY)
	@dpkg-deb --build $(DIST_DIR)/deb-amd64 $(DIST_DIR)/$(BINARY)-linux_$(VERSION)_amd64.deb
	@rm -rf $(DIST_DIR)/deb-amd64

build-linux-deb-arm64: build-linux-arm64
	@echo "Building linux-deb-arm64..."
	@mkdir -p $(DIST_DIR)/deb-arm64/DEBIAN $(DIST_DIR)/deb-arm64/usr/local/bin
	@printf 'Package: $(BINARY)\nVersion: $(VERSION)\nArchitecture: arm64\nMaintainer: Middleware <dev@middleware.io>\nDescription: Middleware ECS auto-instrumentation CLI\n' \
		> $(DIST_DIR)/deb-arm64/DEBIAN/control
	@cp $(DIST_DIR)/$(BINARY)-linux-arm64 $(DIST_DIR)/deb-arm64/usr/local/bin/$(BINARY)
	@dpkg-deb --build $(DIST_DIR)/deb-arm64 $(DIST_DIR)/$(BINARY)-linux_$(VERSION)_arm64.deb
	@rm -rf $(DIST_DIR)/deb-arm64

build-linux-rpm-amd64: build-linux-amd64
	@echo "Building linux-rpm-amd64..."
	@mkdir -p $(DIST_DIR)/rpm-amd64/BUILD $(DIST_DIR)/rpm-amd64/RPMS $(DIST_DIR)/rpm-amd64/SOURCES $(DIST_DIR)/rpm-amd64/SPECS $(DIST_DIR)/rpm-amd64/SRPMS
	@printf 'Name: $(BINARY)\nVersion: $(RPM_VERSION)\nRelease: 1\nSummary: Middleware ECS auto-instrumentation CLI\nLicense: Proprietary\n\n%%description\nMiddleware ECS auto-instrumentation CLI\n\n%%install\nmkdir -p %%{buildroot}/usr/local/bin\ncp %s %%{buildroot}/usr/local/bin/$(BINARY)\n\n%%files\n/usr/local/bin/$(BINARY)\n' \
		"$(CURDIR)/$(DIST_DIR)/$(BINARY)-linux-amd64" \
		> $(DIST_DIR)/rpm-amd64/SPECS/$(BINARY).spec
	@rpmbuild --define "_topdir $(CURDIR)/$(DIST_DIR)/rpm-amd64" --target x86_64 -bb $(DIST_DIR)/rpm-amd64/SPECS/$(BINARY).spec > /dev/null 2>&1
	@cp $(DIST_DIR)/rpm-amd64/RPMS/x86_64/*.rpm $(DIST_DIR)/$(BINARY)-linux-$(RPM_VERSION).x86_64.rpm
	@rm -rf $(DIST_DIR)/rpm-amd64

build-linux-rpm-arm64: build-linux-arm64
	@echo "Building linux-rpm-arm64..."
	@mkdir -p $(DIST_DIR)/rpm-arm64/BUILD $(DIST_DIR)/rpm-arm64/RPMS $(DIST_DIR)/rpm-arm64/SOURCES $(DIST_DIR)/rpm-arm64/SPECS $(DIST_DIR)/rpm-arm64/SRPMS
	@printf 'Name: $(BINARY)\nVersion: $(RPM_VERSION)\nRelease: 1\nSummary: Middleware ECS auto-instrumentation CLI\nLicense: Proprietary\n\n%%description\nMiddleware ECS auto-instrumentation CLI\n\n%%install\nmkdir -p %%{buildroot}/usr/local/bin\ncp %s %%{buildroot}/usr/local/bin/$(BINARY)\n\n%%files\n/usr/local/bin/$(BINARY)\n' \
		"$(CURDIR)/$(DIST_DIR)/$(BINARY)-linux-arm64" \
		> $(DIST_DIR)/rpm-arm64/SPECS/$(BINARY).spec
	@rpmbuild --define "_topdir $(CURDIR)/$(DIST_DIR)/rpm-arm64" --target aarch64 -bb $(DIST_DIR)/rpm-arm64/SPECS/$(BINARY).spec > /dev/null 2>&1
	@cp $(DIST_DIR)/rpm-arm64/RPMS/aarch64/*.rpm $(DIST_DIR)/$(BINARY)-linux-$(RPM_VERSION).aarch64.rpm
	@rm -rf $(DIST_DIR)/rpm-arm64

build-linux-amd64:
	@echo "Building linux-amd64..."
	@mkdir -p $(DIST_DIR)
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-linux-amd64 .

build-linux-arm64:
	@echo "Building linux-arm64..."
	@mkdir -p $(DIST_DIR)
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-linux-arm64 .

build-darwin-amd64:
	@echo "Building darwin-amd64..."
	@mkdir -p $(DIST_DIR)
	@CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-darwin-amd64 .

build-darwin-arm64:
	@echo "Building darwin-arm64..."
	@mkdir -p $(DIST_DIR)
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-darwin-arm64 .

build-windows-amd64:
	@echo "Building windows-amd64..."
	@mkdir -p $(DIST_DIR)
	@CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-windows-amd64.exe .

docker-build: docker-build-java docker-build-node docker-build-python docker-build-all

docker-build-java:
	@echo "Building docker java..."
	@docker build -f $(DOCKER_DIR)/java/Dockerfile.opentelemetry.io -t $(DOCKER_REGISTRY)/mw-ecs-autoinstrumentation-java:$(VERSION) $(DOCKER_DIR)/java

docker-build-node:
	@echo "Building docker node..."
	@docker build -f $(DOCKER_DIR)/node/Dockerfile.opentelemetry.io -t $(DOCKER_REGISTRY)/mw-ecs-autoinstrumentation-node:$(VERSION) $(DOCKER_DIR)/node

docker-build-python:
	@echo "Building docker python..."
	@docker build -f $(DOCKER_DIR)/python/Dockerfile.opentelemetry.io -t $(DOCKER_REGISTRY)/mw-ecs-autoinstrumentation-python:$(VERSION) $(DOCKER_DIR)/python

docker-build-all:
	@echo "Building docker all..."
	@docker build -f $(DOCKER_DIR)/all/Dockerfile.opentelemetry.io -t $(DOCKER_REGISTRY)/mw-ecs-autoinstrumentation-all:$(VERSION) $(DOCKER_DIR)/all

clean:
	rm -rf $(DIST_DIR)
	rm -f $(BINARY)

test:
	go test ./...

lint:
	golangci-lint run ./...
