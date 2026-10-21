FROM golang:1.27 AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod main.go ./
RUN go build -o /out/argo-cd-token-proxy .

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /out/argo-cd-token-proxy .
USER 65532:65532
ENTRYPOINT ["/argo-cd-token-proxy"]
