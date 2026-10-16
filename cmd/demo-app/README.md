# IncidentLens demo app

A small HTTP service written in Go. It listens on port `8080` and provides health, version, and worker endpoints for trying out IncidentLens.

## Requirements

- Go 1.27.2 or later, matching the version declared in `go.mod`; or
- Docker, to run the containerized version.

## Run with Go

From this directory:

```sh
go run .
```

The server listens on `http://localhost:8080`. In another terminal, try:

```sh
curl -i http://localhost:8080/health
curl -i http://localhost:8080/version
curl -i http://localhost:8080/work
```

Each endpoint accepts `GET` requests and returns a plain-text response. `/health` responds with `Health check passed`; `/version` responds with `Version 1.0.0`; and `/work` normally responds with `Worker is running`.

## Simulate a worker failure

Set `FAIL_MODE=true` when starting the service to make `/work` return HTTP 500 with `Worker failed`:

```sh
FAIL_MODE=true go run .
```

The failure setting applies to the lifetime of that process. Restart without `FAIL_MODE=true` to restore normal worker responses.

## Build

From this directory:

```sh
go build -o incident-lens-demo .
./incident-lens-demo
```
