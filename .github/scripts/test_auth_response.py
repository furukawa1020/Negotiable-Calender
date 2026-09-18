import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("check_auth", Path(__file__).with_name("check-auth-response.py"))
check_auth = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check_auth)


class AuthResponseTests(unittest.TestCase):
    def test_expected_contracts(self):
        for mode, status, body in [
            ("true", "200", '{"authenticated":false,"demoMode":true}'),
            ("false", "401", '{"authenticated":false}'),
            ("false", "401", '{"authenticated":false,"demoMode":false}'),
        ]:
            with self.subTest(mode=mode, body=body):
                self.assertTrue(check_auth.valid_response(mode, status, body))

    def test_mismatch_or_unsafe_responses(self):
        for mode, status, body in [
            ("false", "200", '{"authenticated":false,"demoMode":true}'),
            ("false", "200", '{"authenticated":false,"demoMode":false}'),
            ("false", "401", '{"authenticated":false,"demoMode":true}'),
            ("true", "401", '{"authenticated":false}'),
            ("true", "200", '{"authenticated":false}'),
            ("true", "200", '{"authenticated":false,"demoMode":false}'),
            ("false", "500", '{"authenticated":false}'),
            ("false", "401", '{"authenticated":true}'),
            ("false", "401", '{"authenticated":0}'),
            ("false", "401", '{"authenticated":false,"demoMode":null}'),
            ("false", "401", '{"authenticated":false,"demoMode":0}'),
            ("false", "401", '{}'),
            ("false", "401", 'null'),
            ("false", "401", '[]'),
            ("false", "401", '<html>error</html>'),
            ("false", "401", '{"authenticated":false} {}'),
            ("FALSE", "401", '{"authenticated":false}'),
        ]:
            with self.subTest(mode=mode, status=status, body=body):
                self.assertFalse(check_auth.valid_response(mode, status, body))


if __name__ == "__main__":
    unittest.main()
