.PHONY: run build tidy clean

-include .env
export

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

tidy:
	go mod tidy

clean:
	rm -f bin/server
