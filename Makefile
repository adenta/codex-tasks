VERSION ?= development
.PHONY: build test vet dist
build:
	go build -trimpath -ldflags '-X github.com/adenta/codex-tasks/internal/buildinfo.BuildID=$(VERSION)' -o build/codex-tasks ./cmd/codex-tasks
test:
	go test ./...
vet:
	go vet ./...
dist: build
	mkdir -p dist
	tar -czf dist/codex-tasks-linux-$$(go env GOARCH).tar.gz -C build codex-tasks -C .. README.md LICENSE docs skills
	cd dist && sha256sum codex-tasks-linux-$$(go env GOARCH).tar.gz > codex-tasks-linux-$$(go env GOARCH).tar.gz.sha256
