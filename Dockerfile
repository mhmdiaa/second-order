FROM golang:1.17.3-alpine AS build-env
RUN apk add --no-cache git
RUN go install -v github.com/mhmdiaa/second-order@latest

FROM alpine:3.15.0
RUN apk -U upgrade --no-cache \
    && apk add --no-cache bind-tools ca-certificates
COPY --from=build-env /go/bin/second-order /usr/local/bin/

ENTRYPOINT ["second-order"]