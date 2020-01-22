#GIT_VERSION := $(shell git describe --tags)
#VERSION := $(shell cat VERSION)

GIT_VERSION := 3.6.1
VERSION := 3.6.1
GO_PREFIX := github.com/carbonblack/cb-event-forwarder
TARGET_OS=linux
PKG_CONFIG_PATH=$PKG_CONFIG_PATH:/usr/lib/pkgconfig/:`find rdkafka.pc 2>/dev/null`
export GO111MODULE=on

.PHONY: clean test rpmbuild rpminstall build rpm

cb-event-forwarder: build

librdkafka:
ifeq ($TARGET_OS,"linux")
	ldconfig -p | grep librdkafka
endif

build-no-static: librdkafka
	go get -u github.com/gogo/protobuf/protoc-gen-gogofast
	go mod tidy
	protoc --gogofast_out=.  ./internal/sensor_events/sensor_events.proto
	go mod verify
	go build ./cmd/cb-event-forwarder
	go build ./cmd/kafka-util

build: librdkafka
	go get -u github.com/gogo/protobuf/protoc-gen-gogofast
	go mod tidy
	protoc --gogofast_out=.  ./internal/sensor_events/sensor_events.proto
	go mod verify
	go build -tags static ./cmd/cb-event-forwarder 
	go build -tags static ./cmd/kafka-util

rpmbuild: librdkafka
	go get -u github.com/gogo/protobuf/protoc-gen-gogofast
	protoc --gogofast_out=.  ./internal/sensor_events/sensor_events.proto
	go build -tags static -ldflags "-X main.version=${VERSION}" ./cmd/cb-event-forwarder
	go build -tags static -ldflags "-X main.version=${VERSION}" ./cmd/kafka-util

rpminstall:
	mkdir -p ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder
	cp -p cb-event-forwarder ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/cb-event-forwarder
	cp -p kafka-util ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/kafka-util
	mkdir -p ${RPM_BUILD_ROOT}/etc/cb/integrations/event-forwarder
	cp -p conf/cb-event-forwarder.example.ini ${RPM_BUILD_ROOT}/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
	mkdir -p ${RPM_BUILD_ROOT}/etc/init
	mkdir -p ${RPM_BUILD_ROOT}/etc/init.d
	mkdir -p ${RPM_BUILD_ROOT}/etc/systemd/system
	cp -p init-scripts/cb-event-forwarder ${RPM_BUILD_ROOT}/etc/init.d/cb-event-forwarder
	cp -p init-scripts/cb-event-forwarder.conf ${RPM_BUILD_ROOT}/etc/init/cb-event-forwarder.conf
	cp -p cb-event-forwarder.service ${RPM_BUILD_ROOT}/etc/systemd/system/cb-event-forwarder.service
	mkdir -p ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/content
	cp -rp static/* ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/content

test:
	rm -rf test_output/gold_output
	rm -rf test_output/go_output
	rm -rf test_output/leef_output
	mkdir test_output/gold_output
	python test/scripts/process_events_python.py test/raw_data test_output/gold_output
	go test ./cmd/cb-event-forwarder
	PYTHONIOENCODING=utf8 python test/scripts/compare_outputs.py test_output/gold_output test_output/go_output > test_output/output.txt

clean:
	rm -f cb-event-forwarder
	rm -rf test_output/gold_output
	rm -rf test_output/go_output
	rm -rf dist
	rm -rf build
	rm -f VERSION
	rm -rf librdkafka

bench:
	go test -bench=. ./cmd/cb-event-forwarder/

sdist:
	mkdir -p build/cb-event-forwarder-${GIT_VERSION}/src/${GO_PREFIX}
	echo "${GIT_VERSION}" > build/cb-event-forwarder-${GIT_VERSION}/VERSION
	cp -rp cb-event-forwarder cb-event-forwarder.service Makefile go.mod cmd static conf internal init-scripts build/cb-event-forwarder-${GIT_VERSION}/src/${GO_PREFIX}
	cp -rp MANIFEST build/cb-event-forwarder-${GIT_VERSION}/MANIFEST
	(cd build; tar -cz -f cb-event-forwarder-${GIT_VERSION}.tar.gz cb-event-forwarder-${GIT_VERSION})
	sleep 1
	mkdir -p dist
	cp -p build/cb-event-forwarder-${GIT_VERSION}.tar.gz dist/

rpm: sdist
	mkdir -p ${HOME}/rpmbuild/SOURCES
	cp -p dist/cb-event-forwarder-${GIT_VERSION}.tar.gz ${HOME}/rpmbuild/SOURCES/
	rpmbuild --define 'version ${GIT_VERSION}' --define 'release 0' -bb cb-event-forwarder.rpm.spec
