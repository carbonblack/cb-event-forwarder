#GIT_VERSION := $(shell git describe --tags)
#VERSION := $(shell cat VERSION)

GIT_VERSION := 3.2.2
VERSION := 3.2.2
GO_PREFIX := github.com/carbonblack/cb-event-forwarder

cb-event-forwarder: build

build:
	go build

rpmbuild:
	go generate ./...
	go get ./...
	go build -ldflags "-X main.version=${VERSION}"

rpminstall:
	mkdir -p ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder
	cp -p cb-event-forwarder ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/cb-event-forwarder
	mkdir -p ${RPM_BUILD_ROOT}/etc/cb/integrations/event-forwarder
	cp -p conf/cb-event-forwarder.example.ini ${RPM_BUILD_ROOT}/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
	mkdir -p ${RPM_BUILD_ROOT}/etc/init
	cp -p init-scripts/cb-event-forwarder.conf ${RPM_BUILD_ROOT}/etc/init/cb-event-forwarder.conf
	mkdir -p ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/content
	cp -rp static/* ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/content

test:
	rm -rf tests/gold_output
	rm -rf tests/go_output
	rm -rf tests/leef_output
	mkdir tests/gold_output
	python tests/scripts/process_events_python.py tests/raw_data tests/gold_output
	go test
	(cd tests/scripts && python compare_outputs.py > ../output.txt)

clean:
	rm -f cb-event-forwarder
	rm -rf tests/gold_output
	rm -rf tests/go_output
	rm -rf dist
	rm -rf build
	rm -f VERSION

sdist:
	mkdir -p build/cb-event-forwarder-${GIT_VERSION}/src/${GO_PREFIX}
	echo "${GIT_VERSION}" > build/cb-event-forwarder-${GIT_VERSION}/VERSION
	cp -rp Makefile *.go static leef conf deepcopy sensor_events init-scripts build/cb-event-forwarder-${GIT_VERSION}/src/${GO_PREFIX}
	cp -rp MANIFEST build/cb-event-forwarder-${GIT_VERSION}/MANIFEST
	(cd build; tar -cvz -f cb-event-forwarder-${GIT_VERSION}.tar.gz cb-event-forwarder-${GIT_VERSION})
	mkdir -p dist
	cp -p build/cb-event-forwarder-${GIT_VERSION}.tar.gz dist/

rpm: sdist
	mkdir -p ${HOME}/rpmbuild/SOURCES
	cp -p dist/cb-event-forwarder-${GIT_VERSION}.tar.gz ${HOME}/rpmbuild/SOURCES/
	rpmbuild --define 'version ${GIT_VERSION}' --define 'release 1' -bb cb-event-forwarder.rpm.spec
