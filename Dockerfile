FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata ffmpeg python3 py3-pip deno \
 && pip3 install --no-cache-dir -U "yt-dlp[default]" --break-system-packages \
 && ln -sf "$(command -v yt-dlp)" /usr/local/bin/yt-dlp

WORKDIR /app
COPY --chmod=755 bot /app/bot
COPY words_downloaded.txt /app/

CMD ["/app/bot"]
