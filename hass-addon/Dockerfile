# HA local add-on image — ships the prebuilt arm64 binary, so the Supervisor build on the Yellow is
# just "pull distroless + copy the binary" (a few seconds, no Go toolchain). Rebuild the binary with:
#   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o hass-addon/recipearr-arm64 .
FROM gcr.io/distroless/static-debian12
COPY recipearr-arm64 /recipearr
# RECIPEARR_DATA_DIR=/data → the add-on's Supervisor-persisted volume (survives restarts/updates).
ENV RECIPEARR_DATA_DIR=/data \
    RECIPEARR_ADDR=0.0.0.0:8585
EXPOSE 8585
ENTRYPOINT ["/recipearr"]
