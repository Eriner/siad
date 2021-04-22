# build sia
FROM golang:1.16 AS build

WORKDIR /app

COPY . .

ENV GOBIN=/app/bin

# updates the cache to fix GIT_DIRTY detection in makefile
RUN git status > /dev/null
RUN make release

# run sia
FROM debian:stable-slim

# debian-slim does not come with any certs, copy to prevent SSL issues
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# copy built binaries
COPY --from=build /app/bin /usr/local/bin

EXPOSE 9981 9982 9983 9984

# SIA_WALLET_PASSWORD is used to automatically unlock the wallet
ENV SIA_WALLET_PASSWORD=
# SIA_API_PASSWORD sets the password used for API authentication
ENV SIA_API_PASSWORD=

VOLUME [ "/sia-data" ]

ENTRYPOINT [ "siad", "--disable-api-security", "-d", "/sia-data", "--api-addr", ":9980" ]