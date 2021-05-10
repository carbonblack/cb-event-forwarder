FROM centos:7
ARG INIFILE
ENV INIFILE $INIFILE
ARG EFEXE 
ENV EFEXE $EFEXE

ADD $EFEXE /
ADD $INIFILE /
ADD bundleone /bundleone
RUN mkdir -p ~/.aws ; touch ~/.aws/credentials ; printf "[default]\naws_access_key_id=AKIAIOSFODNN7EXAMPLE\naws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" > ~/.aws/credentials
ENTRYPOINT ["/bin/bash"]
CMD ["-c" , "sleep 5 && ./$EFEXE $INIFILE --debug"]
