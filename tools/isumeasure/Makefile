.PHONY: build linux darwin vet
build: vet linux darwin
vet:
	go vet ./...
linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/isumeasure-linux-amd64 .
darwin:
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dist/isumeasure-darwin-arm64 .
