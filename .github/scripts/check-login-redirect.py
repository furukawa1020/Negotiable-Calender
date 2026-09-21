"""Probe only the app's login redirect; never follow Google or print OAuth values."""

import re
import sys
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def valid_location(origin, status, location):
    try:
        app = urllib.parse.urlsplit(origin)
        if (app.scheme != "https" or not app.hostname or app.username or app.password
                or app.query or app.fragment or app.path not in ("", "/")):
            return False
        target = urllib.parse.urlsplit(location)
        if (status != 302 or target.scheme != "https" or target.hostname != "accounts.google.com"
                or target.port not in (None, 443) or target.username or target.password
                or target.fragment or target.path != "/o/oauth2/v2/auth"):
            return False
        query = urllib.parse.parse_qs(target.query, keep_blank_values=True)
        required = ("client_id", "redirect_uri", "response_type", "scope", "state",
                    "code_challenge", "code_challenge_method", "include_granted_scopes")
        if any(len(query.get(key, [])) != 1 for key in required):
            return False
        scopes = query["scope"][0].split()
        return (
            bool(query["client_id"][0])
            and query["redirect_uri"] == [origin.rstrip("/") + "/api/v1/auth/google/callback"]
            and query["response_type"] == ["code"]
            and sorted(scopes) == ["email", "openid", "profile"]
            and query["include_granted_scopes"] == ["false"]
            and query.get("access_type", ["online"]) == ["online"]
            and query["code_challenge_method"] == ["S256"]
            and re.fullmatch(r"[A-Za-z0-9_-]{43}", query["code_challenge"][0]) is not None
            and re.fullmatch(r"[A-Za-z0-9_-]{43}", query["state"][0]) is not None
        )
    except (TypeError, ValueError):
        return False


def probe(origin):
    opener = urllib.request.build_opener(NoRedirect)
    try:
        response = opener.open(origin.rstrip("/") + "/api/v1/auth/google/login", timeout=30)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return valid_location(origin, response.code, response.headers.get("Location", ""))


if __name__ == "__main__":
    try:
        valid = len(sys.argv) == 2 and probe(sys.argv[1])
    except Exception:
        # HTTP/client exceptions may contain sensitive redirects; never print them.
        valid = False
    if not valid:
        print("Identity-only login redirect check failed", file=sys.stderr)
        sys.exit(1)
    print("Identity-only login redirect verified; Google consent was not followed.")
