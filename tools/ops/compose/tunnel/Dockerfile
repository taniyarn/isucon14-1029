FROM alpine:3.20
RUN apk add --no-cache openssh-client autossh
COPY tunnel.sh /tunnel.sh
RUN chmod +x /tunnel.sh
ENTRYPOINT ["/tunnel.sh"]
