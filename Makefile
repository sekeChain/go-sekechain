# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: gseke android ios gseke-cross swarm evm all test clean
.PHONY: gseke-linux gseke-linux-386 gseke-linux-amd64 gseke-linux-mips64 gseke-linux-mips64le
.PHONY: gseke-linux-arm gseke-linux-arm-5 gseke-linux-arm-6 gseke-linux-arm-7 gseke-linux-arm64
.PHONY: gseke-darwin gseke-darwin-386 gseke-darwin-amd64
.PHONY: gseke-windows gseke-windows-386 gseke-windows-amd64

GOBIN = $(shell pwd)/build/bin
GO ?= latest

gseke:
	build/env.sh go run build/ci.go install ./cmd/gseke
	@echo "Done building."
	@echo "Run \"$(GOBIN)/gseke\" to launch gseke."

swarm:
	build/env.sh go run build/ci.go install ./cmd/swarm
	@echo "Done building."
	@echo "Run \"$(GOBIN)/swarm\" to launch swarm."

all:
	build/env.sh go run build/ci.go install

android:
	build/env.sh go run build/ci.go aar --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/gseke.aar\" to use the library."

ios:
	build/env.sh go run build/ci.go xcode --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/Gseke.framework\" to use the library."

test: all
	build/env.sh go run build/ci.go test

clean:
	rm -fr build/_workspace/pkg/ $(GOBIN)/*

# The devtools target installs tools required for 'go generate'.
# You need to put $GOBIN (or $GOPATH/bin) in your PATH to use 'go generate'.

devtools:
	env GOBIN= go get -u golang.org/x/tools/cmd/stringer
	env GOBIN= go get -u github.com/kevinburke/go-bindata/go-bindata
	env GOBIN= go get -u github.com/fjl/gencodec
	env GOBIN= go get -u github.com/golang/protobuf/protoc-gen-go
	env GOBIN= go install ./cmd/abigen
	@type "npm" 2> /dev/null || echo 'Please install node.js and npm'
	@type "solc" 2> /dev/null || echo 'Please install solc'
	@type "protoc" 2> /dev/null || echo 'Please install protoc'

# Cross Compilation Targets (xgo)

gseke-cross: gseke-linux gseke-darwin gseke-windows gseke-android gseke-ios
	@echo "Full cross compilation done:"
	@ls -ld $(GOBIN)/gseke-*

gseke-linux: gseke-linux-386 gseke-linux-amd64 gseke-linux-arm gseke-linux-mips64 gseke-linux-mips64le
	@echo "Linux cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-*

gseke-linux-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/386 -v ./cmd/gseke
	@echo "Linux 386 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep 386

gseke-linux-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/amd64 -v ./cmd/gseke
	@echo "Linux amd64 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep amd64

gseke-linux-arm: gseke-linux-arm-5 gseke-linux-arm-6 gseke-linux-arm-7 gseke-linux-arm64
	@echo "Linux ARM cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep arm

gseke-linux-arm-5:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-5 -v ./cmd/gseke
	@echo "Linux ARMv5 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep arm-5

gseke-linux-arm-6:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-6 -v ./cmd/gseke
	@echo "Linux ARMv6 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep arm-6

gseke-linux-arm-7:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-7 -v ./cmd/gseke
	@echo "Linux ARMv7 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep arm-7

gseke-linux-arm64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm64 -v ./cmd/gseke
	@echo "Linux ARM64 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep arm64

gseke-linux-mips:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips --ldflags '-extldflags "-static"' -v ./cmd/gseke
	@echo "Linux MIPS cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep mips

gseke-linux-mipsle:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mipsle --ldflags '-extldflags "-static"' -v ./cmd/gseke
	@echo "Linux MIPSle cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep mipsle

gseke-linux-mips64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64 --ldflags '-extldflags "-static"' -v ./cmd/gseke
	@echo "Linux MIPS64 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep mips64

gseke-linux-mips64le:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64le --ldflags '-extldflags "-static"' -v ./cmd/gseke
	@echo "Linux MIPS64le cross compilation done:"
	@ls -ld $(GOBIN)/gseke-linux-* | grep mips64le

gseke-darwin: gseke-darwin-386 gseke-darwin-amd64
	@echo "Darwin cross compilation done:"
	@ls -ld $(GOBIN)/gseke-darwin-*

gseke-darwin-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/386 -v ./cmd/gseke
	@echo "Darwin 386 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-darwin-* | grep 386

gseke-darwin-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/amd64 -v ./cmd/gseke
	@echo "Darwin amd64 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-darwin-* | grep amd64

gseke-windows: gseke-windows-386 gseke-windows-amd64
	@echo "Windows cross compilation done:"
	@ls -ld $(GOBIN)/gseke-windows-*

gseke-windows-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/386 -v ./cmd/gseke
	@echo "Windows 386 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-windows-* | grep 386

gseke-windows-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/amd64 -v ./cmd/gseke
	@echo "Windows amd64 cross compilation done:"
	@ls -ld $(GOBIN)/gseke-windows-* | grep amd64
