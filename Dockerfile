FROM centos:7
WORKDIR /go
ENV GOPATH /go
ENV GOBIN /go/bin
ENV PATH $PATH:$GOBIN:$GOPATH
ENV GO111MODULE=on
RUN mkdir /vol

#update pkgs 
#RUN apt-get update -q && apt-get install -y apt-utils
#RUN apt-get update -q && apt-get install -y pkg-config 
#RUN apt-get update -q && apt-get install -y wget gpgv software-properties-common
RUN yum install -y epel-release make curl which git  

#get confluent repo
#RUN wget -qO - http://packages.confluent.io/deb/5.0/archive.key | apt-key add -
#RUN add-apt-repository "deb [arch=amd64] http://packages.confluent.io/deb/5.0 stable main"
RUN rpm --import https://packages.confluent.io/rpm/5.0/archive.key
COPY confluent.repo /etc/yum.repos.d/confluent.repo
RUN yum clean all
RUN yum install -y  librdkafka1 librdkafka-dev 

#update packages and get librdkafka,golang

#install golang 111+ for gomod support 
#RUN curl -kO https://dl.google.com/go/go1.11.4.linux-amd64.tar.gz
#RUN tar -C /usr/local -xzf go1.11.4.linux-amd64.tar.gz
#ENV PATH $PATH:/usr/local/go/bin

#install python3, pip
#RUN apt-get update -q && apt-get install -y python3 python3-pip
RUN yum install -y python36 python36-dev python36-setuptools
RUN easy_install-3.6 pip

#install supervisord on python3
RUN pip3 install git+https://github.com/Supervisor/supervisor@master

#install tools to make protobuf
#RUN apt-get update -q && apt-get -y install autoconf automake libtool curl make g++ unzip

#RUN apt-get update -q && apt-get install -y libprotobuf-dev 
#RUN apt-get update -q && apt-get install -y protobuf-compiler
RUN yum install -y  golang
RUN yum install -y  protobuf-compiler
RUN yum install -y rpm-build redhat-rpm-config

#build forwarder
#
COPY ./ /go/src/github.com/carbonblack/cb-event-forwarder
RUN cd /go/src/github.com/carbonblack/cb-event-forwarder && make build

#
#create supervisor user
#
RUN useradd supervisor
RUN chown -R supervisor:supervisor /var/log

#ENTRYPOINT ["/bin/sh"]

#
# Start supervisord
#
#

CMD ["supervisord", "-c", "/go/src/github.com/carbonblack/cb-event-forwarder/supervisord.conf"]
