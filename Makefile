VPS_IP ?= your-vps-ip
VPS_USER ?= root

.PHONY: build build-linux deploy clean test lint

build:
	go build -o bin/wally-tunnel ./cmd/wally-tunnel
	go build -o bin/wally-tunnel-server ./cmd/wally-tunnel-server

build-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/wally-tunnel-server-linux ./cmd/wally-tunnel-server

deploy: build-linux
	scp bin/wally-tunnel-server-linux $(VPS_USER)@$(VPS_IP):/usr/local/bin/wally-tunnel-server
	ssh $(VPS_USER)@$(VPS_IP) 'chmod +x /usr/local/bin/wally-tunnel-server && systemctl restart wally-tunnel-server'

test:
	go test -race -count=1 -cover ./...

lint:
	go vet ./...
	golangci-lint run

clean:
	rm -rf bin/
