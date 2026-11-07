FROM golang:1.27-alpine AS build

WORKDIR /src

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/trip-service ./cmd/trip-service

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/trip-service /trip-service

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/trip-service"]