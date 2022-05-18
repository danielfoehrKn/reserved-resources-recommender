FROM golang:1.17.9 AS builder

WORKDIR /
COPY . .

RUN make build

FROM alpine:3.13.2 AS runner

COPY --from=builder /reserved_linux_amd64 /reserved_linux_amd64

# use GNU utils via shell exec for disk reservations
RUN apk --no-cache add \
    coreutils

WORKDIR /

ENTRYPOINT ["/reserved_linux_amd64"]