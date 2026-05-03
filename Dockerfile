FROM golang:1.26.2-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -o traces-server .

FROM alpine:latest
RUN apk add --no-cache sqlite-libs ca-certificates

WORKDIR /app

ENV DOCKER=true

COPY --from=builder /app/traces-server .
COPY --from=builder /app/static ./static

RUN mkdir -p /db && chmod 777 /db
RUN mkdir -p /app/media && chmod 777 /app/media

EXPOSE 6270

CMD ["./traces-server"]