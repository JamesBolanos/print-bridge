.PHONY: fmt test vet race lint check

fmt:
	gofmt -w .

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

lint:
	golangci-lint run

check: fmt test vet race
