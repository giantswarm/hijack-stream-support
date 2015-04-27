PROJECT=hijack-stream-support

BUILD_PATH := $(shell pwd)/.gobuild

PROJECT_PATH := "$(BUILD_PATH)/src/github.com/giantswarm"

BIN=$(PROJECT)

.PHONY=clean get-deps 

GOPATH := $(BUILD_PATH)

SOURCE=$(shell find . -name '*.go')

all: get-deps $(BIN)

ci: clean all run-tests

clean:
	rm -rf $(BUILD_PATH) $(BIN)

get-deps: .gobuild

.gobuild:
	mkdir -p $(PROJECT_PATH)
	cd "$(PROJECT_PATH)" && ln -s ../../../.. $(PROJECT)

$(BIN): $(SOURCE)
	GOPATH=$(GOPATH) go build -o $(BIN)

