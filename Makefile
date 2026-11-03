.PHONY: test lint demo run

test:
	go test -race -cover ./...

lint:
	test -z "$$(gofmt -l .)" && go vet ./...

demo:
	go run ./cmd/pitwall gen
	go run ./cmd/pitwall fit -out tyres.json
	go run ./cmd/pitwall plan -model tyres.json

run:
	go run ./cmd/pitwall serve
