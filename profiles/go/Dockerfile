# koya go: Go 最新安定版
FROM docker.io/library/golang:latest AS golang

FROM localhost/koya/base
COPY --from=golang /usr/local/go /usr/local/go
RUN mkdir -p /go && chmod 0777 /go
ENV GOPATH=/go \
    PATH=/go/bin:/usr/local/go/bin:$PATH
