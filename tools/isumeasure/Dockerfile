# build context: tools/isumeasure (used by tools/ops/compose/docker-compose.yml)
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /isumeasure .

# the panel renders pprof (go tool pprof -top / -svg) when generating reports: keep the Go toolchain + graphviz;
# git: the report links SQL to the contest repo (remote URL + path of the code dir, see codelink.go)
FROM golang:1.23-alpine
RUN apk add --no-cache ca-certificates tzdata graphviz git
COPY --from=build /isumeasure /usr/local/bin/isumeasure
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/isumeasure", "panel"]
