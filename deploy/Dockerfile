FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gallery ./cmd/gallery

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 gallery \
 && mkdir /data && chown gallery /data
COPY --from=build /out/gallery /usr/local/bin/gallery
USER gallery
ENV DATA_DIR=/data ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["gallery"]
