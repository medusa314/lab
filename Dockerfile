# syntax=docker/dockerfile:1

FROM golang:1.19

WORKDIR /app

COPY go.mod go.sum main.go ./
RUN go mod download
COPY speedtest/ GOPATH
COPY stats/ GOPATH
COPY timeCalculations/ GOPATH
RUN CGO_ENABLED=0 GOOS=linux go build -o /cfspeedtest main.go

# Run
CMD ["/cfspeedtest"]
