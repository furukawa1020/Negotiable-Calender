"""Bounded, fail-closed scheduled-sync receipt validation; never echo raw bodies."""
import json
import sys

MAX_BYTES = 16384
COUNTS = ("claimed", "attempted", "succeeded", "failed", "unprocessed")
STATES = ("completed", "budget_exhausted", "claim_failed", "not_configured")


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result


def receipt(raw):
    if len(raw) > MAX_BYTES:
        raise ValueError("oversized response")
    value = json.loads(raw, object_pairs_hook=unique_object)
    if not isinstance(value, dict):
        raise ValueError("object required")
    state, configured, result = value.get("status"), value.get("configured"), value.get("result")
    if state not in STATES or type(configured) is not bool or not isinstance(result, dict):
        raise ValueError("invalid status")
    counts = {key: result.get(key) for key in COUNTS}
    if any(type(count) is not int or not 0 <= count <= 20 for count in counts.values()):
        raise ValueError("invalid counts")
    capacity = result.get("capacityReached")
    if type(capacity) is not bool or (capacity and counts["claimed"] == 0):
        raise ValueError("invalid capacity")
    if counts["attempted"] != counts["succeeded"] + counts["failed"] or counts["claimed"] != counts["attempted"] + counts["unprocessed"]:
        raise ValueError("inconsistent counts")
    if state == "not_configured":
        if configured or any(counts.values()) or capacity:
            raise ValueError("inconsistent configuration")
    elif not configured:
        raise ValueError("inconsistent configuration")
    # Explicit allowlist: unknown nested fields may contain account/provider data.
    summary = {"status": state, "configured": configured, "result": {**counts, "capacityReached": capacity}}
    healthy = configured and state == "completed" and counts["failed"] == 0 and counts["unprocessed"] == 0
    return summary, healthy


def main(stream, output, errors):
    try:
        summary, healthy = receipt(stream.read(MAX_BYTES + 1))
    except Exception:
        print("::error::Invalid calendar sync receipt; response body omitted.", file=errors)
        return 1
    print(json.dumps(summary, sort_keys=True), file=output)
    if not healthy:
        print("::error::Calendar sync is not healthy; check configuration or the sanitized batch status.", file=errors)
        return 1
    if summary["result"]["capacityReached"]:
        print("::warning::Batch capacity reached; more work may wait until the next invocation.", file=errors)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.stdin.buffer, sys.stdout, sys.stderr))
