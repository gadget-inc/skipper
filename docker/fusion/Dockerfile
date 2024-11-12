FROM ubuntu:latest

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl unzip && \
    apt-get autoremove -y && \
    apt-get purge -y --auto-remove && \
    rm -rf /var/lib/apt/lists/*

RUN update-ca-certificates

RUN useradd -ms /bin/bash fusion

WORKDIR /home/fusion

USER fusion

COPY fusion .

ENTRYPOINT ["./fusion"]
