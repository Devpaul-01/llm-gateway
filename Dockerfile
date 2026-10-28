# syntax=docker/dockerfile:1

# --- Build stage ---------------------------------------------------------
# Full Go toolchain, used only to produce a static binary. Nothing from
# this stage ships in the final image.
FROM golang:1.25-bookworm AS build

WORKDIR /src

# Cache dependency downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a statically linked binary with no dependency on
# the target image's libc, which is what makes running it on a
# scratch/distroless base (no shell, no package manager, no OS utilities)
# possible at all. Architecture Section 16.1.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/genkey ./cmd/genkey

# --- Final stage -----------------------------------------------------------
# distroless/static has no shell, no package manager, and no OS utilities
# -- only what's needed to run a statically linked binary plus CA
# certificates (required for the outbound HTTPS calls this service makes
# to Groq/Mistral/OpenRouter/Gemini). Final image size is on the order of
# tens of megabytes, per Architecture Section 16.1.
FROM gcr.io/distroless/static-debian12:nonroot AS final

WORKDIR /app
COPY --from=build /out/gateway /app/gateway
COPY --from=build /out/genkey /app/genkey

# The :nonroot tag already runs as UID 65532; this is kept explicit for
# clarity (and to fail loudly if a future base-image swap ever drops the
# implicit nonroot default) rather than relying on it silently.
USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/app/gateway"]
