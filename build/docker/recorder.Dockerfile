FROM golang:1.27-alpine3.24 AS builder
WORKDIR /app

RUN apk add --no-cache build-base

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o recorder ./cmd/recorder/main.go

FROM alpine:3.24
WORKDIR /app
COPY --from=builder /app/recorder .
EXPOSE 8080
CMD ["./recorder"]