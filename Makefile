# The `Build and tests` gate runs `make ci` when this target exists. Keep the name.
#
# Nothing here is version-pinned beyond the toolchain because nothing here is a third-party
# linter: `go build`, `go vet` and `go test` ship with the Go release and agree with CI as far as
# the Go version does. If a linter is ever added, pin it — a linter whose verdict depends on its
# version goes green locally and red on CI against one identical tree.

.PHONY: ci build vet test clean

ci: build vet test

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin/
	go clean -testcache
