"""Read-only release probe; verify static bytes, headers and private-path isolation."""
import hashlib
from pathlib import Path
import sys
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
ORIGIN = "https://negotiable-calendar-480760.web.app"


def check():
    for path, file in [("/", "index.html"), ("/privacy.html", "privacy.html"),
                       ("/terms.html", "terms.html"), ("/styles.css", "styles.css"),
                       ("/.env", None), ("/docs/public-launch-draft.md", None),
                       ("/api/v1/auth/session", None),
                       ("/client_secret_480760664246-in718o35v54ug98edk6nmue6mhv8nl3q.apps.googleusercontent.com.json", None)]:
        try:
            response = urllib.request.urlopen(ORIGIN + path, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            assert response.code == (200 if file else 404)
            for name, value in [("X-Content-Type-Options", "nosniff"),
                                ("X-Frame-Options", "DENY"), ("Referrer-Policy", "no-referrer")]:
                assert response.headers.get(name) == value
            assert "default-src 'none'" in response.headers.get("Content-Security-Policy", "")
            if file:
                actual = hashlib.sha256(response.read()).digest()
                expected = hashlib.sha256((ROOT / "hosting/public" / file).read_bytes()).digest()
                assert actual == expected
        print(f"{path}: verified")


if __name__ == "__main__":
    try:
        check()
    except Exception:
        print("Public policy release verification failed; no response bodies were printed.", file=sys.stderr)
        sys.exit(1)
    print("Public policies match release files; protected paths stay unavailable.")
