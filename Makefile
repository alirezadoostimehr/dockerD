.PHONY: test backend app runtime clean

test:
	cd backend && go test -race ./... && go vet ./...
	swift test

backend:
	cd backend && go build -trimpath -o ../build/dockerd-service ./cmd/dockerd-service

runtime:
	python3 scripts/fetch-runtime.py

app: runtime backend
	swift build -c release
	python3 scripts/package-app.py

clean:
	rm -rf build dist .build
