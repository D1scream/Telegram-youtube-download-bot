FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bot cmd/main.go

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata ffmpeg python3 py3-pip \
 && pip3 install --no-cache-dir -U yt-dlp --break-system-packages \
 && ln -sf "$(command -v yt-dlp)" /usr/local/bin/yt-dlp

WORKDIR /app
COPY --from=build /bot /app/bot

EXPOSE 9000
CMD ["/app/bot"]
