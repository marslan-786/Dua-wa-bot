# بلڈ سٹیج
FROM golang:1.21-alpine AS builder

# سسٹم ٹولز (git لازمی ہے)
RUN apk add --no-cache gcc musl-dev git sqlite-dev

WORKDIR /app

# فائلیں کاپی کریں
COPY . .

# لائبریریز کا درست ورژن ڈاؤن لوڈ کرنا
RUN go mod init otp-bot || true
RUN go get go.mau.fi/whatsmeow@latest
RUN go get github.com/lib/pq@latest
RUN go get github.com/mattn/go-sqlite3@latest
RUN go mod tidy

# بلڈ کرنا
RUN go build -o bot .

# رن سٹیج
FROM alpine:latest
RUN apk add --no-cache ca-certificates sqlite-libs
WORKDIR /app
COPY --from=builder /app/bot .
CMD ["./bot"]
