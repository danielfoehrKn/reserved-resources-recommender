FROM golang:1.16.4 AS builder

WORKDIR /
COPY . .

RUN make build

FROM alpine:3.9 AS runner

COPY --from=builder /reserved_linux_amd64 /reserved_linux_amd64

WORKDIR /

ENTRYPOINT ["/reserved_linux_amd64"]