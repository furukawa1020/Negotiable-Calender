"""Read-only, same-platform timer watchdog. No calendar data or cloud identity."""
from datetime import datetime, timezone
import json
import os
import re
import sys
import urllib.request

REPOSITORY = "furukawa1020/Negotiable-Calender"
WORKFLOW_PATH = ".github/workflows/calendar-sync.yml"
ENDPOINT = "https://api.github.com/repos/" + REPOSITORY + "/actions/workflows/calendar-sync.yml"
RUNS_ENDPOINT = "https://api.github.com/repos/" + REPOSITORY + "/actions/runs/"
MAX_BYTES = 1024 * 1024
MAX_RUNS = 20
MAX_AGE_SECONDS = 90 * 60


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result


def fetch(url, token, opener):
    # URLs are selected here, never from response links or workflow inputs.
    if url not in (ENDPOINT, ENDPOINT + "/runs?branch=main&event=schedule&per_page=20") and not re.fullmatch(re.escape(RUNS_ENDPOINT) + r"[0-9]{1,20}/jobs\?per_page=10", url):
        raise ValueError("unexpected endpoint")
    request = urllib.request.Request(url, headers={
        "Authorization": "Bearer " + token,
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
        "User-Agent": "negotiable-calendar-sync-watchdog",
    })
    with opener.open(request, timeout=10) as response:
        if response.status != 200:
            raise ValueError("unexpected status")
        raw = response.read(MAX_BYTES + 1)
    if len(raw) > MAX_BYTES:
        raise ValueError("oversized response")
    return json.loads(raw, object_pairs_hook=unique_object)


def evaluate(workflow, payload, now):
    if not isinstance(workflow, dict) or workflow.get("path") != WORKFLOW_PATH or type(workflow.get("id")) is not int or workflow["id"] <= 0:
        raise ValueError("invalid workflow")
    if workflow.get("state") != "active":
        return False, {"status": "workflow_inactive"}
    if not isinstance(payload, dict) or not isinstance(payload.get("workflow_runs"), list) or len(payload["workflow_runs"]) > MAX_RUNS:
        raise ValueError("invalid runs")
    ages = []
    for run in payload["workflow_runs"]:
        if not isinstance(run, dict) or type(run.get("workflow_id")) is not int or run["workflow_id"] != workflow["id"]:
            raise ValueError("wrong workflow run")
        if run.get("event") != "schedule" or run.get("head_branch") != "main":
            continue
        if run.get("status") != "completed" or run.get("conclusion") != "success":
            continue
        if type(run.get("id")) is not int or not 0 < run["id"] < 10**20:
            raise ValueError("invalid run identity")
        created = datetime.strptime(run["created_at"], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)
        age = (now - created).total_seconds()
        if age < 0:
            raise ValueError("future run")
        ages.append(age)
    if not ages:
        return False, {"status": "no_scheduled_success"}
    age = min(ages)
    healthy = age <= MAX_AGE_SECONDS
    return healthy, {"status": "healthy" if healthy else "scheduled_success_stale", "age_seconds": int(age), "max_age_seconds": MAX_AGE_SECONDS}


def batch_step_succeeded(payload, run_id):
    if not isinstance(payload, dict) or not isinstance(payload.get("jobs"), list) or len(payload["jobs"]) > 10:
        return False
    matches = [job for job in payload["jobs"] if isinstance(job, dict) and job.get("name") == "sync"]
    if len(matches) != 1:
        return False
    job = matches[0]
    if job.get("run_id") != run_id or job.get("status") != "completed" or job.get("conclusion") != "success" or not isinstance(job.get("steps"), list):
        return False
    steps = [step for step in job["steps"] if isinstance(step, dict) and step.get("name") == "Run one bounded synchronous batch"]
    return len(steps) == 1 and steps[0].get("status") == "completed" and steps[0].get("conclusion") == "success"


def main(token, output, errors, opener=None, now=None):
    try:
        if not token:
            raise ValueError("missing token")
        opener = opener or urllib.request.build_opener(NoRedirect())
        workflow = fetch(ENDPOINT, token, opener)
        runs = fetch(ENDPOINT + "/runs?branch=main&event=schedule&per_page=20", token, opener)
        healthy, summary = evaluate(workflow, runs, now or datetime.now(timezone.utc))
        if healthy:
            candidate = max((run for run in runs["workflow_runs"] if run.get("event") == "schedule" and run.get("head_branch") == "main" and run.get("status") == "completed" and run.get("conclusion") == "success"), key=lambda run: run["created_at"])
            jobs = fetch(RUNS_ENDPOINT + str(candidate["id"]) + "/jobs?per_page=10", token, opener)
            if not batch_step_succeeded(jobs, candidate["id"]):
                healthy, summary = False, {"status": "batch_step_not_successful"}
    except Exception:
        print("::error::Sync watchdog check failed; API response and credentials omitted.", file=errors)
        return 1
    print(json.dumps(summary, sort_keys=True), file=output)
    if not healthy:
        print("::error::Scheduled calendar sync heartbeat is unhealthy; inspect the sync workflow.", file=errors)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(os.environ.get("GH_TOKEN", ""), sys.stdout, sys.stderr))
