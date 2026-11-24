# hxoad: the local server. Commands that agents run execute inside this
# container, so it ships a general toolchain; extend it for your stack, or run
# hxoad natively (scripts/run-native.sh) to use the host's own tools.
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hxoad ./cmd/hxoad \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hxoa ./cmd/hxoa

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      bash ca-certificates curl git ripgrep less procps \
      build-essential python3 python3-pip nodejs npm openssh-client \
 && rm -rf /var/lib/apt/lists/* \
 && git config --system --add safe.directory '*'
COPY --from=build /out/hxoad /out/hxoa /usr/local/bin/
ENV HXOA_DATA=/data HXOA_AGENT_ADDR=0.0.0.0:7070 HXOA_ADMIN_ADDR=0.0.0.0:7071
EXPOSE 7070 7071
ENTRYPOINT ["hxoad"]
