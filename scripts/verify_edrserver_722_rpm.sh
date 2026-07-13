#!/usr/bin/env bash
# EDRSERVER-722: verify built RPM installs cb-event-forwarder.service with mode 644.
# Usage: verify_edrserver_722_rpm.sh <rpm_dir>
# Skips if no RPM, no rpm(8), or EL6 package (no systemd unit in that variant).

set -euo pipefail

RPM_DIR="${1:-}"
if [[ -z "${RPM_DIR}" ]]; then
	echo "usage: $0 <path-to-RPMS-x86_64-directory>" >&2
	exit 2
fi

if ! command -v rpm >/dev/null 2>&1; then
	echo "EDRSERVER-722 verify: rpm(8) not found — skip"
	exit 0
fi

if [[ ! -d "${RPM_DIR}" ]]; then
	echo "EDRSERVER-722 verify: directory not found: ${RPM_DIR} — skip"
	exit 0
fi

shopt -s nullglob
rpms=( "${RPM_DIR}"/cb-event-forwarder-*.rpm )
shopt -u nullglob
if [[ ${#rpms[@]} -eq 0 ]]; then
	echo "EDRSERVER-722 verify: no cb-event-forwarder-*.rpm in ${RPM_DIR} — skip"
	exit 0
fi

RPM="${rpms[0]}"
if [[ "${RPM}" == *".el6."* ]]; then
	echo "EDRSERVER-722 verify: EL6 RPM has no systemd unit in this package — skip"
	exit 0
fi

line="$(rpm -qlvp "${RPM}" | grep '/etc/systemd/system/cb-event-forwarder\.service$' || true)"
if [[ -z "${line}" ]]; then
	echo "EDRSERVER-722 verify: systemd unit missing from package: ${RPM}" >&2
	exit 1
fi

if ! echo "${line}" | grep -q '^-rw-r--r--'; then
	echo "EDRSERVER-722 verify: expected unit mode 644 (-rw-r--r--), got:" >&2
	echo "${line}" >&2
	exit 1
fi

echo "EDRSERVER-722 verify: OK (${RPM})"
