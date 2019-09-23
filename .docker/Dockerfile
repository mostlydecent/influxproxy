FROM golang:1.13-alpine as STAGING
RUN apk add -U --no-cache git

ADD . /go/src/
WORKDIR /go/src

ENV GO111MODULE=on
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o /go/bin/influxproxy ./cmd
RUN CGO_ENABLED=0 go test -installsuffix cgo ./...

FROM alpine:3.10
RUN apk add --no-cache tini

COPY --from=STAGING /go/bin/influxproxy /opt/
WORKDIR /opt

ENTRYPOINT ["/sbin/tini", "--", "/opt/influxproxy"]
CMD ["-source=localhost:3030", "-destination=localhost:8086", "-config=/etc/influxproxy/configuration.yaml"]

VOLUME ["/etc/influxproxy"]
