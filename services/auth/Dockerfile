FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.work go.work.sum ./

COPY services/auth/go.mod services/auth/go.sum ./services/auth/
COPY go.mod go.sum ./ 

COPY . .

RUN go work sync && go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /auth-service ./services/auth/cmd/main.go


FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

COPY --from=builder /auth-service .

EXPOSE 50051

CMD ["./auth-service"]