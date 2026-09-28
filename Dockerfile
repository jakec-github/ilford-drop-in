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

# Stage 3: the CP-SAT solver's venv. The server drafts rotas by running
# `python -m pyallocator` (pkg/core/allocator/cpsat_runner.go), so the runtime
# needs Python with ortools installed.
#
# A venv only works under the interpreter that made it: its bin/python is a
# symlink to that interpreter's absolute path, and ortools, numpy and protobuf
# ship compiled extensions for one Python minor version. So the venv is built
# with Debian 12's own python3.11, the same one at the same path as the runtime
# stage's distroless base. Changing either base means changing both.
#
# Non-editable: an editable install points back at /build, which is not in the
# runtime image.
FROM debian:12 AS pybuild
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 python3-venv \
    && rm -rf /var/lib/apt/lists/*
RUN /usr/bin/python3.11 -m venv /opt/pyallocator
COPY pyallocator/ /build/pyallocator/
RUN /opt/pyallocator/bin/pip install --no-cache-dir /build/pyallocator

# Stage 4: runtime. distroless python3 is distroless static (CA certs and tzdata,
# which the Google clients and Europe/London date handling need) plus Debian's
# Python 3.11, and still no shell or package manager.
FROM gcr.io/distroless/python3-debian12:nonroot
WORKDIR /app
COPY --from=gobuild /server /server
COPY --from=pybuild /opt/pyallocator /opt/pyallocator
# How the server finds the solver (allocator.ResolvePythonInterpreter).
# scripts/image-smoke.sh runs it from this variable, so CI fails if it breaks.
ENV ILFORD_CPSAT_PYTHON=/opt/pyallocator/bin/python
# Config files are mounted read-only into /app (the working directory, where
# the server looks first) by compose — see deploy/compose.yaml.
ENTRYPOINT ["/server", "-env", "prod"]
