import importlib.util
from pathlib import Path
import unittest
import urllib.parse

spec = importlib.util.spec_from_file_location("login_redirect", Path(__file__).with_name("check-login-redirect.py"))
login = importlib.util.module_from_spec(spec)
spec.loader.exec_module(login)


class LoginRedirectTests(unittest.TestCase):
    origin = "https://app.example"

    def location(self, **changes):
        values = {
            "client_id": "synthetic-client", "redirect_uri": self.origin + "/api/v1/auth/google/callback",
            "response_type": "code", "scope": "openid profile email", "state": "s" * 43,
            "code_challenge": "c" * 43, "code_challenge_method": "S256", "include_granted_scopes": "false",
        }
        values.update(changes)
        return "https://accounts.google.com/o/oauth2/v2/auth?" + urllib.parse.urlencode(values)

    def test_identity_only(self):
        self.assertTrue(login.valid_location(self.origin, 302, self.location()))
        self.assertTrue(login.valid_location(self.origin, 302, self.location(scope="email openid profile", access_type="online")))

    def test_broader_or_unbound_authorization_is_rejected(self):
        for changes in [
            {"scope": "openid profile email https://www.googleapis.com/auth/calendar.readonly"},
            {"scope": "openid profile email email"}, {"include_granted_scopes": "true"},
            {"include_granted_scopes": ""}, {"access_type": "offline"}, {"state": ""},
            {"code_challenge": ""}, {"code_challenge_method": "plain"}, {"client_id": ""},
            {"response_type": "token"}, {"redirect_uri": "https://other.example/callback"},
        ]:
            with self.subTest(changes=changes):
                self.assertFalse(login.valid_location(self.origin, 302, self.location(**changes)))

    def test_duplicate_fields_and_wrong_targets_are_rejected(self):
        for location in [
            self.location() + "&scope=openid", self.location() + "&state=second",
            self.location() + "&include_granted_scopes=true", self.location() + "#fragment",
            self.location().replace("accounts.google.com", "accounts.google.com.evil.example"),
            self.location().replace("https://accounts", "http://accounts"),
            self.location().replace("accounts.google.com", "user:password@accounts.google.com"),
            "/local/redirect", "not a URL",
        ]:
            with self.subTest(location=location):
                self.assertFalse(login.valid_location(self.origin, 302, location))
        self.assertFalse(login.valid_location(self.origin, 200, self.location()))

    def test_probe_redirect_handler_never_follows_google(self):
        self.assertIsNone(login.NoRedirect().redirect_request(None, None, 302, "", {}, self.location()))


if __name__ == "__main__":
    unittest.main()
