# build sia
FROM golang:1.16-alpine AS build

RUN apk update && \
	apk add --no-cache git make ca-certificates && \
	update-ca-certificates

WORKDIR /app
COPY . .
ENV GOBIN=/app/bin

# need to run git status first to fix GIT_DIRTY detection in makefile
RUN git status > /dev/null && \
        update-ca-certificates && \
	make static

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /app/release /

# TODO: can we list these with their protocols (/tcp or /udp)?
# It would also be nice to document what these are.
EXPOSE 9981 9982 9983 9984

# SIA_WALLET_PASSWORD is used to automatically unlock the wallet
ENV SIA_WALLET_PASSWORD=
# SIA_API_PASSWORD sets the password used for API authentication
ENV SIA_API_PASSWORD=

# The diretive below is not functionality supported by siad, but it
# would be nice if at some point it was. :)
#HEALTHCHECK --interval=5m --timeout=10s CMD [ "/siad", "--health" ]

VOLUME [ "/sia-data" ]

ENTRYPOINT [ "/siad", "--disable-api-security", "-d", "/sia-data", "--api-addr", ":9980" ]
