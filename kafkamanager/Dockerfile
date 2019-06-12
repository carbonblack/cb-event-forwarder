FROM centos:7

RUN yum install -y which git curl epel-release sbt 

RUN yum install -y java-1.8.0-openjdk-devel

RUN yum -y groupinstall 'Development Tools'

RUN curl https://bintray.com/sbt/rpm/rpm | tee /etc/yum.repos.d/bintray-sbt-rpm.repo

RUN yum -y install sbt

RUN yum -y install gcc-c++ make

RUN curl -sL https://rpm.nodesource.com/setup_12.x | bash -i

RUN yum install -y nodejs

RUN git clone https://github.com/yahoo/kafka-manager

RUN cd kafka-manager ; ./sbt clean dist

RUN mkdir /kafkamanager
RUN unzip kafka-manager/target/universal/kafka-manager*.zip -d /tmp
RUN mv /tmp/kafka-manager-*/* /kafkamanager/

ENTRYPOINT ["/bin/bash"]
CMD ["-c"]
