FROM ubuntu:latest
WORKDIR /go
ENV GOPATH /go
ENV GOBIN /go/bin
ENV PATH $PATH:$GOBIN:$GOPATH
ENV GO111MODULE=on

#update pkgs 
RUN apt-get update -q
RUN apt-get install -y apt-utils wget gnupg2 software-properties-common

#get confluent repo
RUN  wget -qO - http://packages.confluent.io/deb/5.0/archive.key | apt-key add -
RUN add-apt-repository "deb [arch=amd64] http://packages.confluent.io/deb/5.0 stable main"

#update packages and get librdkafka,golang
RUN apt-get update -q
RUN apt-get install -y git librdkafka1 librdkafka-dev golang-go 

#install python3, pip
RUN apt-get install -y python3 python3-pip

#install supervisord on python3
RUN pip3 install git+https://github.com/Supervisor/supervisor@master

#install tools to make protobuf
RUN apt-get -y install autoconf automake libtool curl make g++ unzip

#checkout protobuf and build from source
RUN git clone https://github.com/protocolbuffers/protobuf.git
RUN cd protobuf && git submodule update --init --recursive && ./autogen.sh
RUN cd protobuf && ./configure
RUN cd protobuf &&  make && make check && make install && ldconfig 

#TODO: remove dep forever
#get dep to manage golang dependencies
#RUN go get -u github.com/golang/dep/cmd/dep

#Install a specific version of protoc-gen-go
RUN mkdir -p src/github.com/golang/protobuf
RUN cd src/github.com/golang && git clone https://github.com/golang/protobuf.git 
RUN cd $GOPATH/src/github.com/golang/protobuf/protoc-gen-go && git checkout master && go install

#install cb-event-forwarder from source
RUN mkdir -p /go/src/github.com/carbonblack/cb-event-forwarder
RUN cd /go/src/github.com/carbonblack && git clone https://github.com/carbonblack/cb-event-forwarder
RUN cd /go/src/github.com/carbonblack/cb-event-forwarder && git checkout dockerbuild 
RUN cd /go/src/github.com/carbonblack/cb-event-forwarder && make build 

#SET PYTHONPATH
#
##
#
ENV PYTHONPATH  /vol 

#
#create supervisor user
#
RUN useradd supervisor
RUN chown -R supervisor:supervisor /var/log
ENTRYPOINT ["/bin/sh", "-c"]

#
# Start supervisord
#
#

#CMD ["supervisord", "-c", "/vol/cb-event-forwarder/supervisord.conf"]
