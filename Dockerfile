FROM golang:alpine
WORKDIR /go/src/github.com/mhmdiaa/second-order
COPY . .
RUN apk --no-cache add git \
    && go get -u github.com/mhmdiaa/second-order

ENTRYPOINT ["go", "run", "second-order.go"]
