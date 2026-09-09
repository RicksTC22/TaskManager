# syntax=docker/dockerfile:1

# ---- build ----------------------------------------------------------------
FROM golang:1.27 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# modernc.org/sqlite is pure Go, so we can build a fully static binary and run
# it on a distroless/scratch base. Templates and static assets are embedded.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cairn .

# ---- run ---------------------------------------------------------------
# Plain distroless/static (runs as root) so a mounted Render disk at /data is
# always writable; swap to :nonroot once the app is on Postgres and /data is gone.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/cairn /cairn

# Render sets $PORT; the app reads it. Data lives on a mounted disk (or, after
# the Postgres migration, in DATABASE_URL and this dir is unused).
ENV CAIRN_DB=/data/cairn.db \
    CAIRN_SECURE_COOKIES=1 \
    CAIRN_DEMO=false

EXPOSE 8080
ENTRYPOINT ["/cairn"]
