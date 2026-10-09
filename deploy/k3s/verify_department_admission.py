# Verification for this recorded two-machine pilot; never prints credentials.
import copy,json,os,pathlib,subprocess
out=pathlib.Path('/home/utmist/mist-department-results-2026-10-09')
env={**os.environ,'KUBECONFIG':'/home/utmist/.kube/config'}
ns='mist-team-db3933031a6c2437'
def k(*args, data=None):return subprocess.run(['k3s','kubectl',*args],input=data,text=True,capture_output=True,env=env)
jobs=json.loads(k('get','jobs','-n',ns,'-o','json').stdout)['items']
source=next(j for j in jobs if j['metadata']['name']=='mist-7d426bc89696c01e19474c00')
base={'apiVersion':'v1','kind':'Pod','metadata':{'name':'admission-probe','namespace':ns},'spec':source['spec']['template']['spec']}
probes={
'host_network':lambda p:p['spec'].update(hostNetwork=True),
'host_pid':lambda p:p['spec'].update(hostPID=True),
'service_account_token':lambda p:p['spec'].update(automountServiceAccountToken=True),
'privileged_container':lambda p:p['spec']['containers'][0]['securityContext'].update(privileged=True,allowPrivilegeEscalation=True),
'root_host_path':lambda p:p['spec']['volumes'].append({'name':'forbidden','hostPath':{'path':'/'}}),
'capability_add':lambda p:p['spec']['containers'][0]['securityContext']['capabilities'].update(add=['SYS_ADMIN'])}
report={}
for name,edit in probes.items():
 p=copy.deepcopy(base);edit(p)
 r=k('create','--dry-run=server','-f','-',data=json.dumps(p))
 assert r.returncode and 'mist-team-boundary' in r.stderr,(name,r.stderr)
 report[name]={'denied':True,'reason':r.stderr.strip()}
 print('PASS ADMISSION_DENIED_'+name)
(out/'admission-results.json').write_text(json.dumps(report,indent=2)+'\n')
