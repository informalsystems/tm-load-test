FROM cometbft/cometbft:v0.38.2
LABEL maintainer="hello@informal.systems"

ARG UID
ARG GID

USER root

RUN apk update && \
    apk upgrade && \
    apk --no-cache add curl jq file shadow

RUN groupadd -g ${GID} tmgroup || true

# Set the tmuser's UID and GID to the preconfigured values to match the host's
# UID/GID combo. Otherwise permissions become difficult to manage.
RUN usermod -u ${UID} -g ${GID} tmuser

USER tmuser

VOLUME /cometbft
WORKDIR /cometbft

EXPOSE 26656 26657
ENTRYPOINT ["/usr/bin/entrypoint.sh"]
CMD ["node", "--proxy_app", "kvstore"]
STOPSIGNAL SIGTERM

COPY entrypoint.sh /usr/bin/entrypoint.sh
COPY config-template.toml /etc/cometbft/config-template.toml
