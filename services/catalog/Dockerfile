FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.work go.work.sum ./

COPY services/catalog/go.mod services/catalog/go.sum ./services/catalog/
COPY go.mod go.sum ./              

RUN go work sync && go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /catalog-service ./services/catalog/cmd/main.go


FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

COPY --from=builder /auth-service .

EXPOSE 50052

CMD ["./catalog-service"]