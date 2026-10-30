.PHONY: test vet run

test:
	go test ./...

vet:
	go vet ./...

run:
	go run ./cmd/api
