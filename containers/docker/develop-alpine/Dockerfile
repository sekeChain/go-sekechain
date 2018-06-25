FROM alpine:3.7

RUN \
  apk add --update go git make gcc musl-dev linux-headers ca-certificates && \
  git clone --depth 1 https://github.com/sekechain/go-sekechain && \
  (cd go-sekeereum && make gseke) && \
  cp go-sekeereum/build/bin/gseke /gseke && \
  apk del go git make gcc musl-dev linux-headers && \
  rm -rf /go-sekeereum && rm -rf /var/cache/apk/*

EXPOSE 8545
EXPOSE 30303

ENTRYPOINT ["/gseke"]
