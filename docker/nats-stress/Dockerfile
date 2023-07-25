FROM golang:1.19-alpine as build

WORKDIR /go/src/github.com/holoplot/sw__supernova/go

COPY ["go/go.mod", "go/go.sum", "./"]

RUN go mod download

COPY go .

RUN go build -o /go/bin/nats-stream-configure ./cmd/nats-stream-configure

RUN go build -o /go/bin/nats-stream-publisher ./cmd/nats-stream-publisher

RUN go build -o /go/bin/nats-stream-consumer ./cmd/nats-stream-consumer

FROM alpine:3.18

COPY --from=build /go/bin /opt/bin