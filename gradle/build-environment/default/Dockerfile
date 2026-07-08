# This dockerfile creates the cb enterprise build environment
ARG ARTIFACTORY_ENV=dev
ARG ARTIFACTORY_BASE_URL=usw1.packages.broadcom.com
ARG ARTIFCATORY_DOCKER_URL=cb-docker-dev-virtual
ARG DOCKER_IMAGE_TAG
ARG BASE_IMAGE=${ARTIFCATORY_DOCKER_URL}.${ARTIFACTORY_BASE_URL}/cb/connector_env_base:centos7-${DOCKER_IMAGE_TAG}
FROM $BASE_IMAGE
ARG ARTIFACTORY_BASE_URL
ARG ARTIFCATORY_DOCKER_URL
ARG BASE_IMAGE

ENV ARTIFACTORY_SERVER=${ARTIFACTORY_BASE_URL}
ENV BASE_IMAGE=$BASE_IMAGE

# Set GOCache to RW
ENV GOFLAGS -modcacherw

COPY artifactory-docker-ce.repo /etc/yum.repos.d/artifactory-docker-ce.repo
RUN --mount=type=secret,id=artifactory-username \
    --mount=type=secret,id=artifactory-api-key \
    ARTIFACTORY_USERNAME=$(cat /run/secrets/artifactory-username) && \
    ARTIFACTORY_API_KEY=$(cat /run/secrets/artifactory-api-key) && \
    sed -i -e "s|{ARTIFACTORY_USERNAME}|${ARTIFACTORY_USERNAME}|g" -e "s|{ARTIFACTORY_API_KEY}|${ARTIFACTORY_API_KEY}|g" /etc/yum.repos.d/artifactory-docker-ce.repo

RUN yum install -y --disablerepo=* --enablerepo=artifactory-docker-ce-stable docker-ce-cli

ADD entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
