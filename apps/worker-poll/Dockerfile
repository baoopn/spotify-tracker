# Multi-stage Dockerfile for apps/worker-poll
# Builder: compile a Linux binary with Deno
FROM denoland/deno:latest AS builder
WORKDIR /src

# Copy only what we need to keep the build context small
COPY deno.json deno.lock* ./
COPY apps ./apps
COPY packages/shared ./packages/shared

# Compile the worker to a standalone Linux binary.
# Adjust permissions as required by the worker at runtime.
RUN deno compile --target x86_64-unknown-linux-gnu \
	--allow-env --allow-net --allow-read \
	--output /worker /src/apps/worker-poll/src/index.ts

# Runtime: minimal base image using distroless (glibc support)
FROM gcr.io/distroless/cc-debian11:nonroot
LABEL org.opencontainers.image.source="git@github.com:baoopn/spotify-status.git"
COPY --from=builder /worker /worker

# Add a lightweight healthcheck that executes the compiled binary with `--health`.
# Use JSON form so no shell is required (distroless runtime).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/worker","--health"]

# Run as non-root user provided by the distroless image
USER nonroot
ENTRYPOINT ["/worker"]
