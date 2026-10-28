.PHONY: gen lint core-build core-test adapter-build

# Regenerate stubs (Go -> core/gen, TS -> adapter/src/gen). Needs buf.
gen:
	buf generate

lint:
	buf lint

core-build:
	cd core && go build ./...

core-test:
	cd core && go test ./...

# Build the agentd gRPC core binary.
agentd:
	cd core && go build -o bin/agentd ./cmd/agentd

# TS stubs are regenerated (not committed), so generate before building.
adapter-build: gen
	cd adapter && npm install && npm run build
