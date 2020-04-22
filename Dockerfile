FROM centos:7
ARG INIFILE
ENV INIFILE $INIFILE

ADD cb-event-forwarder /
ADD $INIFILE /

ENTRYPOINT ["/bin/bash"]
CMD ["-c" , "sleep 45 && ./cb-event-forwarder $INIFILE"]
