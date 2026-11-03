FROM golang:1.22 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pitwall ./cmd/pitwall

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pitwall /pitwall
EXPOSE 8080
ENTRYPOINT ["/pitwall", "serve", "-addr", ":8080"]
