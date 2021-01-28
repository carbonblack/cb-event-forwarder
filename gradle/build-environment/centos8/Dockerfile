# This dockerfile creates the cb enterprise build environment
ARG ARTIFACTORY_SERVER=artifactory-pub.bit9.local
ARG BASE_IMAGE=${ARTIFACTORY_SERVER}:5000/cb/connector_env_base:centos8-1.0.0

FROM ${BASE_IMAGE}

ARG ARTIFACTORY_SERVER
ENV ARTIFACTORY_SERVER=${ARTIFACTORY_SERVER}

ARG BASE_IMAGE
ENV BASE_IMAGE=${BASE_IMAGE}

#INSTALL GO TOOLCHAIN
RUN yum -y install curl
RUN curl -LO https://golang.org/dl/go1.15.7.linux-amd64.tar.gz
RUN tar -C /usr/local -xzf go1.15.7.linux-amd64.tar.gz
ENV PATH $PATH:/usr/local/go/bin

#ENABLE POWER TOOLS FOR PROTOBUF SUPPORT
RUN yum -y install dnf-plugins-core
RUN yum config-manager --set-enabled PowerTools

#INSTALL PROTOBUF
RUN yum -y install protobuf-devel protobuf-compiler

ADD entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
