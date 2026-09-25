import importlib.util
import io
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("sync_result", Path(__file__).with_name("check-sync-result.py"))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


def response(**changes):
    value = {"status": "completed", "configured": True, "result": dict.fromkeys(checker.COUNTS, 0)}
    value["result"]["capacityReached"] = False
    value.update(changes)
    return value


class SyncResultTests(unittest.TestCase):
    def run_receipt(self, raw):
        out, err = io.StringIO(), io.StringIO()
        code = checker.main(io.BytesIO(raw), out, err)
        return code, out.getvalue(), err.getvalue()

    def test_empty_and_processed_batches(self):
        for count in (0, 1, 5, 20):
            value = response()
            value["result"].update(claimed=count, attempted=count, succeeded=count, capacityReached=count == 5)
            code, out, err = self.run_receipt(json.dumps(value).encode())
            self.assertEqual(code, 0)
            self.assertEqual(json.loads(out), value)
            self.assertEqual("::warning::" in err, count == 5)

    def test_unhealthy_batches_fail(self):
        values = [response(status="not_configured", configured=False), response(status="budget_exhausted"), response(status="claim_failed")]
        failed = response()
        failed["result"].update(claimed=1, attempted=1, failed=1)
        values.append(failed)
        partial = response()
        partial["result"].update(claimed=2, attempted=1, succeeded=1, unprocessed=1)
        values.append(partial)
        for value in values:
            with self.subTest(value=value):
                code, out, err = self.run_receipt(json.dumps(value).encode())
                self.assertEqual(code, 1)
                self.assertEqual(json.loads(out), value)
                self.assertIn("::error::", err)

    def test_invalid_contracts(self):
        values = [None, [], {}, response(status="unknown-private-error"), response(configured=1), response(configured=False), response(result=[])]
        for key in checker.COUNTS:
            for bad in (-1, 21, True, 0.0, "0", None):
                value = response()
                value["result"][key] = bad
                values.append(value)
            value = response()
            del value["result"][key]
            values.append(value)
        for key, bad in (("claimed", 1), ("attempted", 1), ("capacityReached", 0), ("capacityReached", True)):
            value = response()
            value["result"][key] = bad
            values.append(value)
        for value in values:
            with self.subTest(value=value):
                code, out, err = self.run_receipt(json.dumps(value).encode())
                self.assertEqual(code, 1)
                self.assertEqual(out, "")
                self.assertIn("response body omitted", err)

    def test_malformed_duplicate_oversized_and_private_data(self):
        private = "private-user secret-token https://provider.example/private"
        valid = response()
        valid["unknown"] = private
        valid["result"]["providerError"] = private
        code, out, err = self.run_receipt(json.dumps(valid).encode())
        self.assertEqual(code, 0)
        self.assertNotIn(private, out + err)
        duplicate = json.dumps(response()).replace('"configured": true', '"configured": false, "configured": true').encode()
        self.assertEqual(self.run_receipt(duplicate)[0], 1)
        for raw in (private.encode(), b"{} {}", b'{' + b'"status":"completed","status":"completed"}', b'"' + b'x' * checker.MAX_BYTES + b'"', b'\xff', b'[' * 2000):
            code, out, err = self.run_receipt(raw)
            self.assertEqual(code, 1)
            self.assertEqual(out, "")
            self.assertNotIn(private, err)

    def test_read_is_bounded(self):
        class BoundedStream:
            requested = None
            def read(self, size):
                self.requested = size
                return b" " * size
        stream = BoundedStream()
        self.assertEqual(checker.main(stream, io.StringIO(), io.StringIO()), 1)
        self.assertEqual(stream.requested, checker.MAX_BYTES + 1)


if __name__ == "__main__":
    unittest.main()
