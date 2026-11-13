"""Exercise the actual embedded guest code without Azure or system changes."""

import ast
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile
import tarfile
import unittest
from unittest import mock
import xml.etree.ElementTree as ET


class GuestScriptTests(unittest.TestCase):
    def setUp(self):
        source = Path(__file__).with_name("Invoke-RunCommandGuest.sh").read_text()
        embedded = source.split("<<'PY'\n", 1)[1].rsplit("\nPY", 1)[0]
        tree = ast.parse(embedded, feature_version=(3, 10))
        functions = ast.Module(
            body=[node for node in tree.body if isinstance(node, ast.FunctionDef)],
            type_ignores=[],
        )
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.agent = Path(self.temporary.name)
        self.handler = "Microsoft.CPlat.Core.RunCommandHandlerLinux"
        self.test_handler = "Microsoft.Azure.Extensions.Edp.RunCommandHandlerLinuxTest"
        self.hashes = {
            name: hashlib.sha256(b"fixture").hexdigest()
            for name in ("run-command-handler", "immediate-run-command-handler")
        }
        self.namespace = {
            "Path": Path, "ET": ET, "hashlib": hashlib, "json": json, "tarfile": tarfile,
            "agent": self.agent, "logs": self.agent / "logs",
            "marker": self.agent / "validation.json", "config": {"hashes": dict(self.hashes)},
        }
        for node in tree.body:
            if (isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name)
                    and node.targets[0].id in ("handler", "handlers")):
                self.namespace[node.targets[0].id] = ast.literal_eval(node.value)
        exec(compile(functions, "Invoke-RunCommandGuest.sh", "exec"), self.namespace)

    def directory(self, version, state=None, binary=True, handler=None):
        directory = self.agent / ((handler or self.handler) + "-" + version)
        directory.mkdir()
        if binary:
            (directory / "bin").mkdir()
            (directory / "bin/run-command-handler").write_bytes(b"fixture")
            (directory / "bin/immediate-run-command-handler").write_bytes(b"fixture")
        if state is not None:
            (directory / "config").mkdir()
            (directory / "config/HandlerState").write_text(state)
        return directory

    def xml(self, version, name="ExtensionsConfig.xml", namespace="", handler=None):
        path = self.agent / name
        path.write_text(
            '<Extensions xmlns="' + namespace + '"><Plugins><Plugin name="'
            + (handler or self.handler) + '" version="' + version + '"/></Plugins></Extensions>'
        )
        return path

    def active(self):
        return self.namespace["active_directory"]()

    def test_enabled_handler_with_retained_versions_and_no_xml(self):
        self.directory("1.3.20", "Installed")
        active = self.directory("1.3.29", "Enabled\n")
        self.assertEqual(self.active(), active)

    def test_enabled_handler_overrides_stale_xml(self):
        self.directory("1.3.20", "Installed")
        active = self.directory("1.3.29", "Enabled")
        self.xml("1.3.20")
        self.assertEqual(self.active(), active)

    def test_enabled_handler_resolves_requested_major_minor_version(self):
        active = self.directory("1.3.29", "Enabled")
        self.xml("1.3")
        self.assertEqual(self.active(), active)

    def test_multiple_enabled_versions_are_rejected(self):
        self.directory("1.3.20", "Enabled")
        self.directory("1.3.29", "Enabled")
        self.xml("1.3.29")
        with self.assertRaisesRegex(RuntimeError, "multiple enabled"):
            self.active()

    def test_enabled_directory_without_binary_is_rejected(self):
        self.directory("1.3.29", "Enabled", binary=False)
        self.directory("1.3.20")
        with self.assertRaisesRegex(RuntimeError, "missing.*binary"):
            self.active()

    def test_single_directory_without_state_or_xml_is_supported(self):
        active = self.directory("1.3.29")
        self.assertEqual(self.active(), active)

    def test_exact_xml_version_without_state_is_supported(self):
        self.directory("1.3.20")
        active = self.directory("1.3.29")
        self.xml("1.3.29", namespace="urn:fixture")
        self.assertEqual(self.active(), active)

    def test_known_inactive_handler_is_not_selected(self):
        self.directory("1.3.29", "Installed")
        self.xml("1.3.29")
        with self.assertRaisesRegex(RuntimeError, "Installed"):
            self.active()

    def test_missing_xml_target_does_not_select_another_directory(self):
        self.directory("1.3.20")
        self.xml("1.3.29")
        with self.assertRaisesRegex(RuntimeError, "missing RunCommand directory"):
            self.active()

    def test_only_newest_xml_is_used(self):
        self.directory("1.3.20")
        self.directory("1.3.29")
        old = self.xml("1.3.20", name="ExtensionsConfig.1.xml")
        new = self.agent / "ExtensionsConfig.2.xml"
        new.write_text("<Extensions><Plugins/></Extensions>")
        os.utime(old, (100, 100))
        os.utime(new, (200, 200))
        with self.assertRaisesRegex(RuntimeError, "Cannot unambiguously"):
            self.active()

    def test_ambiguity_reports_directories_and_states(self):
        self.directory("1.3.20")
        self.directory("1.3.29")
        with self.assertRaises(RuntimeError) as error:
            self.active()
        self.assertIn(self.handler + "-1.3.20", str(error.exception))
        self.assertIn(self.handler + "-1.3.29", str(error.exception))
        self.assertIn("state=missing", str(error.exception))

    def test_empty_installation_reports_missing_handler(self):
        with self.assertRaisesRegex(RuntimeError, "none"):
            self.active()

    def test_streaming_digest_matches_sha256(self):
        binary = self.agent / "binary"
        content = b"fixture" * 200000
        binary.write_bytes(content)
        self.assertEqual(
            self.namespace["digest"](binary), hashlib.sha256(content).hexdigest()
        )

    def test_verify_uses_enabled_directory_with_retained_versions(self):
        self.directory("1.3.20", "Installed")
        active = self.directory("1.3.29", "Enabled")
        self.namespace["marker"].write_text(json.dumps({
            "directory": str(active),
            "hashes": self.hashes,
        }))
        self.namespace["verify"]()

    def test_verify_rejects_changed_active_directory(self):
        old = self.directory("1.3.20", "Installed")
        self.directory("1.3.29", "Enabled")
        self.namespace["marker"].write_text(json.dumps({
            "directory": str(old), "hashes": {},
        }))
        with self.assertRaisesRegex(RuntimeError, "active RunCommand directory changed"):
            self.namespace["verify"]()

    def test_handler_names_match_repository_constants(self):
        constants = (Path(__file__).resolve().parents[1]
                     / "internal/constants/constants.go").read_text()
        names = re.findall(r'RunCommand(?:Test)?ExtensionName\s*=\s*"([^"]+)"', constants)
        self.assertEqual(set(self.namespace["handlers"]), set(names))
        self.assertEqual(len(names), 2)

    def test_edp_enabled_handler_without_production_directory(self):
        active = self.directory("1.3.29", "Enabled", handler=self.test_handler)
        self.assertEqual(self.active(), active)

    def test_edp_single_directory_without_state_or_xml(self):
        active = self.directory("1.3.29", handler=self.test_handler)
        self.assertEqual(self.active(), active)

    def test_edp_xml_selects_exact_version(self):
        self.directory("1.3.20", handler=self.test_handler)
        active = self.directory("1.3.29", handler=self.test_handler)
        self.xml("1.3.29", namespace="urn:fixture", handler=self.test_handler)
        self.assertEqual(self.active(), active)

    def test_edp_enabled_handler_ignores_inactive_production_version(self):
        self.directory("1.3.20", "Installed")
        active = self.directory("1.3.29", "Enabled", handler=self.test_handler)
        self.assertEqual(self.active(), active)

    def test_production_enabled_handler_ignores_inactive_edp_version(self):
        active = self.directory("1.3.29", "Enabled")
        self.directory("1.3.20", "Installed", handler=self.test_handler)
        self.assertEqual(self.active(), active)

    def test_enabled_production_and_edp_handlers_are_ambiguous(self):
        self.directory("1.3.29", "Enabled")
        self.directory("1.3.29", "Enabled", handler=self.test_handler)
        with self.assertRaisesRegex(RuntimeError, "multiple enabled"):
            self.active()

    def test_xml_with_both_handler_names_is_ambiguous_even_at_same_version(self):
        self.directory("1.3.29")
        self.directory("1.3.29", handler=self.test_handler)
        path = self.xml("1.3.29")
        tree = ET.parse(path)
        ET.SubElement(tree.getroot().find("Plugins"), "Plugin", {
            "name": self.test_handler, "version": "1.3.29",
        })
        tree.write(path)
        with self.assertRaisesRegex(RuntimeError, "ambiguous RunCommand handlers"):
            self.active()

    def test_edp_binary_hash_verification(self):
        active = self.directory("1.3.29", "Enabled", handler=self.test_handler)
        self.namespace["marker"].write_text(json.dumps({
            "directory": str(active),
            "hashes": self.hashes,
        }))
        self.namespace["verify"]()

    def test_verify_rejects_missing_or_incomplete_local_hashes(self):
        for hashes in (None, {}, {"run-command-handler": self.hashes["run-command-handler"]}):
            with self.subTest(hashes=hashes):
                self.namespace["config"]["hashes"] = hashes
                with self.assertRaisesRegex(RuntimeError, "both locally built"):
                    self.namespace["verify"]()

    def test_verify_rejects_stale_binary_even_when_marker_matches_local_build(self):
        active = self.directory("1.3.29", "Enabled")
        self.namespace["marker"].write_text(json.dumps({
            "directory": str(active), "hashes": self.hashes,
        }))
        for name in self.hashes:
            with self.subTest(binary=name):
                binary = active / "bin" / name
                binary.write_bytes(b"original published binary")
                with self.assertRaisesRegex(RuntimeError, "binary changed: " + name):
                    self.namespace["verify"]()
                binary.write_bytes(b"fixture")

    def test_verify_rejects_self_consistent_stale_marker_and_binaries(self):
        active = self.directory("1.3.29", "Enabled")
        stale = {}
        for name in self.hashes:
            (active / "bin" / name).write_bytes(b"previous build")
            stale[name] = hashlib.sha256(b"previous build").hexdigest()
        self.namespace["marker"].write_text(json.dumps({
            "directory": str(active), "hashes": stale,
        }))
        with self.assertRaisesRegex(RuntimeError, "marker does not match the locally built"):
            self.namespace["verify"]()

    def test_service_verification_checks_running_executable_against_local_build(self):
        active = self.directory("1.3.29", "Enabled")
        self.namespace["marker"].write_text(json.dumps({
            "directory": str(active), "hashes": self.hashes,
        }))
        self.namespace["config"]["requireService"] = True
        executable = self.agent / "proc/123/exe"
        executable.parent.mkdir(parents=True)
        system = mock.Mock()
        system.check_output.return_value = "123\n"
        with mock.patch.dict(self.namespace, {
            "subprocess": system, "service": "managedruncommand.service",
            "Path": lambda path: self.agent / "proc" if path == "/proc" else Path(path),
        }):
            executable.write_bytes(b"fixture")
            self.namespace["verify"]()
            system.run.assert_called_once_with(
                ["systemctl", "is-active", "--quiet", "managedruncommand.service"], check=True
            )
            executable.write_bytes(b"previous running service build")
            with self.assertRaisesRegex(RuntimeError, "running RunCommand service is not the buddy-build"):
                self.namespace["verify"]()

    def test_unknown_handler_name_is_reported_but_not_selected(self):
        unknown = self.directory("1.3.29", "Enabled", handler="Unknown.RunCommandHandlerLinux")
        with self.assertRaises(RuntimeError) as error:
            self.active()
        self.assertIn(unknown.name, str(error.exception))
        self.assertIn(self.test_handler, str(error.exception))

    def test_diagnostics_include_production_and_edp_paths(self):
        logs = self.namespace["logs"]
        logs.mkdir()
        expected = []
        for handler in (self.handler, self.test_handler):
            directory = self.directory("1.3.29", handler=handler)
            status = directory / "status"
            status.mkdir()
            log = logs / handler
            log.mkdir()
            expected.extend((status, log))
        journal = self.agent / "journal.txt"
        paths = self.namespace["diagnostic_paths"](journal)
        self.assertIn(journal, paths)
        for path in expected:
            self.assertIn(path, paths)

    def test_archive_contains_versioned_unversioned_and_multiconfig_execution_logs(self):
        logs = self.namespace["logs"]
        expected = []
        for handler in (self.handler, self.test_handler):
            directory = self.directory("1.3.29", handler=handler)
            status = directory / "status"
            status.mkdir()
            for name in ("immediate-inline.0.status", "immediateGoalStatesInTerminalStatusFile.status"):
                path = status / name
                path.write_bytes(b"fixture diagnostic output")
                expected.append(path)
            for suffix in ("", "-1.3.29"):
                directory = logs / (handler + suffix)
                directory.mkdir(parents=True)
                for name in ("CommandExecution.log", "CommandExecution_immediate-inline.log"):
                    path = directory / name
                    path.write_text("fixture diagnostic output")
                    expected.append(path)
        journal = self.agent / "journal.txt"
        journal.write_text("fixture service journal")
        archive = self.agent / "logs.tar.gz"
        self.namespace["collect_logs"](archive, journal)
        with tarfile.open(archive, "r:gz") as bundle:
            for path in expected:
                member = next(
                    entry for entry in bundle.getmembers()
                    if entry.name.endswith("/" + path.parent.name + "/" + path.name)
                )
                self.assertEqual(bundle.extractfile(member).read(), b"fixture diagnostic output")

    def test_collection_does_not_report_success_without_execution_logs(self):
        with self.assertRaisesRegex(RuntimeError, "No RunCommand CommandExecution"):
            self.namespace["collect_logs"](self.agent / "logs.tar.gz", self.agent / "journal.txt")


if __name__ == "__main__":
    unittest.main()
