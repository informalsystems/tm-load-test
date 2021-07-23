FROM ubuntu:latest

ARG ROOT_DIR=/root
# install golang
RUN curl https://dl.google.com/go/go1.16.4.linux-amd64.tar.gz --output go.tar.gz
RUN tar -C /usr/local -xzf go.tar.gz
ENV PATH="/usr/local/go/bin:$PATH"
ENV GOPATH=/go
ENV PATH=$PATH:$GOPATH/bin

RUN apt-get install make

WORKDIR $ROOT_DIR
RUN git clone https://github.com/sunnyk56/tm-load-test.git

WORKDIR $ROOT_DIR/tm-load-test
RUN make

RUN cp /build/tm-load-test /usr/bin/tm-load-test

WORKDIR $ROOT_DIR






