GIT_VERSION := 3.7.4
VERSION := 3.7.4
GO_PREFIX := github.com/carbonblack/cb-event-forwarder
EL_VERSION := $(shell rpm -E %{rhel})
TARGET_OS=linux
export GO111MODULE=auto

.PHONY: clean test rpmbuild rpminstall build rpm

cb-event-forwarder: build

getdeps:
	go mod download -x

easyjson:
	go get github.com/mailru/easyjson/...

generateeasyjsonmodels: easyjson
	cd pkg/protobufmessageprocessor ; easyjson -all protobuf_json_structs.go

protocgengo: 
	go install google.golang.org/protobuf/cmd/protoc-gen-go

compile-protobufs: protocgengo
	protoc --go_out=.  pkg/sensorevents/sensor_events.proto

format:
	go fmt cmd/cb-event-forwarder/*.go

build-no-static: compile-protobufs format 
	go build ./cmd/cb-event-forwarder
	go build ./cmd/kafka-util

build: 
	go build -tags static ./cmd/cb-event-forwarder
	go build -tags static ./cmd/kafka-util

rpmbuild:
	go build -tags static -ldflags "-X main.version=${VERSION}" ./cmd/cb-event-forwarder
	go build -tags static -ldflags "-X main.version=${VERSION}" ./cmd/kafka-util

rpminstall:
	mkdir -p ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder
	cp -p cb-event-forwarder ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/cb-event-forwarder
	cp -p kafka-util ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/kafka-util
	cp -p cb-edr-fix-permissions.sh ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/cb-edr-fix-permissions.sh
	mkdir -p ${RPM_BUILD_ROOT}/etc/cb/integrations/event-forwarder
	cp -p conf/cb-event-forwarder.example.ini ${RPM_BUILD_ROOT}/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
ifeq (${EL_VERSION},6)
	mkdir -p ${RPM_BUILD_ROOT}/etc/init.d
	cp -p init-scripts/cb-event-forwarder ${RPM_BUILD_ROOT}/etc/init.d/cb-event-forwarder
	chmod 755 ${RPM_BUILD_ROOT}/etc/init.d/cb-event-forwarder
else
	mkdir -p ${RPM_BUILD_ROOT}/etc/systemd/system
	cp -p cb-event-forwarder.service ${RPM_BUILD_ROOT}/etc/systemd/system/cb-event-forwarder.service
endif
	mkdir -p ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/content
	cp -rp static/* ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/content

unittest: compile-protobufs
	go test ./cmd/cb-event-forwarder

test: unittest
	rm -rf test_output/gold_output
	rm -rf test_output/go_output
	rm -rf test_output/leef_output
	mkdir test_output/gold_output
	python test/scripts/process_events_python.py test/raw_data test_output/gold_output
	PYTHONIOENCODING=utf8 python test/scripts/compare_outputs.py test_output/gold_output test_output/go_output > test_output/output.txt

clean:
	rm -f cb-event-forwarder
	rm -rf test_output/gold_output
	rm -rf test_output/go_output
	rm -rf dist
	rm -rf build
	rm -f VERSION

bench:
	go test -bench=. ./cmd/cb-event-forwarder/

sdist: 
	mkdir -p ${RPM_OUTPUT_DIR}/SOURCES/cb-event-forwarder-${GIT_VERSION}/src/${GO_PREFIX}
	echo "${GIT_VERSION}" > ${RPM_OUTPUT_DIR}/SOURCES/cb-event-forwarder-${GIT_VERSION}/VERSION
	cp -rp cb-edr-fix-permissions.sh cb-event-forwarder.service pkg Makefile go.mod cmd static conf init-scripts ${RPM_OUTPUT_DIR}/SOURCES/cb-event-forwarder-${GIT_VERSION}/src/${GO_PREFIX}
	cp -rp MANIFEST${EL_VERSION} ${RPM_OUTPUT_DIR}/SOURCES/cb-event-forwarder-${GIT_VERSION}/MANIFEST
	cd ${RPM_OUTPUT_DIR}/SOURCES ; tar -cz -f cb-event-forwarder-${GIT_VERSION}.tar.gz cb-event-forwarder-${GIT_VERSION} ; cd ..

rpm: sdist
	rpmbuild --define '_topdir ${RPM_OUTPUT_DIR}'  --define 'version ${GIT_VERSION}' --define 'release 1' -bb cb-event-forwarder.rpm.spec

critic: 
	gocritic check -enableAll -disable='#experimental,#opinionated' ./cmd/cb-event-forwarder/*.go
