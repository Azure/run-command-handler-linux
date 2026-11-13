#!/usr/bin/env bash
set -euo pipefail

action="$1"
config="$2"

python3 - "$action" "$config" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import time
import urllib.request
import xml.etree.ElementTree as ET

action = sys.argv[1]
config = json.loads(Path(sys.argv[2]).read_text())
agent = Path("/var/lib/waagent")
logs = Path("/var/log/azure")
handlers = (
    "Microsoft.CPlat.Core.RunCommandHandlerLinux",
    "Microsoft.Azure.Extensions.Edp.RunCommandHandlerLinuxTest",
)
service = "managedruncommand.service"
marker = Path("/var/lib/runcommand-local-validation.json")


def active_directory():
    directories = sorted(
        path for handler in handlers
        for path in agent.glob(handler + "-*") if path.is_dir()
    )
    states = {}
    for directory in directories:
        state_file = directory / "config/HandlerState"
        states[directory] = state_file.read_text().strip() if state_file.is_file() else None
    inventory = ", ".join(
        directory.name + " (state=" + (states[directory] or "missing")
        + ", binary=" + ("present" if (directory / "bin/run-command-handler").is_file() else "missing") + ")"
        for directory in directories
    ) or "none"

    def fail(message):
        other = sorted(
            path.name for path in agent.iterdir()
            if path.is_dir() and path not in directories
            and ("runcommand" in path.name.lower() or "run-command" in path.name.lower())
        ) if agent.is_dir() else []
        raise RuntimeError(
            message + ". Discovered RunCommand directories: " + inventory
            + ". Searched handler names: " + ", ".join(handlers)
            + ". Other RunCommand directories in " + str(agent) + ": " + (", ".join(other) or "none")
        )

    def checked(directory):
        if not (directory / "bin/run-command-handler").is_file():
            fail("The selected RunCommand directory has a missing handler binary: " + str(directory))
        if states[directory] not in (None, "Enabled"):
            fail("The selected RunCommand handler is not enabled: " + str(directory))
        return directory

    # HandlerState records the installed version, unlike cached/requested XML versions.
    enabled = [directory for directory in directories if states[directory] == "Enabled"]
    if len(enabled) > 1:
        fail("The VM agent reports multiple enabled RunCommand versions")
    if enabled:
        return checked(enabled[0])

    configurations = sorted(
        agent.glob("ExtensionsConfig*.xml"), key=lambda p: p.stat().st_mtime, reverse=True
    )
    for path in configurations[:1]:
        root = ET.parse(path).getroot()
        references = {
            (element.attrib["name"], element.attrib["version"])
            for element in root.iter()
            if element.tag.rsplit("}", 1)[-1] == "Plugin"
            and element.attrib.get("name") in handlers
            and "version" in element.attrib
        }
        if len(references) == 1:
            handler, version = references.pop()
            directory = agent / (handler + "-" + version)
            if not directory.is_dir():
                fail("The active agent configuration references a missing RunCommand directory: " + str(directory))
            return checked(directory)
        if references:
            fail("The current agent configuration has ambiguous RunCommand handlers or versions")
    candidates = [
        directory for directory in directories
        if states[directory] is None and (directory / "bin/run-command-handler").is_file()
    ]
    if len(candidates) != 1:
        fail("Cannot unambiguously identify the active RunCommand handler directory")
    return candidates[0]


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        while chunk := stream.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


def diagnostic_paths(journal):
    paths = [logs / "run-command-handler", Path("/var/log/waagent.log"), journal]
    for handler in handlers:
        paths.extend(logs.glob(handler + "*"))
        paths.extend(agent.glob(handler + "-*/status"))
    return paths


def collect_logs(archive, journal):
    paths = [path for path in diagnostic_paths(journal) if path.exists()]
    execution_logs = sorted({
        str(log) for path in paths if path.is_dir()
        for log in path.glob("CommandExecution*.log")
    })
    if not execution_logs:
        raise RuntimeError("No RunCommand CommandExecution*.log files were found under " + str(logs))
    with tarfile.open(archive, "w:gz") as bundle:
        for path in paths:
            bundle.add(path, arcname=str(path).lstrip("/"))
    print("Collected RunCommand execution logs: " + ", ".join(execution_logs), flush=True)


def verify():
    hashes = config.get("hashes")
    if not isinstance(hashes, dict) or set(hashes) != {
        "run-command-handler", "immediate-run-command-handler"
    }:
        raise RuntimeError("Verification requires hashes of both locally built RunCommand binaries")
    installed = json.loads(marker.read_text())
    directory = active_directory()
    if str(directory) != installed["directory"]:
        raise RuntimeError("The active RunCommand directory changed after binary replacement")
    if installed["hashes"] != hashes:
        raise RuntimeError("The installation marker does not match the locally built binary hashes")
    for name, expected in hashes.items():
        if digest(directory / "bin" / name) != expected:
            raise RuntimeError("The installed buddy-build binary changed: " + name)
    if config.get("requireService"):
        subprocess.run(["systemctl", "is-active", "--quiet", service], check=True)
        pid = subprocess.check_output(
            ["systemctl", "show", service, "--property=MainPID", "--value"], text=True
        ).strip()
        if not pid.isdigit() or int(pid) == 0:
            raise RuntimeError("RunCommand service has no running main process")
        if digest(Path("/proc") / pid / "exe") != hashes["immediate-run-command-handler"]:
            raise RuntimeError("The running RunCommand service is not the buddy-build binary")
    print("Verified installed buddy-build hashes in " + str(directory), flush=True)


if action == "install":
    directory = active_directory()
    hashes = config["hashes"]
    for name, expected in hashes.items():
        if digest(Path(name)) != expected:
            raise RuntimeError("Downloaded binary checksum mismatch: " + name)
    was_running = subprocess.run(
        ["systemctl", "is-active", "--quiet", service], check=False
    ).returncode == 0
    if was_running:
        subprocess.run(["systemctl", "stop", service], check=True)
    print("Installing buddy-build binaries into " + str(directory), flush=True)
    for name in hashes:
        destination = directory / "bin" / name
        staged = destination.with_name(destination.name + ".validation-new")
        with Path(name).open("rb") as source, staged.open("wb") as target:
            while chunk := source.read(1024 * 1024):
                target.write(chunk)
            target.flush()
            os.fsync(target.fileno())
        os.chown(staged, 0, 0)
        staged.chmod(0o755)
        os.replace(staged, destination)
    marker.write_text(json.dumps({"directory": str(directory), "hashes": hashes}))
    marker.chmod(0o600)
    if was_running or config.get("requireService"):
        subprocess.run(["systemctl", "restart", service], check=True)
    verify()
elif action == "wait-service":
    deadline = time.monotonic() + 600
    while subprocess.run(["systemctl", "is-active", "--quiet", service], check=False).returncode:
        if time.monotonic() >= deadline:
            raise RuntimeError("Timed out waiting for managedruncommand.service to become active")
        time.sleep(10)
    print("RunCommand service is active", flush=True)
elif action == "verify":
    verify()
elif action == "collect":
    archive = Path("/var/tmp/runcommand-validation-logs.tar.gz")
    journal = Path("/var/tmp/runcommand-validation-journal.txt")
    with journal.open("w") as stream:
        result = subprocess.run(
            ["journalctl", "--unit=" + service, "--no-pager", "--since=-2h"],
            stdout=stream, stderr=subprocess.STDOUT, check=False
        )
        stream.write("\njournalctl exit code: " + str(result.returncode) + "\n")
    collect_logs(archive, journal)
    # Upload using a write-only user-delegation SAS; never print the URL.
    request = urllib.request.Request(
        config["uploadUri"], data=archive.read_bytes(), method="PUT",
        headers={"x-ms-blob-type": "BlockBlob", "Content-Type": "application/gzip"}
    )
    with urllib.request.urlopen(request, timeout=120) as response:
        if response.status != 201:
            raise RuntimeError("Log upload returned HTTP " + str(response.status))
    print("Uploaded RunCommand diagnostic archive", flush=True)
else:
    raise RuntimeError("Unknown guest action: " + action)
PY
