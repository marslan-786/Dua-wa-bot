FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev git sqlite-dev

WORKDIR /app
COPY . .

# فائلیں صاف کریں اور لیٹسٹ لائبریریز (بشمول مونگو ڈی بی) ڈاؤن لوڈ کریں
RUN rm -f go.mod go.sum || true
RUN go mod init otp-bot
RUN go get go.mau.fi/whatsmeow@latest
RUN go get go.mongodb.org/mongo-driver/mongo@latest
RUN go get github.com/mattn/go-sqlite3@latest
RUN go mod tidy

# بلڈ
RUN go build -o bot .

FROM alpine:latest
RUN apk add --no-cache ca-certificates sqlite-libs
WORKDIR /app
COPY --from=builder /app/bot .

CMD ["./bot"]