"""Validate an anonymous session probe without logging its response body."""

import json
import sys


def valid_response(mode, status, raw):
    try:
        payload = json.loads(raw)
    except (ValueError, TypeError):
        return False
    if not isinstance(payload, dict) or payload.get("authenticated") is not False:
        return False
    if mode == "true":
        return status == "200" and payload.get("demoMode") is True
    if mode == "false":
        return status == "401" and payload.get("demoMode", False) is False
    return False


if __name__ == "__main__":
    if len(sys.argv) != 3 or not valid_response(sys.argv[1], sys.argv[2], sys.stdin.read(16385)):
        print("Anonymous authentication mode check failed", file=sys.stderr)
        sys.exit(1)
