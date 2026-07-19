# Stage 1: frontend build
FROM oven/bun:1 AS webbuild
WORKDIR /build/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# Stage 2: server build, embedding the frontend
FROM golang:1.25 AS gobuild
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=webbuild /build/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

# Stage 3: runtime. distroless static ships CA certs and tzdata, which the
# Google clients and Europe/London date handling need.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=gobuild /server /server
# Config files are mounted read-only into /app (the working directory, where
# the server looks first) by compose — see deploy/compose.yaml.
ENTRYPOINT ["/server", "-env", "prod"]
