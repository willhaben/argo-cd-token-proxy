FROM golang:1.27 AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod main.go ./
RUN go build -trimpath -ldflags="-s -w" -o /out/argo-cd-token-proxy .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/argo-cd-token-proxy /argo-cd-token-proxy
USER 65532:65532
ENTRYPOINT ["/argo-cd-token-proxy"]
