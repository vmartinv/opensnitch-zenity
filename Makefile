.PHONY: build test vet proto install clean

PREFIX ?= /usr
GOBIN_PATH := $(shell go env GOPATH)/bin

build:
	go build -o opensnitch-zenity ./cmd/opensnitch-zenity

test:
	go test -race ./...

vet:
	gofmt -l . | (! grep .)
	go vet ./...

# Generated code is committed; only needed after updating proto/ui.proto.
proto:
	PATH="$$PATH:$(GOBIN_PATH)" protoc -Iproto \
		--go_out=proto --go_opt=paths=source_relative \
		--go-grpc_out=proto --go-grpc_opt=paths=source_relative \
		proto/ui.proto

install: build
	install -Dm755 opensnitch-zenity $(DESTDIR)$(PREFIX)/bin/opensnitch-zenity
	install -Dm644 contrib/systemd/opensnitch-zenity.service \
		$(DESTDIR)$(PREFIX)/lib/systemd/user/opensnitch-zenity.service

clean:
	rm -f opensnitch-zenity
