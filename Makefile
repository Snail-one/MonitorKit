.PHONY: build test vet check

BINARY := bin/snailmon

build:
	mkdir -p $(dir $(BINARY))
	go build -trimpath -o $(BINARY) ./cmd/snailmon

test:
	go test ./...

vet:
	go vet ./...

check: test vet
	bash -n scripts/probes/node_exporter/install.sh
	bash -n scripts/probes/alloy/install.sh
