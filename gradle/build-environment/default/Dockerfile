# This dockerfile creates the cb enterprise build environment
ARG ARTIFACTORY_SERVER=artifactory-pub.bit9.local
ARG BASE_IMAGE=${ARTIFACTORY_SERVER}:5000/cb/connector_env_base:centos7-1.0.0

FROM ${BASE_IMAGE}

ARG ARTIFACTORY_SERVER
ENV ARTIFACTORY_SERVER=${ARTIFACTORY_SERVER}

ARG BASE_IMAGE
ENV BASE_IMAGE=${BASE_IMAGE}

#INSTALL PROTOBUF
RUN yum -y install protobuf-devel protobuf-compiler

#INSTALL GOCRITIC
RUN curl -OL https://github.com/go-critic/go-critic/releases/download/v0.5.3/gocritic-linux-amd64.zip
RUN unzip gocritic-linux-amd64.zip -d /usr/bin

ADD entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
