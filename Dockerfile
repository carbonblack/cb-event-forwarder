FROM ubuntu:latest
WORKDIR /go
ENV GOPATH /go
ENV GOBIN /go/bin
ENV PATH $PATH:$GOBIN:$GOPATH
ENV GO111MODULE=on
RUN mkdir /vol

#update pkgs 
RUN apt-get update -q && apt-get install -y apt-utils
RUN apt-get update -q && apt-get install -y pkg-config 
RUN apt-get update -q && apt-get install -y wget gpgv software-properties-common

#get confluent repo
RUN wget -qO - http://packages.confluent.io/deb/5.0/archive.key | apt-key add -
RUN add-apt-repository "deb [arch=amd64] http://packages.confluent.io/deb/5.0 stable main"

#update packages and get librdkafka,golang
RUN apt-get update -q && apt-get install -y git librdkafka1 librdkafka-dev  
RUN apt-get update -q && apt-get install -y curl  

#install golang 111+ for gomod support 
RUN curl -kO https://dl.google.com/go/go1.11.4.linux-amd64.tar.gz
RUN tar -C /usr/local -xzf go1.11.4.linux-amd64.tar.gz
ENV PATH $PATH:/usr/local/go/bin

#install tools to make protobuf
RUN apt-get update -q && apt-get install -y libprotobuf-dev 
RUN apt-get update -q && apt-get install -y protobuf-compiler

#todo: add static deps libz, openssl, cyrus-sasl

#build forwarder
#
COPY ./ /go/src/github.com/carbonblack/cb-event-forwarder
RUN cd /go/src/github.com/carbonblack/cb-event-forwarder && make build-no-static

ENTRYPOINT ["/bin/bash"]
