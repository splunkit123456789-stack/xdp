FROM alpine:3.17
WORKDIR /app
RUN apk add --no-cache python3
COPY build/docker-bin/ /usr/local/bin/
COPY migrations ./migrations
COPY configs ./configs
EXPOSE 8080
CMD ["xdp-api"]
