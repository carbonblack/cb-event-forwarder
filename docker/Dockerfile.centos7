FROM centos:7
WORKDIR /go
ENV GOPATH /go
ENV GOBIN /go/bin
ENV PATH $PATH:$GOBIN:$GOPATH
ENV GO111MODULE=on
RUN mkdir /vol

RUN yum install -y make which git curl epel-release 

#get confluent repo
RUN rpm --import https://packages.confluent.io/rpm/5.0/archive.key
COPY confluent.repo /etc/yum.repos.d/confluent.repo
RUN yum clean all
RUN yum install -y  librdkafka-devel 

#update packages and get librdkafka,golang

RUN yum install -y golang
RUN yum install -y protobuf-compiler

RUN yum install -y zlib zlib-devel cyrus-sasl-devel openssl-devel

#build forwarder
#
COPY ./ /go/src/github.com/carbonblack/cb-event-forwarder
RUN cd /go/src/github.com/carbonblack/cb-event-forwarder

ENTRYPOINT ["/bin/bash"]
