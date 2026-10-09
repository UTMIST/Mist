// Requires Playwright and an existing protected admin bootstrap JSON file.
// No credentials, session tokens or cookies are written into the result report.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { randomBytes } from 'node:crypto'
const { chromium } = await import(process.env.MIST_PLAYWRIGHT_MODULE ?? '/tmp/mist-browser-check/node_modules/playwright/index.mjs')
const base=process.env.MIST_SITE_URL ?? 'http://100.73.139.66:8088'
const credentials=JSON.parse(readFileSync(process.env.MIST_ADMIN_FILE ?? '/home/utmist/.config/mist/admin-bootstrap.json'))
const output=process.env.MIST_EVIDENCE_DIR ?? '/home/utmist/mist-complete-results-2026-10-09'
mkdirSync(output,{recursive:true})
const browser=await chromium.launch({headless:true})
const context=await browser.newContext({baseURL:base,viewport:{width:1440,height:1000}})
const page=await context.newPage()
const errors=[];page.on('pageerror',e=>errors.push(e.message))
const evidence={site:base,started:new Date().toISOString(),jobs:[],checks:{}}
const check=(value,message)=>{if(!value)throw new Error(message)}
async function login(target,email,password){await target.goto('/jobs');await target.getByLabel('Email',{exact:true}).fill(email);await target.getByLabel('Password',{exact:true}).fill(password);await target.getByRole('button',{name:'Sign in',exact:true}).click();await target.getByRole('heading',{name:'Jobs',exact:true}).waitFor()}
async function json(path,options){const r=await context.request.fetch(base+path,options);check(r.ok(),`${path} HTTP ${r.status()} ${await r.text()}`);return r.json()}
function matrix(n,m,phase){return Array.from({length:n},(_,i)=>Array.from({length:m},(_,j)=>Math.sin((i+1)*(j+2)+phase)))}
function multiply(x,w){return x.map(row=>w[0].map((_,k)=>row.reduce((sum,v,j)=>sum+v*w[j][k],0)))}
try{
 await login(page,credentials.email,credentials.password)
 evidence.checks.browser_login=true
 await page.screenshot({path:resolve(output,'01-jobs.png'),fullPage:true})
 const x=matrix(128,32,0.1),vx=matrix(64,32,1.2),truth=matrix(32,32,2.3).map(row=>row.map(v=>v*0.1))
 const dataset=Buffer.from(JSON.stringify({x,y:multiply(x,truth),validation_x:vx,validation_y:multiply(vx,truth)}))
 await page.goto('/datasets')
 await page.getByLabel('Dataset name (optional)').fill('Shared regression verification')
 await page.getByLabel('File or ZIP archive').setInputFiles({name:'regression.json',mimeType:'application/json',buffer:dataset})
 await page.getByRole('button',{name:'Upload dataset',exact:true}).click()
 await page.getByRole('status').filter({hasText:'is ready to use'}).waitFor({timeout:60000})
 const ds=(await json('/api/datasets')).datasets.find(d=>d.name==='Shared regression verification')
 check(ds?.state==='Ready','Uploaded dataset not ready');evidence.dataset={id:ds.id,sha256:ds.sha256,size:ds.size}
 await page.screenshot({path:resolve(output,'02-dataset.png'),fullPage:true})
 const training=readFileSync('deploy/k3s/examples/train_shared.py','utf8')
 const cpuScript=`import json\nfrom pathlib import Path\np=Path('/inputs/regression.json')\nd=json.loads(p.read_text())\nassert len(d['x'])==128\nPath('/outputs/dataset-check.json').write_text(json.dumps({'rows':len(d['x']),'input_readonly':True}))\ntry:\n Path('/inputs/probe').write_text('bad')\n raise AssertionError('dataset mount is writable')\nexcept OSError:\n pass\nprint('SHARED_DATASET_PASSED CPU',flush=True)\n`
 for(const backend of ['cpu','nvidia','tenstorrent']){
  await page.goto('/jobs');await page.getByLabel('Name',{exact:true}).fill(`Shared storage ${backend}`)
  await page.getByLabel('Compute',{exact:true}).selectOption(backend)
  await page.getByLabel('Dataset (optional)').selectOption(ds.id)
  if(backend!=='cpu')await page.getByLabel(backend==='nvidia'?'GPUs':'Boards (two chips each)').fill(backend==='nvidia'?'2':'1')
  // Custom Python script with argv exercises uploaded data on actual accelerator.
  const script=backend==='cpu'?cpuScript:training.replace('args = parser.parse_args()',`args = parser.parse_args(['${backend}', '--expected-devices', '${backend==='nvidia'?2:2}', '--output', '/outputs'])`)
  await page.getByLabel('Python script',{exact:true}).fill(script)
  const responsePromise=page.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/api/jobs'))
  await page.getByRole('button',{name:'Submit job',exact:true}).click()
  const response=await responsePromise;check(response.ok(),`Submit ${backend} failed: ${await response.text()}`)
  const job=(await response.json()).job;evidence.jobs.push({id:job.id,backend})
 }
 for(const entry of evidence.jobs){
  const deadline=Date.now()+10*60*1000
  while(Date.now()<deadline){const job=await json(`/api/jobs/${entry.id}`);entry.state=job.job_state;entry.node=job.node;if(['Success','Failure','Cancelled'].includes(job.job_state))break;await new Promise(r=>setTimeout(r,3000))}
  const logs=(await json(`/api/jobs/${entry.id}/logs`)).logs
  writeFileSync(resolve(output,`${entry.backend}.log`),logs)
  check(entry.state==='Success',`${entry.backend} failed: ${logs.slice(-2000)}`)
  check(logs.includes(entry.backend==='cpu'?'SHARED_DATASET_PASSED':'TRAINING_PASSED'),`Missing training marker ${entry.backend}`)
  if(entry.backend!=='cpu')check(logs.includes('DATASET_LOADED file=/inputs/regression.json'),'Training did not consume uploaded data')
  const listing=await json(`/api/jobs/${entry.id}/files`);check(listing.files.length>0,`No persisted outputs ${entry.backend}`)
  entry.files=[]
  for(const file of listing.files){const r=await context.request.get(`${base}/api/jobs/${entry.id}/files/download`,{params:{path:file.path}});check(r.status()===200,`Download failed ${file.path}`);const contents=await r.body();check(contents.length===file.size,'Output size mismatch');writeFileSync(resolve(output,`${entry.backend}-${file.path.replaceAll('/','_')}`),contents);entry.files.push({path:file.path,size:file.size})}
 }
 await page.goto('/jobs');await page.getByRole('button',{name:'Files',exact:true}).first().click();await page.getByRole('heading',{name:'Saved files'}).waitFor();await page.screenshot({path:resolve(output,'03-completed-files.png'),fullPage:true})
 const downloadPromise=page.waitForEvent('download');await page.locator('a[href*="/files/download"]').first().click();const download=await downloadPromise;await download.saveAs(resolve(output,'browser-download-'+download.suggestedFilename()));check(await download.failure()===null,'Browser download failed')
 evidence.checks.browser_upload_select_train_download=true
 // Real second account created using the administrative website.
 const member={name:'Verification Member',email:`verify-${Date.now()}@mist.invalid`,password:randomBytes(20).toString('base64url')}
 await page.goto('/profile');await page.getByLabel('Name',{exact:true}).fill(member.name);await page.getByLabel('Email',{exact:true}).fill(member.email);await page.getByLabel('Initial password').fill(member.password)
 const createPromise=page.waitForResponse(r=>r.url().endsWith('/auth/admin/create-user'));await page.getByRole('button',{name:'Add member'}).click();const created=await createPromise;check(created.ok(),`Member creation failed: ${await created.text()}`)
 const memberID=(await created.json()).user.id
 const memberContext=await browser.newContext({baseURL:base});const memberPage=await memberContext.newPage();await login(memberPage,member.email,member.password)
 await memberPage.goto('/profile');await memberPage.getByLabel('Current password').fill(member.password);member.password=randomBytes(20).toString('base64url');await memberPage.getByLabel('New password').fill(member.password);await memberPage.getByRole('button',{name:'Change password',exact:true}).click();await memberPage.getByRole('status').filter({hasText:'Password changed.'}).waitFor();evidence.checks.member_password_change=true
 const foreignJob=evidence.jobs[0].id
 for(const path of [`/api/jobs/${foreignJob}`,`/api/jobs/${foreignJob}/logs`,`/api/jobs/${foreignJob}/files`,`/api/datasets/${ds.id}`]){const r=await memberContext.request.get(base+path);check(r.status()===404,`Foreign resource exposed: ${path}`)}
 check((await (await memberContext.request.get(base+'/api/jobs')).json()).count===0,'Member saw administrator jobs')
 const forbidden=await memberContext.request.post(base+'/auth/admin/create-user',{headers:{Origin:base},data:{name:'bad',email:'bad@mist.invalid',password:member.password,role:'admin'}});check(forbidden.status()===403,'Regular member created admin')
 const anon=await browser.newContext({baseURL:base});check((await anon.request.get(base+'/api/jobs')).status()===401,'Anonymous jobs accessible');check((await anon.request.post(base+'/auth/sign-up/email',{data:{name:'bad',email:'bad2@mist.invalid',password:member.password}})).status()>=400,'Public signup allowed');await anon.close()
 const evil=await context.request.post(base+'/api/jobs',{headers:{Origin:'http://evil.invalid'},data:{command:['true']}});check(evil.status()===403,'Cross-origin submission accepted')
 const ban=await context.request.post(base+'/auth/admin/ban-user',{headers:{Origin:base},data:{userId:memberID,banReason:'Completed verification'}});check(ban.ok(),`Ban failed HTTP ${ban.status()}: ${await ban.text()}`);check((await memberContext.request.get(base+'/api/jobs')).status()===401,'Deactivated session still accepted');await memberContext.close()
 evidence.checks.member_creation_isolation_deactivation=true
 evidence.checks.anonymous_signup_csrf_denied=true
 check(errors.length===0,`Browser errors: ${errors.join('; ')}`);evidence.browser_errors=errors
 evidence.finished=new Date().toISOString();writeFileSync(resolve(output,'complete-results.json'),JSON.stringify(evidence,null,2));console.log(JSON.stringify(evidence,null,2))
}catch(err){evidence.error=err.message;writeFileSync(resolve(output,'complete-results.json'),JSON.stringify(evidence,null,2));throw err}finally{await browser.close()}
