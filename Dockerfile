# syntax=docker/dockerfile:1

FROM golang:1.22.11-alpine as builder

WORKDIR /app/cfspeedtest

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o cfspeedtest main.go

FROM alpine:latest
COPY --from=builder /app/cfspeedtest/cfspeedtest ./

# Run
CMD ["./cfspeedtest"]
