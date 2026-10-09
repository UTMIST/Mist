# Verification for this recorded two-machine pilot; never prints credentials.
import json, pathlib, subprocess, time, shlex
root=pathlib.Path('/home/utmist/mist-department-results-2026-10-09')
credentials=json.loads(pathlib.Path('/home/utmist/.config/mist/admin-bootstrap.json').read_text())
ssh=['ssh','-i','/home/utmist/.ssh/id_ed25519_quietbox_codex','-o','IdentitiesOnly=yes','-o','BatchMode=yes','utmist-tt@100.95.175.37']
base='~/mist-department-verification/mist-cli --api-url http://100.73.139.66:8088/api --config ~/mist-department-verification/verification-config.json '
def run(cmd, data=None):
 p=subprocess.run(ssh+[cmd], input=data, text=True,capture_output=True,timeout=40)
 if p.returncode: raise RuntimeError('Remote command failed: '+p.stderr[:2000])
 return p.stdout
probe="""from pathlib import Path
import torch
assert torch.cuda.is_available(), 'CUDA unavailable'
assert torch.cuda.device_count()==1, torch.cuda.device_count()
x=torch.randn(128,128,device='cuda',requires_grad=True)
loss=(x@x).square().mean()
loss.backward()
assert torch.isfinite(x.grad).all()
Path('/outputs/remote-cli-proof.txt').write_text('REMOTE_CLI_NVIDIA_PASSED devices=1')
print('REMOTE_CLI_NVIDIA_PASSED devices=1',flush=True)
"""
run("cat > ~/mist-department-verification/nvidia.py",probe)
report={'origin_node':'utmist-tt','destination_node':'utmist-z1opa08','team_id':'team-db3933031a6c2437'}
try:
 run(base+'auth login --email '+shlex.quote(credentials['email'])+' --password-stdin',credentials['password']+'\n')
 report['login']=True
 run(base+'team use team-db3933031a6c2437')
 report['team_selection']=True
 submitted=run(base+'job submit ~/mist-department-verification/nvidia.py --compute NVIDIA --devices 1 --timeout 120 --name remote-cli-nvidia')
 job=submitted.strip().split()[-1]; report['job_id']=job
 for _ in range(90):
  status=run(base+'job status '+job)
  if 'Success' in status: break
  if 'Failure' in status or 'Cancelled' in status: raise RuntimeError(status)
  time.sleep(2)
 else: raise RuntimeError('Remote submitted job did not finish')
 logs=run(base+'job logs '+job)
 assert 'utmist-z1opa08' in status and 'REMOTE_CLI_NVIDIA_PASSED' in logs
 report['job_succeeded']=True
 (root/'remote-cli-status.txt').write_text(status)
 (root/'remote-cli-job.log').write_text(logs)
 print(status,logs)
finally:
 run(base+'auth logout')
 run("python3 -c \"from pathlib import Path; Path.home().joinpath('mist-department-verification/verification-config.json').unlink(missing_ok=True)\"")
(root/'remote-cli-results.json').write_text(json.dumps(report,indent=2)+'\n')
print('PASS SSH_TENSTORRENT_CLI_SUBMISSION_NVIDIA_ON_MAIN')
