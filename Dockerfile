FROM ubuntu:latest
WORKDIR /go
ENV GOPATH /go
ENV GOBIN /go/bin
ENV PATH $PATH:$GOBIN:$GOPATH
RUN apt-get update -q
RUN apt-get install -y apt-utils wget gnupg2 software-properties-common
RUN  wget -qO - http://packages.confluent.io/deb/5.0/archive.key | apt-key add -
RUN add-apt-repository "deb [arch=amd64] http://packages.confluent.io/deb/5.0 stable main"
RUN apt-get update -q
RUN apt-get install -y git librdkafka1 librdkafka-dev golang-go 
RUN apt-get install -y protobuf-compiler
RUN go get -u github.com/golang/dep/cmd/dep
RUN mkdir -p src/github.com/carbonblack
RUN cd src/github.com/carbonblack && git clone https://github.com/carbonblack/cb-event-forwarder 
RUN cd src/github.com/carbonblack/cb-event-forwarder && git checkout 3.4.x-maintenance 
RUN cd src/github.com/carbonblack/cb-event-forwarder && make build 
ENTRYPOINT ["src/github.com/carbonblack/cb-event-forwarder/cb-event-forwarder","vol/cb-event-forwarder/cb-event-forwarder.conf"] 
