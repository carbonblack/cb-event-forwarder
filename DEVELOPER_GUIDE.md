# Building and Developing cb-event-forwarder

This guide is for contributors and developers who have cloned this repository and want to
build, modify, and run the Event Forwarder from source.

For installation of a pre-built release, see [README.md](README.md).

---

## Prerequisites

| Tool | Minimum version | Purpose |
|---|---|---|
| Go | 1.25+ (as declared in `go.mod`) | Compile the Go source |
| make | any | Invoke build targets |
| protoc | 3.x | Regenerate protobuf files (only if `.proto` changes) |
| rpmbuild | any | Build the RPM package (Linux only) |
| git | any | Version detection during build |

Install Go from https://go.dev/dl/ and ensure `go` is on your `PATH`.

---

## Build Paths

There are two independent build paths. Most contributors only need **Path 1**.

### Path 1 — Go / Make build (no Artifactory required)

This is the standard path for local development and for building the binary or RPM.
It has **no dependency on Artifactory** whatsoever.

```bash
# Clone the repo
git clone https://github.com/carbonblack/cb-event-forwarder.git
cd cb-event-forwarder

# Download Go module dependencies
make getdeps

# Compile the binary
make build

# Run unit tests
go test ./tests
```

To build the RPM package you need `rpmbuild` installed. `RABBITMQ_SALT` must always be set:

```bash
export RABBITMQ_SALT="<your-salt-value>"
make rpm
```

> **What value to use for `RABBITMQ_SALT`?**
> - **On RHEL 9 / EL9:** The salt is used at runtime to derive the RabbitMQ password from the token in `/etc/cb/cb.conf`. The value **must match** what the EDR server uses — obtain it from your EDR installation or Broadcom support contact.
> - **On EL8 and earlier:** The EDR uses a plaintext `RabbitMQPassword` so the salt is never used at runtime. You can set it to any non-empty string to satisfy the build.

The RPM is written to `$RPM_OUTPUT_DIR/RPMS/x86_64/` (defaults to `build/<el-version>/rpm/`).

---

### Path 2 — Gradle / CI build (Artifactory required)

The Gradle build is used by internal CI pipelines. It wraps the `make` targets,
adds Docker image publishing, smoke/regression/perf tests, and artifact upload.

This path requires access to an Artifactory instance that hosts the internal
Gradle plugins (`com.carbonblack.gradle-dockerized-wrapper`, `com.palantir.git-version`, etc.).

#### Environment Variables

Set the following before running any `./gradlew` command:

| Variable | Required | Description |
|---|---|---|
| `ARTIFACTORY_URL` | Yes | **Full URL** of the Artifactory repository used for Gradle plugin and dependency resolution. The URL you provide is used as-is — include the repo path if needed (e.g. `https://myartifactory.example.com/artifactory/my-virtual-repo`). |
| `ARTIFACTORY_BASE_URL` | Yes (Docker tasks) | Artifactory **hostname only** (no `https://`, no path). Used for Docker registry login and image tagging (e.g. `myartifactory.example.com`). |
| `ACCESS_ID` | Yes | Artifactory username / access ID. |
| `ACCESS_TOKEN` | Yes | Artifactory API token or password. |
| `GOPROXY` | No | Go module proxy URL. Defaults to `https://proxy.golang.org` if unset. |

Example setup:

```bash
export ARTIFACTORY_URL="https://myartifactory.example.com/artifactory/my-virtual-repo"
export ARTIFACTORY_BASE_URL="myartifactory.example.com"
export ACCESS_ID="your-username"
export ACCESS_TOKEN="your-api-token"
```

#### Run the Gradle build

```bash
# Build the binary + RPM (equivalent to make rpm, wrapped by Gradle)
./gradlew build

# Run unit tests
./gradlew runUnitTests

# Run integration tests
./gradlew runIntegrationTests

# Build + upload RPM to Artifactory (CI only, requires JENKINS_CI_VERSION to be set)
./gradlew runJenkinsBuild
```

---

## Protobuf Regeneration

Only needed if you modify `pkg/sensorevents/sensor_events.proto`:

```bash
make compile-protobufs
```

This requires `protoc` and `protoc-gen-go` on your `PATH`. The generated
`sensor_events.pb.go` file is committed to the repository so most contributors
do not need to run this step.

---

## Running the Binary Locally

After `make build`, the `cb-event-forwarder` binary is produced in the repo root.
It requires a configuration file:

```bash
# Copy the example config and edit it
cp conf/cb-event-forwarder.example.ini /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
$EDITOR /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf

# Validate the configuration (prints "Initialized output" on success)
sudo ./cb-event-forwarder -check /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf

# Run
sudo ./cb-event-forwarder /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
```

Key configuration fields to set for a standalone test:

```ini
# RabbitMQ connection (required)
rabbit_mq_username=<username>
rabbit_mq_password=<password>
cb_server_hostname=<edr-server-hostname>

# Output — write events to a local file for testing
output_type=file
outfile=/tmp/event_bridge_output.json
```

---

## Project Layout

```
cb-event-forwarder/
├── cmd/cb-event-forwarder/   # Main binary entry point
├── cmd/go-serviced/          # Systemd service helper
├── cmd/kafka-util/           # Kafka utility binary
├── pkg/                      # Core library packages
│   ├── config/               # Configuration parsing
│   ├── forwarder/            # Main forwarder logic
│   ├── outputs/              # Output adapters (file, S3, Kafka, HTTP, ...)
│   ├── protobufmessageprocessor/ # Protobuf event processing
│   ├── rabbitmq/             # RabbitMQ AMQP consumer
│   ├── sensorevents/         # Generated protobuf types
│   └── utils/                # Shared utilities
├── tests/                    # Unit and integration tests
├── conf/                     # Example configuration file
├── Makefile                  # Primary build entry point
├── build.gradle.kts          # Gradle CI build (wraps make)
└── settings.gradle.kts       # Gradle plugin management
```

---

## Common Issues

**`protoc: command not found`**
Install `protoc` from https://grpc.io/docs/protoc-installation/ and run `make compile-protobufs` again.

**Gradle fails on plugin resolution**
Ensure `ARTIFACTORY_URL`, `ACCESS_ID`, and `ACCESS_TOKEN` are all exported in your shell before running `./gradlew`.

**`go: cannot find module providing package`**
Run `make getdeps` first to populate the Go module cache.
