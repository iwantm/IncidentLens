FROM golang:1.27-alpine3.24 AS builder
WORKDIR /app
COPY go.mod ./
COPY /cmd/demo-app/main.go ./
RUN go build -o demo-app main.go

FROM alpine:3.24
WORKDIR /app
COPY --from=builder /app/demo-app .
EXPOSE 8080
CMD ["./demo-app"]