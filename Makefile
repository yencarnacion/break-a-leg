.PHONY: run test build tidy

run:
	go run .

test:
	go test ./...

build:
	go build ./...

tidy:
	go mod tidy
