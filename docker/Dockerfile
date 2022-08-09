# This dockerfile creates the cb enterprise build environment
ARG ARTIFACTORY_SERVER=artifactory-pub.bit9.local
ARG BASE_IMAGE=${ARTIFACTORY_SERVER}:5000/cb/connector_env_base:centos7-1.4.0

FROM ${BASE_IMAGE}

FROM artifactory-pub.bit9.local:5000/cb/connector_env_base:centos7-1.4.0
ENV GOFLAGS -modcacherw
ADD cb-event-forwarder-*.el7.x86_64.rpm /tmp
RUN yum -y install /tmp/cb-event-forwarder-*.el7.x86_64.rpm
RUN mv /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf /root
ADD compose.yml /compose.yml
ADD entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]