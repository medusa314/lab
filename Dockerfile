# syntax=docker/dockerfile:1

FROM golang:1.19.0-alpine as builder

WORKDIR /app/cfspeedtest

COPY go.mod go.sum .
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o cfspeedtest main.go

FROM alpine:latest
COPY --from=builder /app/cfspeedtest/cfspeedtest ./

# Run
CMD ["./cfspeedtest"]
