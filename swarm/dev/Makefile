.PHONY: build cluster test

default: build

build:
	go build -o bin/swarm    github.com/sekechain/go-sekechain/cmd/swarm
	go build -o bin/gseke     github.com/sekechain/go-sekechain/cmd/gseke
	go build -o bin/bootnode github.com/sekechain/go-sekechain/cmd/bootnode

cluster: build
	scripts/boot-cluster.sh

test:
	go test -v github.com/sekechain/go-sekechain/swarm/...
