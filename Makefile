# Default for local builds; CI sets GIT_VERSION/VERSION via Gradle (see build.gradle.kts + Jenkins timestamp env).
GIT_VERSION ?= 3.8.5
VERSION ?= $(GIT_VERSION)
GO_PREFIX := github.com/carbonblack/cb-event-forwarder
EL_VERSION := $(shell rpm -E %{rhel})
GOPATH := $(shell go env GOPATH)
TARGET_OS=linux
RABBITMQ_SALT_INTERNAL := ${RABBITMQ_SALT}
export GO111MODULE=auto
# Passed in by Gradle / Jenkins for the consolidated report (optional).
BUILD_NUMBER ?= 0
GIT_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo local)

.PHONY: clean test rpmbuild rpminstall build rpm check-env integration_test generate_report

cb-event-forwarder: build

check-env:
ifndef RABBITMQ_SALT
	$(error RABBITMQ_SALT is not defined)
endif

getdeps:
	go mod download -x

easyjson:
	GOBIN=$(GOPATH)/bin go install github.com/mailru/easyjson/easyjson@v0.9.2

generateeasyjsonmodels: easyjson
	cd pkg/protobufmessageprocessor ; $(GOPATH)/bin/easyjson -all protobuf_json_structs.go

protocgengo:
	go install google.golang.org/protobuf/cmd/protoc-gen-go

compile-protobufs: protocgengo
	protoc --go_out=.  pkg/sensorevents/sensor_events.proto

format:
	go fmt cmd/cb-event-forwarder/*.go

build-no-static: compile-protobufs format
	go build ./cmd/cb-event-forwarder
	go build ./cmd/kafka-util
	go build ./cmd/go-serviced

build:
	go build -tags static ./cmd/cb-event-forwarder
	go build -tags static ./cmd/kafka-util
	go build -tags static ./cmd/go-serviced

rpmbuild: check-env
	go build -tags static -ldflags "-X 'main.version=${VERSION}' -X 'main.rabbitMQSalt=${RABBITMQ_SALT_INTERNAL}'" ./cmd/cb-event-forwarder
	go build -tags static -ldflags "-X 'main.version=${VERSION}'" ./cmd/kafka-util
	go build -tags static -ldflags "-X 'main.version=${VERSION}'" ./cmd/go-serviced

rpminstall:
	mkdir -p ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder
	cp -p cb-event-forwarder ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/cb-event-forwarder
	cp -p kafka-util ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/kafka-util
	cp -p go-serviced ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/go-serviced
	cp -p cb-edr-fix-permissions.sh ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/cb-edr-fix-permissions.sh
	mkdir -p ${RPM_BUILD_ROOT}/etc/cb/integrations/event-forwarder
	cp -p conf/cb-event-forwarder.example.ini ${RPM_BUILD_ROOT}/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
ifeq (${EL_VERSION},6)
	mkdir -p ${RPM_BUILD_ROOT}/etc/init.d
	cp -p init-scripts/cb-event-forwarder ${RPM_BUILD_ROOT}/etc/init.d/cb-event-forwarder
	chmod 755 ${RPM_BUILD_ROOT}/etc/init.d/cb-event-forwarder
else
	mkdir -p ${RPM_BUILD_ROOT}/etc/systemd/system
	install -m 644 cb-event-forwarder.service ${RPM_BUILD_ROOT}/etc/systemd/system/cb-event-forwarder.service
endif
	mkdir -p ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/content
	cp -rp static/* ${RPM_BUILD_ROOT}/usr/share/cb/integrations/event-forwarder/content

unittest: compile-protobufs
	go test ./cmd/cb-event-forwarder

test: unittest
	rm -rf test_output
	rm -rf test_output_old
	mkdir -p test_output/gold_output
	python test/scripts/process_events_python.py test/raw_data test_output/gold_output
	PYTHONIOENCODING=utf8 python test/scripts/compare_outputs.py test_output/gold_output test_output/go_output > test_output/output.txt

clean:
	rm -f cb-event-forwarder
	rm -rf test_output
	rm -rf test_output_old
	rm -rf dist
	rm -rf build
	rm -f VERSION

bench:
	go test -bench=. ./cmd/cb-event-forwarder/

sdist:
	mkdir -p ${RPM_OUTPUT_DIR}/SOURCES/cb-event-forwarder-${GIT_VERSION}/src/${GO_PREFIX}
	echo "${GIT_VERSION}" > ${RPM_OUTPUT_DIR}/SOURCES/cb-event-forwarder-${GIT_VERSION}/VERSION
	cp -rp cb-edr-fix-permissions.sh cb-event-forwarder.service pkg Makefile go.mod go.sum cmd static conf init-scripts ${RPM_OUTPUT_DIR}/SOURCES/cb-event-forwarder-${GIT_VERSION}/src/${GO_PREFIX}
	cp -rp MANIFEST${EL_VERSION} ${RPM_OUTPUT_DIR}/SOURCES/cb-event-forwarder-${GIT_VERSION}/MANIFEST
	cd ${RPM_OUTPUT_DIR}/SOURCES ; tar -cz -f cb-event-forwarder-${GIT_VERSION}.tar.gz cb-event-forwarder-${GIT_VERSION} ; cd ..

rpm: sdist
	rpmbuild --define '_topdir ${RPM_OUTPUT_DIR}' --define 'bare_version ${GIT_VERSION}' --define 'release 1' -bb cb-event-forwarder.rpm.spec

critic:
	gocritic check -enableAll -disable='#experimental,#opinionated' ./cmd/cb-event-forwarder/*.go

go-junit-report:
	GOBIN=$(GOPATH)/bin go install github.com/jstemmer/go-junit-report/v2@latest

unittest_coverage: go-junit-report compile-protobufs
	mkdir -p build/code_coverage/
	mkdir -p build/test-results/
	go test ./tests -json -cover -covermode=atomic -coverpkg ./pkg/...,./cmd/... -coverprofile=build/code_coverage/coverage.out > build/code_coverage/test_output.json 2>&1 || (cat build/code_coverage/test_output.json >&2; exit 1)
	# generate junit format xml from json report
	$(GOPATH)/bin/go-junit-report -parser gojson -in build/code_coverage/test_output.json -out build/test-results/junit.xml
	# generate html coverage report
	go tool cover -html=build/code_coverage/coverage.out -o build/code_coverage/coverage.html
	# generate function level coverage
	go tool cover -func=build/code_coverage/coverage.out | tee build/code_coverage/unittest_coverage.txt | grep 'total:'
	# calculate average function coverage percentage
	awk '/^[^t]/ && NF==3 {total++; gsub(/%/, "", $$3); sum += $$3} END {if (total>0) printf "%.1f\n", sum/total; else print "0.0"}' build/code_coverage/unittest_coverage.txt > build/code_coverage/function_coverage_percent.txt

integration_test: go-junit-report compile-protobufs
	mkdir -p build/integration-test-results/
	go test -tags integration ./tests -json -v -timeout 120s -run TestIntegration \
		-cover -covermode=atomic -coverpkg ./pkg/...,./cmd/... \
		-coverprofile=build/integration-test-results/coverage.out \
		> build/integration-test-results/test_output.json 2>&1; \
	TEST_EXIT=$$?; \
	$(GOPATH)/bin/go-junit-report -parser gojson \
		-in build/integration-test-results/test_output.json \
		-out build/integration-test-results/junit.xml; \
	if [ -f build/integration-test-results/coverage.out ]; then \
		go tool cover -html=build/integration-test-results/coverage.out \
			-o build/integration-test-results/coverage.html; \
		go tool cover -func=build/integration-test-results/coverage.out \
			| tee build/integration-test-results/integration_coverage.txt \
			| grep 'total:' || true; \
		awk '/^[^t]/ && NF==3 {total++; gsub(/%/, "", $$3); sum += $$3} END {if (total>0) printf "%.1f\n", sum/total; else print "0.0"}' \
			build/integration-test-results/integration_coverage.txt \
			> build/integration-test-results/function_coverage_percent.txt; \
	fi; \
	if [ $$TEST_EXIT -ne 0 ]; then \
		cat build/integration-test-results/test_output.json >&2; \
		exit $$TEST_EXIT; \
	fi

# Generate a single consolidated HTML report from unit and integration test data.
# All inputs are optional; the script handles missing files gracefully.
generate_report:
	python3 scripts/generate_test_report.py \
		--branch       "$(GIT_BRANCH)" \
		--build-number "$(BUILD_NUMBER)" || true
