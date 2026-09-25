from datetime import datetime, timedelta, timezone
import importlib.util
import io
import json
from pathlib import Path
import unittest
import urllib.error

spec = importlib.util.spec_from_file_location("watchdog", Path(__file__).with_name("check-sync-watchdog.py"))
watchdog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(watchdog)
NOW = datetime(2026, 9, 26, tzinfo=timezone.utc)
WORKFLOW = {"id": 123, "path": watchdog.WORKFLOW_PATH, "state": "active"}


def run(age=60, **changes):
    value = {"id": 901, "workflow_id": 123, "event": "schedule", "head_branch": "main", "status": "completed", "conclusion": "success", "created_at": (NOW - timedelta(seconds=age)).strftime("%Y-%m-%dT%H:%M:%SZ")}
    value.update(changes)
    return value


def jobs(conclusion="success", step_conclusion="success"):
    return {"jobs": [{"run_id": 901, "name": "sync", "status": "completed", "conclusion": conclusion, "steps": [{"name": "Run one bounded synchronous batch", "status": "completed", "conclusion": step_conclusion}]}]}


class Response(io.BytesIO):
    status = 200
    requested = None

    def read(self, size=-1):
        self.requested = size
        return super().read(size)


class Opener:
    def __init__(self, values):
        self.responses = [Response(json.dumps(v).encode()) for v in values]
        self.calls = []

    def open(self, request, timeout):
        if request.get_method() != "GET":
            raise AssertionError("write request")
        self.calls.append((request.full_url, timeout))
        return self.responses[len(self.calls) - 1]


class WatchdogTests(unittest.TestCase):
    def evaluate(self, runs, workflow=WORKFLOW):
        return watchdog.evaluate(workflow, {"workflow_runs": runs}, NOW)

    def test_success_boundary_and_staleness(self):
        for age, healthy in ((0, True), (5399, True), (5400, True), (5401, False), (86400, False)):
            with self.subTest(age=age):
                value, summary = self.evaluate([run(age)])
                self.assertEqual(value, healthy)
                self.assertEqual(summary["age_seconds"], age)
        self.assertTrue(self.evaluate([run(8000), run(60)])[0])

    def test_manual_and_rerun_do_not_mask_stopped_timer(self):
        for ignored in (run(event="workflow_dispatch"), run(head_branch="feature"), run(status="in_progress"), run(conclusion="failure"), run(status="queued"), run(conclusion="skipped")):
            self.assertFalse(self.evaluate([ignored])[0])
            self.assertFalse(self.evaluate([run(8000), ignored])[0])
        # Recent updated_at / rerun attempt does not freshen old scheduled creation.
        self.assertFalse(self.evaluate([run(8000, run_attempt=2, updated_at=NOW.isoformat())])[0])
        self.assertFalse(self.evaluate([])[0])

    def test_disabled_workflow_and_identity(self):
        for state in ("disabled_inactivity", "disabled_manually", "deleted", None):
            self.assertEqual(self.evaluate([run()], {**WORKFLOW, "state": state}), (False, {"status": "workflow_inactive"}))
        for workflow in (None, {}, {**WORKFLOW, "id": True}, {**WORKFLOW, "path": "other"}):
            with self.assertRaises(ValueError):
                self.evaluate([run()], workflow)
        for values in ([run(workflow_id=456)], [run(workflow_id=True)], [None], [run()] * 21, [run(-1)], [run(created_at="private-invalid-timestamp")]):
            with self.assertRaises((ValueError, TypeError)):
                self.evaluate(values)

    def test_fetch_bounds_safe_summary_and_no_writes(self):
        private = "private-user private-token https://private.example"
        opener = Opener([{**WORKFLOW, "unknown": private}, {"workflow_runs": [run(actor=private, display_title=private)]}, jobs()])
        out, err = io.StringIO(), io.StringIO()
        self.assertEqual(watchdog.main(private, out, err, opener, NOW), 0)
        self.assertNotIn(private, out.getvalue() + err.getvalue())
        self.assertEqual(opener.calls, [(watchdog.ENDPOINT, 10), (watchdog.ENDPOINT + "/runs?branch=main&event=schedule&per_page=20", 10), (watchdog.RUNS_ENDPOINT + "901/jobs?per_page=10", 10)])
        self.assertTrue(all(r.requested == watchdog.MAX_BYTES + 1 for r in opener.responses))

    def test_failures_never_echo_response_or_exception(self):
        private = "private-token ::error::private-user"
        for raw in (private.encode(), b'{} {}', b'{"id":123,"id":456}', b' ' * (watchdog.MAX_BYTES + 1), b'\xff', b'[' * 2000):
            opener = Opener([])
            opener.responses = [Response(raw)]
            out, err = io.StringIO(), io.StringIO()
            self.assertEqual(watchdog.main(private, out, err, opener, NOW), 1)
            self.assertEqual(out.getvalue(), "")
            self.assertNotIn(private, err.getvalue())
        class FailedOpener:
            def open(self, *args, **kwargs):
                raise urllib.error.URLError(private)
        out, err = io.StringIO(), io.StringIO()
        self.assertEqual(watchdog.main(private, out, err, FailedOpener(), NOW), 1)
        self.assertNotIn(private, out.getvalue() + err.getvalue())
        self.assertEqual(watchdog.main("", out, err, FailedOpener(), NOW), 1)

    def test_unhealthy_summary_fails_and_redirect_is_refused(self):
        out, err = io.StringIO(), io.StringIO()
        opener = Opener([WORKFLOW, {"workflow_runs": [run(5401)]}])
        self.assertEqual(watchdog.main("token", out, err, opener, NOW), 1)
        self.assertEqual(json.loads(out.getvalue())["status"], "scheduled_success_stale")
        self.assertIsNone(watchdog.NoRedirect().redirect_request(None, None, 302, "", {}, "https://private.example"))
        with self.assertRaises(ValueError):
            watchdog.fetch("https://private.example", "token", opener)

    def test_successful_run_with_skipped_or_missing_batch_is_not_healthy(self):
        for payload in ({}, {"jobs": []}, jobs("skipped"), jobs(step_conclusion="skipped"), jobs(step_conclusion="failure")):
            opener = Opener([WORKFLOW, {"workflow_runs": [run()]}, payload])
            out, err = io.StringIO(), io.StringIO()
            self.assertEqual(watchdog.main("token", out, err, opener, NOW), 1)
            self.assertEqual(json.loads(out.getvalue())["status"], "batch_step_not_successful")


if __name__ == "__main__":
    unittest.main()
