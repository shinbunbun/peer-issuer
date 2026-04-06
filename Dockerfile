FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /peer-issuer ./cmd/peer-issuer

FROM scratch
COPY --from=builder /peer-issuer /peer-issuer
ENTRYPOINT ["/peer-issuer"]
