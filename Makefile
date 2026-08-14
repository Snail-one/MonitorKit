.PHONY: build test vet check

BINARY := bin/snailmon

build:
	OUTPUT=$(BINARY) ./build.sh

test:
	go test ./...

vet:
	go vet ./...

check: test vet
	bash -n build.sh scripts/*.sh
	bash -n scripts/probes/node_exporter/install.sh
	bash -n scripts/probes/alloy/install.sh
	bash scripts/test_installer.sh
