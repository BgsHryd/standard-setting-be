// Execute only against the explicitly isolated QA backend; never against production.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
const base = process.env.QA_API_URL || 'http://127.0.0.1:18081/api/v1';
if (!['127.0.0.1','localhost'].includes(new URL(base).hostname) || !process.env.QA_ALLOW_WRITE) throw new Error('Set QA_ALLOW_WRITE=1 and use an isolated local backend.');
const password = process.env.QA_PASSWORD || 'QA-Retest-20261013';
const initial = process.env.QA_INITIAL_PASSWORD || '12345*';
const evidence = {startedAt:new Date().toISOString(),baseURL:base,cases:[]};
let stage = 'setup';
async function request(path, session, method='GET', body, expected=200) {
  const response=await fetch(base+path,{method,headers:{'Content-Type':'application/json',...(session?{Authorization:`Bearer ${session.accessToken}`}:{})},...(body===undefined?{}:{body:JSON.stringify(body)})});
  const result=await response.json();
  assert.equal(response.status,expected,`${stage}: ${method} ${path}: ${JSON.stringify(result)}`);
  evidence.cases.push({stage,method,path,status:response.status});
  return result.data;
}
async function login(username) {
  let result;
  try { result=await request('/auth/login',null,'POST',{username,password:initial}); }
  catch { result=await request('/auth/login',null,'POST',{username,password}); }
  if (result.user.mustChangePassword) {
    await request('/me',result,'GET',undefined,428);
    result=await request('/auth/change-password',result,'POST',{currentPassword:initial,newPassword:password});
    assert.equal(result.user.mustChangePassword,false);
    assert.ok(result.accessToken);
  }
  await request('/me',result);
  return result;
}
try {
  stage='QA-09 fresh password session';
  const admin=await login('superadmin');
  const marker=`QA-${Date.now()}`;
  const itemIds=[`${marker}-PG`,`${marker}-PGK`,`${marker}-ESSAY`];
  const types=['pilihan_ganda','pilihan_ganda_kompleks','essay'];
  stage='QA-17 item create/edit/view';
  await request('/items',admin,'POST',{items:itemIds.map((id,i)=>({id,subjectCode:'MAT',subjectName:'Matematika',questionType:types[i],maxScore:[1,3,4][i],rubric:['QA only']}))});
  const item=await request(`/items/${itemIds[0]}`,admin);assert.equal(item.usageStatus,'available');
  stage='QA-10..15 draft wizard, resume, server metadata';
  let project=await request('/projects',admin,'POST',{name:`Retest ${marker}`,description:'QA isolated',startDate:'2026-10-13T00:00:00Z',endDate:'2026-10-31T23:59:59Z',wizardStep:1,subjects:[],status:'completed',createdBy:'forged',code:'FORGED'});
  assert.equal(project.status,'draft');assert.notEqual(project.code,'FORGED');
  await request(`/projects/${project.id}/activate`,admin,'POST',undefined,422);
  project=await request(`/projects/${project.id}`,admin,'PUT',{...project,wizardStep:3,subjects:[{code:'MAT',name:'Matematika',itemIds,panelistCount:2,picName:'PIC QA Retest',panelistNames:['Panelis QA A','Panelis QA B']}]});
  const saved=await request(`/projects/${project.id}`,admin);assert.equal(saved.name,project.name);assert.deepEqual(saved.subjects[0].itemIds,itemIds);
  const accounts=await request(`/projects/${project.id}/accounts/generate`,admin,'POST',{accounts:[{name:'PIC QA Retest',role:'pic_mapel',subjectCode:'MAT'},{name:'Panelis QA A',role:'panelis',subjectCode:'MAT'},{name:'Panelis QA B',role:'panelis',subjectCode:'MAT'}]});
  const again=await request(`/projects/${project.id}/accounts/generate`,admin,'POST',{accounts:[{name:'PIC QA Retest',role:'pic_mapel',subjectCode:'MAT'},{name:'Panelis QA A',role:'panelis',subjectCode:'MAT'},{name:'Panelis QA B',role:'panelis',subjectCode:'MAT'}]});
  assert.deepEqual(again.map(a=>a.id),accounts.map(a=>a.id));
  stage='QA-16 users and project origin';
  assert.equal((await request(`/users?projectId=${project.id}`,admin)).length,3);
  assert.ok((await request('/users',admin)).some(u=>u.role==='admin_super'&&!u.projectId&&u.lastLoginAt));
  await request(`/projects/${project.id}/activate`,admin,'POST');
  await request(`/projects/${project.id}`,admin,'PUT',project,409);
  assert.equal((await request(`/items/${itemIds[0]}`,admin)).usageStatus,'allocated');
  const picAccount=accounts.find(a=>a.role==='pic_mapel');
  const panelistAccounts=accounts.filter(a=>a.role==='panelis');
  const pic=await login(picAccount.username), panelists=await Promise.all(panelistAccounts.map(a=>login(a.username)));
  evidence.fixture={projectId:project.id,projectCode:project.code,itemIds,picUsername:picAccount.username,panelistUsernames:panelistAccounts.map(a=>a.username)};
  stage='KI-01/08 scope';
  await request('/projects',pic,'GET',undefined,403);
  await request('/pic/monitoring',panelists[0],'GET',undefined,403);
  await request('/panelist/form',admin,'GET',undefined,403);
  await request('/pic/monitoring?subjectCode=BIN',pic,'GET',undefined,403);
  await request('/pic/monitoring?projectId=other',pic,'GET',undefined,403);
  await request(`/pic/panelists/${admin.user.id}/submissions?round=1`,pic,'GET',undefined,403);
  stage='QA-20/21 initial monitoring no fabricated statistics';
  const initialMonitor=await request('/pic/monitoring',pic);assert.equal(initialMonitor.statistics,null);assert.equal(initialMonitor.totalDone,0);assert.equal(initialMonitor.panelists[0].lastActive,null);
  assert.equal((await request(`/projects/${project.id}/monitoring?subjectCode=MAT`,admin)).panelists.length,2);
  stage='KI-11/12 incomplete finalize/complete rejected';
  await request('/pic/rounds/finalize',pic,'POST',{confirmation:'FINALISASI PUTARAN',roundNumber:1},409);
  await request(`/projects/${project.id}/complete`,admin,'POST',{confirmation:'SELESAIKAN PROYEK'},409);
  stage='QA-25/26 real form and three answer types';
  assert.equal((await request('/panelist/form',panelists[0])).items.length,3);
  await request(`/panelist/submissions/${itemIds[0]}`,panelists[0],'PUT',{roundNumber:1,answer:{questionType:'essay',distribution:[100,0]}},422);
  await request(`/panelist/submissions/${itemIds[1]}`,panelists[0],'PUT',{roundNumber:1,answer:{questionType:types[1],distribution:[50,50]}},422);
  async function fill(round,delta) {
    for(let i=0;i<2;i++)for(let j=0;j<3;j++) {
      const answer=j===0?{questionType:types[0],value:60+20*i+delta}:{questionType:types[j],distribution:j===1?[0,0,0,100]:[0,0,100,0,0]};
      await request(`/panelist/submissions/${itemIds[j]}`,panelists[i],'PUT',{roundNumber:round,answer});
    }
  }
  await fill(1,0);
  const first=(await request('/panelist/form',panelists[0])).submissions.find(s=>s.itemId===itemIds[0]);
  const resaved=await request(`/panelist/submissions/${itemIds[0]}`,panelists[0],'PUT',{roundNumber:1,answer:{questionType:types[0],value:60}});assert.equal(resaved.id,first.id);
  assert.equal((await request('/panelist/form',panelists[0])).submissions.length,3);
  const full=await request('/pic/monitoring',pic);assert.equal(full.totalDone,6);assert.equal(full.statistics.mean,71.25);assert.equal(full.statistics.standardDeviation,1.25);assert.equal(full.canFinalize,true);
  assert.equal((await request(`/projects/${project.id}/panelists/${panelists[0].user.id}/submissions?subjectCode=MAT&round=1`,admin)).length,3);
  assert.equal((await request(`/pic/panelists/${panelists[0].user.id}/submissions?round=1`,pic)).length,3);
  stage='QA-24/27/28 finalize, history and rollback';
  await request('/pic/rounds/finalize',pic,'POST',{confirmation:'WRONG',roundNumber:1},409);
  await request('/pic/rounds/finalize',pic,'POST',{confirmation:'FINALISASI PUTARAN',roundNumber:1});
  for(const p of panelists) { const f=await request('/panelist/form',p);assert.equal(f.round.number,2);assert.equal(f.submissions.length,0);assert.equal(f.previousSubmissions.length,3);assert.equal(f.previousRound,1); }
  await request(`/panelist/submissions/${itemIds[0]}`,panelists[0],'PUT',{roundNumber:1,answer:{questionType:types[0],value:70}},422);
  await request('/pic/rounds/rollback',pic,'POST',{confirmation:'ROLLBACK PUTARAN',roundNumber:2});
  assert.equal((await request('/panelist/form',panelists[0])).round.number,1);
  await request('/pic/rounds/finalize',pic,'POST',{confirmation:'FINALISASI PUTARAN',roundNumber:1});
  await fill(2,10);
  await request('/pic/rounds/finalize',pic,'POST',{confirmation:'FINALISASI PUTARAN',roundNumber:2});
  stage='QA-18/19 final-round snapshot only';
  await request(`/projects/${project.id}/complete`,admin,'POST',{confirmation:'SELESAIKAN PROYEK'});
  const result=await request(`/items/${itemIds[0]}/result`,admin);assert.equal(result.statistics.mean,80);assert.equal(result.statistics.roundNumber,2);assert.equal(result.statistics.panelistCount,2);assert.equal(result.responses.length,2);assert.equal(result.item.readOnly,true);
  assert.ok(result.responses.every(r=>r.roundNumber===2));
  assert.equal((await request('/panelist/form',panelists[0])).readOnly,true);
  await request(`/panelist/submissions/${itemIds[0]}`,panelists[0],'PUT',{roundNumber:2,answer:{questionType:types[0],value:50}},422);
  await request('/items',admin,'POST',{items:[{id:itemIds[0],subjectCode:'MAT',subjectName:'Matematika',questionType:types[0],maxScore:1}]},409);
  // A separate active fixture remains available for browser autosave / round-switch QA.
  stage='browser fixture';
  const browserIds=itemIds.map(id=>id+'-UI');
  await request('/items',admin,'POST',{items:browserIds.map((id,i)=>({id,subjectCode:'MAT',subjectName:'Matematika',questionType:types[i],maxScore:[1,3,4][i]}))});
  let browserProject=await request('/projects',admin,'POST',{name:`Browser Retest ${marker}`,startDate:'2026-10-13T00:00:00Z',endDate:'2026-10-31T23:59:59Z',wizardStep:3,subjects:[{code:'MAT',name:'Matematika',itemIds:browserIds,panelistCount:1,picName:'PIC Browser QA',panelistNames:['Panelis Browser QA']}]});
  const browserAccounts=await request(`/projects/${browserProject.id}/accounts/generate`,admin,'POST',{accounts:[{name:'PIC Browser QA',role:'pic_mapel',subjectCode:'MAT'},{name:'Panelis Browser QA',role:'panelis',subjectCode:'MAT'}]});
  for(const a of browserAccounts) await login(a.username);
  await request(`/projects/${browserProject.id}/activate`,admin,'POST');
  evidence.browserFixture={projectId:browserProject.id,projectCode:browserProject.code,itemIds:browserIds,accounts:browserAccounts.map(({id,username,role})=>({id,username,role}))};
  evidence.outcome='PASS';
} catch(error) { evidence.outcome='FAIL';evidence.failure={stage,message:error.message};process.exitCode=1; }
evidence.finishedAt=new Date().toISOString();
const output=new URL('../docs/qa/retest-api-2026-10-13.json',import.meta.url);
await mkdir(new URL('../docs/qa/',import.meta.url),{recursive:true});
await writeFile(output,JSON.stringify(evidence,null,2));
console.log(JSON.stringify({outcome:evidence.outcome,requests:evidence.cases.length,...(evidence.failure?{failure:evidence.failure}:{}),fixture:evidence.fixture,browserFixture:evidence.browserFixture},null,2));
