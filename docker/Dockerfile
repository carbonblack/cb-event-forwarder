# This Dockerfile creates the EF Production Container
ARG ARTIFACTORY_ENV=dev
ARG ARTIFACTORY_BASE_URL
ARG ARTIFCATORY_DOCKER_URL
ARG DOCKER_IMAGE_TAG
ARG BASE_IMAGE=${ARTIFCATORY_DOCKER_URL}.${ARTIFACTORY_BASE_URL}/cb/connector_env_base:centos8-$DOCKER_IMAGE_TAG
FROM $BASE_IMAGE
ARG ARTIFACTORY_ENV
ARG ARTIFACTORY_BASE_URL
ARG ARTIFCATORY_DOCKER_URL
ARG BASE_IMAGE

ENV ARTIFACTORY_SERVER=${ARTIFACTORY_BASE_URL}
ENV BASE_IMAGE=$BASE_IMAGE
ENV GOFLAGS -modcacherw

ADD cb-event-forwarder-*.el8.x86_64.rpm /tmp
RUN dnf -y update && \
    dnf -y install /tmp/cb-event-forwarder-*.el8.x86_64.rpm
RUN mv /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf /root
ADD compose.yml /compose.yml
ADD entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
