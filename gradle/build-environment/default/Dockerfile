# This dockerfile creates the cb enterprise build environment
ARG ARTIFACTORY_SERVER=artifactory-pub.bit9.local
ARG BASE_IMAGE=${ARTIFACTORY_SERVER}:5000/cb/connector_env_base:centos7-1.4.0

FROM ${BASE_IMAGE}

ARG ARTIFACTORY_SERVER
ENV ARTIFACTORY_SERVER=${ARTIFACTORY_SERVER}

ARG BASE_IMAGE
ENV BASE_IMAGE=${BASE_IMAGE}

# Set GOCache to RW
ENV GOFLAGS -modcacherw

RUN curl "https://${ARTIFACTORY_SERVER}/artifactory/cb/repos/yum/centos7/artifactory-docker-ce.repo" > /etc/yum.repos.d/artifactory-docker-ce.repo
RUN yum install -y docker-ce-cli

ADD entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
