"""Live Mist API acceptance: real CPU/CUDA/TT jobs, queues, cancellation, logs.

The API submits every workload. An admin kubectl connection is used only to
inspect reservations and release the training-test synchronization barriers.
No accelerator can fall back to CPU. Jobs and checkpoints are retained.
"""

import argparse
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://127.0.0.1:3000")
    parser.add_argument("--artifacts", type=Path, required=True)
    args = parser.parse_args()
    args.artifacts.mkdir(parents=True, exist_ok=True)
    run = datetime.now(timezone.utc).strftime("api-%m%d-%H%M%S")
    evidence = {"run": run, "checks": {}, "jobs": {}}

    def api(method, path, body=None, expected=200):
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(args.url + path, data=data, method=method,
                                         headers={"Content-Type": "application/json"})
        try:
            response = urllib.request.urlopen(request, timeout=45)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            result = json.load(response)
            assert response.code == expected, (path, response.code, result)
            return result

    def kubectl(*options):
        return subprocess.check_output(["k3s", "kubectl", *options], text=True, timeout=45)

    def wait(check, description, timeout=240):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            value = check()
            if value:
                return value
            time.sleep(1)
        raise AssertionError("Timed out: " + description)

    def submit(label, **request):
        result = api("POST", "/jobs", {"name": run + "-" + label, **request}, expected=201)
        evidence["jobs"][label] = result["job_id"]
        print(f"Submitted {label}: {result['job_id']}", flush=True)
        return result["job_id"]

    def status(job):
        return api("GET", "/jobs/" + job)

    def logs(job):
        return api("GET", "/jobs/" + job + "/logs")["logs"]

    def finished(job, desired="Success", timeout=240):
        def check():
            value = status(job)
            if value["job_state"] in ["Success", "Failure", "Cancelled"]:
                assert value["job_state"] == desired, value
                return value
        value = wait(check, job + " complete", timeout)
        text = logs(job)
        (args.artifacts / (job + ".log")).write_text(text)
        evidence["checks"][job] = {"status": value, "logs": text}
        return value, text

    def healthy():
        try:
            return api("GET", "/healthz")["executor"] == "kubernetes"
        except urllib.error.URLError:
            return False
    wait(healthy, "API reachable after deployment", timeout=60)
    before = api("GET", "/jobs")["count"]
    for request in [
        {"command": ["true"], "accelerator": "nvidia", "device_count": 3},
        {"command": ["true"], "accelerator": "tenstorrent", "device_count": 5},
        {"command": ["true"], "privileged": True},
        {"command": ["true"], "cpu": "9"},
        {"command": ["true"], "memory": "1Ti"},
        {"command": ["true"], "image": "unapproved/image:latest"},
        {"script": "print(1)", "script_name": "../escape.py"},
    ]:
        api("POST", "/jobs", request, expected=400)
    assert api("GET", "/jobs")["count"] == before
    api("GET", "/jobs/training-tenstorrent", expected=404)
    api("POST", "/jobs/training-tenstorrent/cancel", expected=404)
    evidence["validation_rejected_without_jobs"] = True

    cpu = submit("cpu", accelerator="cpu", script="""import os,pathlib
p=pathlib.Path(os.environ['MIST_CHECKPOINT_DIR']); p.mkdir(parents=True,exist_ok=True)
(p/'output.txt').write_text('persistent CPU output')
print('CPU_API_PASSED',flush=True)
""")
    value, text = finished(cpu)
    assert value["exit_code"] == 0 and "CPU_API_PASSED" in text
    failed = submit("failure", accelerator="cpu", script="import sys; print('EXPECTED_FAILURE',flush=True); sys.exit(17)")
    value, text = finished(failed, "Failure")
    assert value["exit_code"] == 17 and "EXPECTED_FAILURE" in text
    cancelled = submit("cancel", accelerator="cpu", script="import time; print('CANCEL_LOG_PRESERVED',flush=True); time.sleep(120)")
    wait(lambda: "CANCEL_LOG_PRESERVED" in logs(cancelled), "cancel workload started")
    assert api("POST", "/jobs/" + cancelled + "/cancel")["job_state"] == "Cancelled"
    assert api("POST", "/jobs/" + cancelled + "/cancel")["job_state"] == "Cancelled"
    def stopped():
        pods = json.loads(kubectl("-n", "mist", "get", "pods", "-l", "batch.kubernetes.io/job-name=" + cancelled, "-o", "json"))["items"]
        return not pods or all(p["status"]["phase"] not in ["Running", "Pending"] for p in pods)
    wait(stopped, "cancelled pod stopped", timeout=45)
    value, text = finished(cancelled, "Cancelled")
    assert "CANCEL_LOG_PRESERVED" in text
    deadline = submit("deadline", accelerator="cpu", command=["python", "-c", "import time; time.sleep(60)"], timeout_seconds=10)
    wait(lambda: "DeadlineExceeded" in status(deadline).get("message", ""), "deadline reported by controller", timeout=60)
    value, _ = finished(deadline, "Failure", timeout=60)
    assert "DeadlineExceeded" in value["message"], value
    print("CPU execution, failure, cancellation, retained logs, and deadline passed", flush=True)

    def training_group(accelerator, parallel):
        # Each workload now mounts only its own output directory. Release each
        # initialized pod's barrier individually, rather than sharing a PVC root.
        barrier = "/outputs/start"
        group = [submit(f"{accelerator}-{i}", type="training-smoke", accelerator=accelerator,
                        device_count=1, args=["--steps", "500", "--start-file", barrier]) for i in range(parallel)]
        for job in group:
            wait(lambda: "BARRIER_READY" in logs(job), job + " initialized")
        snapshots = [status(job) for job in group]
        assert all(s["job_state"] == "InProgress" and s["device_count"] == 1 for s in snapshots)
        allocations = []
        for snapshot in snapshots:
            allocation = json.loads(kubectl("-n", "mist", "exec", snapshot["pod"], "--", "cat", snapshot["checkpoint_directory"] + "/allocation.json"))
            assert len(allocation["device_nodes"]) == 1
            allocations.append(allocation)
        assert len({a["device_nodes"][0] for a in allocations}) == parallel, allocations
        if accelerator == "tenstorrent":
            devices = [s["devices"][0]["device"] for s in snapshots]
            assert len(set(devices)) == 4
        fifth = submit(accelerator + "-queued", type="training-smoke", accelerator=accelerator,
                       device_count=1, args=["--steps", "500"])
        def queued():
            snapshot = status(fifth)
            if snapshot["job_state"] == "Scheduled" and snapshot.get("message"):
                assert not snapshot.get("node"), snapshot
                reason = "cannot allocate" if accelerator == "tenstorrent" else "Insufficient nvidia.com/gpu"
                assert reason in snapshot["message"], snapshot
                return snapshot
        queue = wait(queued, accelerator + " job queued", timeout=60)
        for snapshot in snapshots:
            kubectl("-n", "mist", "exec", snapshot["pod"], "--", "touch", barrier)
        for job in group + [fifth]:
            snapshot, text = finished(job)
            chips = 2 if accelerator == "tenstorrent" else 1
            assert snapshot["exit_code"] == 0 and f"TRAINING_PASSED backend={accelerator} devices={chips}" in text
        evidence[accelerator + "_concurrency"] = {"initial_status": snapshots, "allocations": allocations,
             "queued_status": queue, "all_training_passed": True}
        print(f"{accelerator}: {parallel} distinct reservations trained; queued job automatically reused hardware", flush=True)

    training_group("nvidia", 2)
    training_group("tenstorrent", 4)
    for accelerator, count, chips in [("nvidia", 2, 2), ("tenstorrent", 2, 4), ("tenstorrent", 4, 8)]:
        job = submit(f"{accelerator}-full-{count}", type="training-smoke", accelerator=accelerator, device_count=count)
        value, text = finished(job)
        assert value["exit_code"] == 0 and f"TRAINING_PASSED backend={accelerator} devices={chips}" in text
    # Cancelling a whole-QuietBox reservation must unblock another TT job.
    holder = submit("tt-cancel-whole", accelerator="tenstorrent", device_count=4,
                    script="import time; print('WHOLE_TT_RESERVED',flush=True); time.sleep(120)")
    wait(lambda: "WHOLE_TT_RESERVED" in logs(holder), "whole TT reservation running")
    queued = submit("tt-after-cancel", type="training-smoke", accelerator="tenstorrent", device_count=1)
    wait(lambda: "cannot allocate" in status(queued).get("message", ""), "TT job queued behind holder", timeout=60)
    assert api("POST", "/jobs/" + holder + "/cancel")["job_state"] == "Cancelled"
    finished(holder, "Cancelled")
    finished(queued)
    evidence["cancelled_tt_reservation_released"] = True
    listed = api("GET", "/jobs")["jobs"]
    assert set(evidence["jobs"].values()).issubset({job["id"] for job in listed})
    evidence["completed_at"] = datetime.now(timezone.utc).isoformat()
    (args.artifacts / "api-acceptance.json").write_text(json.dumps(evidence, indent=2) + "\n")
    print("MIST_API_PASSED: CPU, NVIDIA, Tenstorrent, queues, cancellation, logs, and checkpoints", flush=True)


if __name__ == "__main__":
    main()
