FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/jcm-geoip .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
    && addgroup -S jcm \
    && adduser -S -G jcm -h /nonexistent -s /sbin/nologin jcm

COPY --from=build /out/jcm-geoip /usr/local/bin/jcm-geoip
USER jcm
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/jcm-geoip"]
