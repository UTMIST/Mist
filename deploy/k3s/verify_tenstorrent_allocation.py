"""Live acceptance test: four isolated n300 jobs, a fifth queued, then reuse.

Run from the repo root with an admin KUBECONFIG and Python PyYAML installed.
Test jobs and checkpoint subdirectories are unique per run and retained.
The device probes mount character nodes solely to check cgroup enforcement;
normal training jobs receive their devices exclusively through DRA/CDI.
"""

import argparse
import copy
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess
import time

import yaml


def kubectl(*args, data=None):
    result = subprocess.run(["k3s", "kubectl", *args], input=data, text=True,
                            capture_output=True, timeout=45)
    if result.returncode:
        raise RuntimeError(result.stderr or result.stdout)
    return result.stdout


def get(kind, *args):
    return json.loads(kubectl("-n", "mist", "get", kind, *args, "-o", "json"))


def poll(check, description, timeout=240):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(2)
    raise AssertionError(f"Timed out: {description}")


def pod_for(job):
    pods = get("pods", "-l", f"job-name={job}")["items"]
    if not pods:
        return None
    assert len(pods) == 1, f"Expected one pod for {job}"
    pod = pods[0]
    assert pod["status"]["phase"] != "Failed", kubectl("-n", "mist", "logs", pod["metadata"]["name"])
    return pod


def collect_results(run, jobs, pods, artifacts):
    """Read retained PVC data after training pods exit, then remove the reader."""
    reader_name = run + "-results"
    reader = {"apiVersion": "v1", "kind": "Pod", "metadata": {
        "name": reader_name, "namespace": "mist"}, "spec": {
        "restartPolicy": "Never", "automountServiceAccountToken": False,
        "activeDeadlineSeconds": 180,
        "nodeSelector": {"kubernetes.io/hostname": "utmist-tt"},
        "containers": [{"name": "reader", "image": pods[0]["spec"]["containers"][0]["image"],
            "imagePullPolicy": "IfNotPresent", "command": ["sleep", "180"],
            "securityContext": {"allowPrivilegeEscalation": False, "capabilities": {"drop": ["ALL"]}},
            "resources": {"requests": {"cpu": "10m", "memory": "32Mi"},
                          "limits": {"cpu": "1", "memory": "256Mi"}},
            "volumeMounts": [{"name": "checkpoints", "mountPath": "/checkpoints", "readOnly": True}]}],
        "volumes": [{"name": "checkpoints", "persistentVolumeClaim": {
            "claimName": "training-tenstorrent-checkpoints", "readOnly": True}}]}}
    kubectl("create", "-f", "-", data=json.dumps(reader))
    try:
        poll(lambda: get("pod", reader_name)["status"]["phase"] == "Running", "PVC reader running", timeout=90)
        script = '''import json, pathlib, sys
root=pathlib.Path("/checkpoints")/sys.argv[1]
data=[]
for i in range(5):
    folder=root/str(i)
    results=json.loads((folder/"results.json").read_text())
    for result in results:
        assert (folder/("tenstorrent-"+str(result["device_id"])+".pt")).stat().st_size>1000
    data.append({"allocation":json.loads((folder/"allocation.json").read_text()), "results":results})
print(json.dumps(data))
'''
        summaries = json.loads(kubectl("-n", "mist", "exec", reader_name, "--", "python3", "-c", script, run))
        for index, summary in enumerate(summaries):
            summary["job"] = jobs[index]["metadata"]["name"]
            assert len(summary["results"]) == 2 and summary["allocation"]["chip_count"] == 2
            assert len(summary["allocation"]["device_nodes"]) == 1
            for result in summary["results"]:
                assert result["final_mse"] < result["initial_mse"] * 0.02
                assert result["validation_mse"] < 0.0005
                assert result["max_weight_change"] > 0.05
                assert abs(result["checkpoint_reload_mse"] - result["validation_mse"]) < 1e-7
        starts = [datetime.fromisoformat(s["results"][0]["started_at"]) for s in summaries[:4]]
        ends = [datetime.fromisoformat(s["results"][0]["finished_at"]) for s in summaries[:4]]
        overlap = (min(ends) - max(starts)).total_seconds()
        assert overlap > 0, "First-chip training must overlap across all four jobs"
        released = min(datetime.fromisoformat(p["status"]["containerStatuses"][0]["state"]["terminated"]["finishedAt"].replace("Z", "+00:00"))
                       for p in pods[:4])
        assert datetime.fromisoformat(summaries[4]["results"][0]["started_at"]) > released
        data = {"four_job_training_overlap_seconds": overlap, "jobs": summaries}
        (artifacts / "training-results.json").write_text(json.dumps(data, indent=2) + "\n")
        with (artifacts / "concurrent-checkpoints.tar.gz").open("wb") as output:
            subprocess.run(["k3s", "kubectl", "-n", "mist", "exec", reader_name, "--", "tar",
                            "-C", "/checkpoints", "-czf", "-", run], stdout=output, check=True, timeout=45)
        return data
    finally:
        kubectl("-n", "mist", "delete", "pod", reader_name, "--wait=false", "--ignore-not-found")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--artifacts", type=Path, required=True)
    args = parser.parse_args()
    args.artifacts.mkdir(parents=True, exist_ok=True)
    run = "tt-alloc-" + datetime.now(timezone.utc).strftime("%m%d-%H%M%S")
    checkpoint_root = f"/checkpoints/{run}"
    docs = list(yaml.safe_load_all(Path("deploy/k3s/training-tenstorrent-job.yaml").read_text()))
    base = next(doc for doc in docs if doc["kind"] == "Job")
    kubectl("apply", "-f", "-", data=yaml.safe_dump_all(doc for doc in docs if doc["kind"] != "Job"))
    configmap = kubectl("-n", "mist", "create", "configmap", "training-smoke-script",
                       "--from-file=deploy/k3s/training_smoke.py", "--dry-run=client", "-o", "yaml")
    kubectl("apply", "-f", "-", data=configmap)
    names = [f"{run}-{i}" for i in range(5)]

    def job(index):
        manifest = copy.deepcopy(base)
        manifest["metadata"]["name"] = names[index]
        manifest["metadata"]["labels"] = {"mist.io/allocation-test": run}
        spec = manifest["spec"]["template"]["spec"]
        spec["automountServiceAccountToken"] = False
        container = spec["containers"][0]
        container["command"] = container["command"][:4] + [
            "--expected-devices", "2", "--steps", "500", "--output", f"{checkpoint_root}/{index}",
            "--start-file", f"{checkpoint_root}/start"]
        for number in range(4):
            spec["volumes"].append({"name": f"probe-{number}", "hostPath": {
                "path": f"/dev/tenstorrent/{number}", "type": "CharDevice"}})
            container["volumeMounts"].append({"name": f"probe-{number}",
                "mountPath": f"/isolation/{number}"})
        return manifest

    kubectl("apply", "-f", "-", data=yaml.safe_dump_all(job(i) for i in range(4)))
    print(f"RUN={run}: waiting for four initialized training jobs", flush=True)

    def ready():
        pods = [pod_for(name) for name in names[:4]]
        if not all(pod and pod["status"]["phase"] == "Running" for pod in pods):
            return None
        logs = [kubectl("-n", "mist", "logs", pod["metadata"]["name"]) for pod in pods]
        return pods if all("BARRIER_READY" in log for log in logs) else None

    pods = poll(ready, "four jobs ready")
    claims = get("resourceclaims")
    allocations = {}
    for pod in pods:
        claim_name = pod["status"]["resourceClaimStatuses"][0]["resourceClaimName"]
        claim = next(c for c in claims["items"] if c["metadata"]["name"] == claim_name)
        result = claim["status"]["allocation"]["devices"]["results"]
        assert len(result) == 1
        allocations[pod["metadata"]["name"]] = result[0]["device"]
    assert len(set(allocations.values())) == 4, allocations
    (args.artifacts / "claims-while-full.json").write_text(json.dumps(claims, indent=2) + "\n")
    (args.artifacts / "pods-while-full.json").write_text(json.dumps(pods, indent=2) + "\n")

    probe = '''import json,os,pathlib
visible=list(pathlib.Path("/dev/tenstorrent").iterdir())
assert len(visible)==1, visible
allowed=[]; denied=[]
for i in range(4):
    try:
        fd=os.open("/isolation/"+str(i),os.O_RDWR); os.close(fd); allowed.append(str(i))
    except PermissionError:
        denied.append(str(i))
assert allowed==[visible[0].name], (allowed,visible)
assert len(denied)==3, denied
print(json.dumps({"allowed":allowed,"denied":denied}))
'''
    isolation = {}
    for pod in pods:
        name = pod["metadata"]["name"]
        isolation[name] = json.loads(kubectl("-n", "mist", "exec", name, "--", "python3", "-c", probe))
    print(f"Four distinct reservations, device access isolated: {allocations}", flush=True)

    kubectl("apply", "-f", "-", data=yaml.safe_dump(job(4)))

    def queued():
        pod = pod_for(names[4])
        if not pod:
            return None
        events = get("events", "--field-selector", f"involvedObject.name={pod['metadata']['name']}")["items"]
        failed = [event for event in events if event["reason"] == "FailedScheduling"]
        if not failed:
            return None
        assert pod["status"]["phase"] == "Pending", pod
        assert not pod["spec"].get("nodeName"), pod
        assert any("resourceclaim" in e["message"].lower() or
                   "cannot allocate" in e["message"].lower() for e in failed), failed
        return {"pod": pod, "events": failed}

    queue_evidence = poll(queued, "fifth job blocked by device allocation", timeout=60)
    (args.artifacts / "fifth-job-queued.json").write_text(json.dumps(queue_evidence, indent=2) + "\n")
    print("Fifth job is pending because all boards are reserved; releasing training barrier", flush=True)
    kubectl("-n", "mist", "exec", pods[0]["metadata"]["name"], "--", "touch", f"{checkpoint_root}/start")

    def complete():
        jobs = [get("jobs", name) for name in names]
        assert not any(j["status"].get("failed") for j in jobs), "A training job failed"
        return jobs if all(j["status"].get("succeeded") == 1 for j in jobs) else None

    jobs = poll(complete, "all five jobs successfully trained", timeout=300)
    final_pods = [pod_for(name) for name in names]
    for index, pod in enumerate(final_pods):
        log = kubectl("-n", "mist", "logs", pod["metadata"]["name"])
        assert "TRAINING_PASSED backend=tenstorrent devices=2" in log
        assert pod["status"]["containerStatuses"][0]["state"]["terminated"]["exitCode"] == 0
        (args.artifacts / f"job-{index}.log").write_text(log)
    summary = {"run": run, "allocations": allocations, "isolation": isolation,
               "fifth_job_queued": True, "all_five_jobs_complete": True,
               "checkpoint_root": checkpoint_root, "jobs": jobs, "pods": final_pods}
    summary["training"] = collect_results(run, jobs, final_pods, args.artifacts)
    (args.artifacts / "acceptance.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(f"ALLOCATION_PASSED run={run}; all five jobs completed; checkpoints at {checkpoint_root}", flush=True)


if __name__ == "__main__":
    main()
