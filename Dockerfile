FROM alpine:3.21
RUN addgroup -S gohome && adduser -S gohome -G gohome
COPY gohome /usr/local/bin/gohome
USER gohome
EXPOSE 3000
ENTRYPOINT ["gohome"]
