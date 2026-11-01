# Pure-Go SQLite driver: no cgo, a static binary on a distroless base.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/socialflow-lite ./cmd/socialflow-lite

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/socialflow-lite /socialflow-lite
ENV DATABASE_PATH=/data/socialflow-lite.db PORT=8080
VOLUME /data
EXPOSE 8080
# Health: GET /healthz (distroless has no curl; probe it from your orchestrator).
ENTRYPOINT ["/socialflow-lite"]
CMD ["serve"]
