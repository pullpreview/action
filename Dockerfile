FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/pullpreview ./cmd/pullpreview

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends openssh-client git ca-certificates \
  && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/pullpreview /usr/local/bin/pullpreview

ENTRYPOINT ["/usr/local/bin/pullpreview"]
