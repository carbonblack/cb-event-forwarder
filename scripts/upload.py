#!/usr/bin/env python3
"""
Upload cb-event-forwarder unsigned Yum content (RPM + repodata from createYumRepo) to Artifactory.

Source layout matches Gradle: build/<elN>/rpm/RPMS/x86_64/ (Event Forwarder RPM output).
Signing is handled elsewhere; this upload is unsigned only.

Requires: jfrog CLI on PATH, JENKINS_CI_VERSION set by CI so Gradle invokes upload.
Auth: USW1_ACCESS_ID_DEV + USW1_ACCESS_TOKEN_DEV (or ARTIFACTORY_USER + ARTIFACTORY_API_KEY).
"""
from __future__ import annotations

import glob
import logging
import os
import re
import shlex
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from optparse import OptionParser
from typing import Dict, Optional

_logger = logging.getLogger(__name__)

JFROG_CLI = os.environ.get("JFROG_CLI", "jfrog")
ARTIFACTORY_PUB_URL = "https://usw1.packages.broadcom.com/artifactory"
API_KEY_PATTERN = re.compile(r"(--apikey)=(\S+)")

TIMESTAMP_ENV_NAMES = ("CB_EDR_CONNECTOR_BUILD_TIMESTAMP", "cb_edr_connector_build_timestamp")

# Artifactory path (cbent-style): unsigned/<branch>/<baseVersion>.<timestamp>/
# baseVersion from gradle.properties currentVersion with RPM release stripped (3.8.5-1 -> 3.8.5).

# e.g. cb-event-forwarder-3.8.5-1.el8.x86_64.rpm — group 1 is version-release (3.8.5-1)
RPM_GLOB = "build/{os}/rpm/RPMS/x86_64/cb-event-forwarder-*.rpm"
RPM_VERSION_RE = re.compile(
    r"^cb-event-forwarder-(.+)\.(el7(?:\.centos)?|el8|el9)\.x86_64\.rpm$"
)


def _repo_root() -> str:
    return os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))


def _get_timestamp() -> str:
    for name in TIMESTAMP_ENV_NAMES:
        v = os.environ.get(name)
        if v:
            return v.strip()
    return "local"


def _get_git_branch() -> str:
    cmd = ["git", "rev-parse", "--abbrev-ref", "HEAD"]
    _logger.info("Resolving branch: %s", " ".join(cmd))
    p = subprocess.run(cmd, cwd=_repo_root(), capture_output=True, text=True, check=False)
    if p.returncode != 0:
        raise RuntimeError("Unable to determine git branch: %s" % (p.stderr or p.stdout))
    return p.stdout.strip()


def _get_branch() -> str:
    b = (
        os.environ.get("CI_BUILD_REF_NAME")
        or os.environ.get("GIT_BRANCH_LOCAL")
        or _get_git_branch()
    )
    return b.replace("origin/", "").strip()


def _strip_rpm_release_suffix(version: str) -> str:
    """Strip trailing RPM release: 3.8.5-1 -> 3.8.5."""
    m = re.match(r"^(.+)-(\d+)$", version.strip())
    if m:
        return m.group(1)
    return version.strip()


def _base_version_from_gradle_properties() -> str:
    """Read currentVersion from gradle.properties and drop -<release> (e.g. 3.8.5-1 -> 3.8.5)."""
    props_path = os.path.join(_repo_root(), "gradle.properties")
    if not os.path.isfile(props_path):
        raise RuntimeError("gradle.properties not found at %s" % props_path)
    with open(props_path, encoding="utf-8") as f:
        for line in f:
            line = line.split("#")[0].strip()
            if line.startswith("currentVersion="):
                raw = line.split("=", 1)[1].strip()
                return _strip_rpm_release_suffix(raw)
    raise RuntimeError("currentVersion not found in gradle.properties")


def _parse_rpm_version(os_type: str) -> str:
    pattern = os.path.join(_repo_root(), RPM_GLOB.format(os=os_type))
    files = glob.glob(pattern)
    if len(files) != 1:
        raise RuntimeError(
            "Expected exactly one cb-event-forwarder RPM matching %s, found %d" % (pattern, len(files))
        )
    base = os.path.basename(files[0])
    m = RPM_VERSION_RE.match(base)
    if not m:
        raise RuntimeError("Could not parse version from RPM filename: %s" % base)
    return m.group(1)


def _property_headers() -> Dict[str, str]:
    token = os.environ.get("USW1_ACCESS_TOKEN_DEV") or os.environ.get("ARTIFACTORY_API_KEY", "")
    return {"X-JFrog-Art-Api": token}


def update_properties(repo_path: str, properties: Dict[str, str], recursive: bool = False) -> None:
    props = ";".join("%s=%s" % (k, urllib.parse.quote(str(v), safe="")) for k, v in properties.items())
    url = "%s/api/storage/%s?properties=%s&recursive=%d" % (
        ARTIFACTORY_PUB_URL,
        repo_path.rstrip("/"),
        props,
        int(recursive),
    )
    _logger.info("Setting properties on %s", repo_path)
    req = urllib.request.Request(url, method="PUT", headers=_property_headers())
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            if resp.status not in (200, 201, 204):
                raise RuntimeError("Unexpected status %s" % resp.status)
    except urllib.error.HTTPError as e:
        raise RuntimeError("Property update failed: %s %s" % (e.code, e.read().decode(errors="replace"))) from e


class UploadRunner:
    """One tree: build/<el>/rpm/RPMS/x86_64/."""

    def __init__(self, user: Optional[str], api_key: Optional[str], os_type: Optional[str]) -> None:
        self.username = user or os.environ.get("USW1_ACCESS_ID_DEV") or os.environ.get("ARTIFACTORY_USER")
        self.api_key = str(
            api_key or os.environ.get("USW1_ACCESS_TOKEN_DEV") or os.environ.get("ARTIFACTORY_API_KEY") or ""
        )
        self.os_type = (os_type or "el7").strip()
        self.git_branch = _get_branch()
        self.timestamp = _get_timestamp()
        self.version = _parse_rpm_version(self.os_type)
        try:
            self.base_product_version = _base_version_from_gradle_properties()
        except Exception as ex:
            _logger.warning("Using RPM filename for base version (gradle.properties: %s)", ex)
            self.base_product_version = _strip_rpm_release_suffix(self.version)
        # e.g. develop/3.8.5.260416.0256 (same pattern as cbent: branch / version.timestamp)
        self.version_timestamp_segment = "%s.%s" % (self.base_product_version, self.timestamp)
        self.source_rel = "build/%s/rpm/RPMS/x86_64" % self.os_type
        self.source_abs = os.path.join(_repo_root(), self.source_rel)
        self.target_root = (
            "cb-generic-dev-local/cbr/product/connectors/event-forwarder/%s/unsigned/%s/%s"
            % (self.os_type, self.git_branch, self.version_timestamp_segment)
        )
        self.props_target = (
            "cb-generic-dev-local/cbr/product/connectors/event-forwarder/%s/unsigned/%s"
            % (self.os_type, self.git_branch)
        )

    def upload(self) -> None:
        if not os.path.isdir(self.source_abs):
            raise RuntimeError("Source directory missing: %s" % self.source_abs)
        target = "%s/" % self.target_root
        cmd = (
            "%s rt upload --url=%s --user=%s --apikey=%s --flat=false --recursive=true "
            "--props=version=%s ./ %s"
            % (
                JFROG_CLI,
                ARTIFACTORY_PUB_URL,
                self.username,
                self.api_key,
                self.version,
                target,
            )
        )
        log_cmd = cmd if _logger.isEnabledFor(logging.DEBUG) else API_KEY_PATTERN.sub(r"\1=<redacted>", cmd)
        _logger.info("Uploading from %s to %s", self.source_abs, target)
        _logger.info("Executing: %s", log_cmd)
        p = subprocess.run(
            shlex.split(cmd),
            cwd=self.source_abs,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        if p.stdout:
            _logger.info(p.stdout.rstrip())
        if p.returncode != 0:
            raise RuntimeError("jfrog rt upload failed with exit code %s" % p.returncode)

        try:
            update_properties(self.props_target, {"latest": self.version}, recursive=False)
        except Exception as ex:
            _logger.warning("Could not set Artifactory properties (optional): %s", ex)


def _build_cli_parser() -> OptionParser:
    parser = OptionParser(
        usage="%prog [OPTIONS]",
        description="Upload cb-event-forwarder unsigned yum artifacts to Artifactory.",
    )
    parser.add_option("-v", "--verbose", action="store_true", dest="verbose", default=False)
    parser.add_option("-u", "--user", dest="user", default=None)
    parser.add_option("-k", "--api-key", dest="api_key", default=None)
    parser.add_option("-o", "--os-type", dest="os_type", default=None, help="el7, el8, or el9")
    return parser


def main(argv: Optional[list] = None) -> int:
    argv = argv if argv is not None else sys.argv[1:]
    parser = _build_cli_parser()
    options, _args = parser.parse_args(argv)
    logging.basicConfig(level=logging.DEBUG if options.verbose else logging.INFO)
    runner = UploadRunner(user=options.user, api_key=options.api_key, os_type=options.os_type)
    runner.upload()
    return 0


if __name__ == "__main__":
    sys.exit(main())
