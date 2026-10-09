"""Submit a uniquely named training check reserving one to four n300 boards.

Uses the admin KUBECONFIG, k3s kubectl, and Python PyYAML. This submits the
small TT-NN regression test; application/model integration is separate.
"""

import argparse
import copy
from pathlib import Path
import re
import subprocess

import yaml


def manifests(name, boards, steps):
    docs = list(yaml.safe_load_all(Path(__file__).with_name("training-tenstorrent-job.yaml").read_text()))
    pvc, claim, job = copy.deepcopy(docs)
    claim["metadata"]["name"] = f"training-tenstorrent-{boards}-boards"
    claim["spec"]["spec"]["devices"]["requests"][0]["exactly"]["count"] = boards
    job["metadata"]["name"] = name
    pod = job["spec"]["template"]["spec"]
    pod["resourceClaims"][0]["resourceClaimTemplateName"] = claim["metadata"]["name"]
    train = pod["containers"][0]
    train["command"] = train["command"][:4] + ["--expected-devices", str(boards * 2),
        "--steps", str(steps), "--output", f"/checkpoints/{name}"]
    for resources in (train["resources"]["requests"], train["resources"]["limits"]):
        resources["hugepages-1Gi"] = f"{boards * 2}Gi"
    train["resources"]["requests"]["cpu"] = str(boards * 2)
    train["resources"]["requests"]["memory"] = f"{boards * 4}Gi"
    train["resources"]["limits"]["cpu"] = str(boards * 8)
    train["resources"]["limits"]["memory"] = f"{boards * 8}Gi"
    return pvc, claim, job


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("name", help="Unique Kubernetes Job name; also used for the checkpoint directory")
    parser.add_argument("--boards", type=int, choices=range(1, 5), default=1)
    parser.add_argument("--steps", type=int, default=100)
    parser.add_argument("--dry-run", action="store_true", help="Print the manifests without changing the cluster")
    args = parser.parse_args()
    if len(args.name) > 63 or not re.fullmatch(r"[a-z0-9](?:[-a-z0-9]*[a-z0-9])?", args.name):
        parser.error("name must be a DNS label with at most 63 characters")
    if args.steps < 100:
        parser.error("at least 100 steps are required for the convergence check")
    pvc, claim, job = manifests(args.name, args.boards, args.steps)
    if args.dry_run:
        print(yaml.safe_dump_all([pvc, claim, job]), end="")
    else:
        subprocess.run(["k3s", "kubectl", "apply", "-f", "-"],
                       input=yaml.safe_dump_all([pvc, claim]), text=True, check=True)
        configmap = subprocess.run(["k3s", "kubectl", "-n", "mist", "create", "configmap",
            "training-smoke-script", "--from-file=" + str(Path(__file__).with_name("training_smoke.py")),
            "--dry-run=client", "-o", "yaml"], text=True, capture_output=True, check=True).stdout
        subprocess.run(["k3s", "kubectl", "apply", "-f", "-"], input=configmap, text=True, check=True)
        subprocess.run(["k3s", "kubectl", "create", "-f", "-"],
                       input=yaml.safe_dump(job), text=True, check=True)
        print(f"Requested {args.boards} board(s), {args.boards * 2} chips; checkpoints: /checkpoints/{args.name}")
