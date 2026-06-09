# ---- build ----
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure-Go (modernc sqlite), so a static CGO-free binary works on scratch/distroless.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/recipearr .

# ---- runtime ----
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/recipearr /recipearr
ENV RECIPEARR_DATA_DIR=/data \
    RECIPEARR_ADDR=0.0.0.0:8585
VOLUME ["/data"]
EXPOSE 8585
ENTRYPOINT ["/recipearr"]
