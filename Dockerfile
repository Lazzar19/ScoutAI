# syntax=docker/dockerfile:1

FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scout .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget \
    && adduser -D -H -u 65532 app
COPY --from=build /out/scout /usr/local/bin/scout
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/scout"]
