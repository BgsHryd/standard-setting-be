import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
if(process.env.QA_ALLOW_MUTATION!=='scratch-only') throw new Error('QA memerlukan backend dengan database scratch. Jalankan dengan QA_ALLOW_MUTATION=scratch-only hanya setelah memeriksa environment backend.');
const base='http://127.0.0.1:18081/api/v1';
const cases=[];
let session;
async function request(path,options={},auth=session){const response=await fetch(base+path,{...options,headers:{'Content-Type':'application/json',...(auth?{Authorization:`Bearer ${auth.accessToken}`}:{})}});const body=await response.json();return {status:response.status,data:body.data,message:body.message};}
async function ok(path,options={},auth=session){const r=await request(path,options,auth);assert.equal(r.status,200,`${path}: ${r.message}`);return r.data;}
const body=value=>JSON.stringify(value);
async function participant(username){let s=await ok('/auth/login',{method:'POST',body:body({username,password:'12345*'})},null);if(s.user.mustChangePassword)s=await ok('/auth/change-password',{method:'POST',body:body({currentPassword:'12345*',newPassword:'QA-user-password'})},s);return s;}
function pass(name){cases.push({name,status:'PASS'});console.log('PASS',name);}
const adminLogin=await request('/auth/login',{method:'POST',body:body({username:'superadmin',password:'12345*'})},null);
if(adminLogin.status===200){session=adminLogin.data;if(session.user.mustChangePassword)session=await ok('/auth/change-password',{method:'POST',body:body({currentPassword:'12345*',newPassword:'QA-admin-password'})});}else session=await ok('/auth/login',{method:'POST',body:body({username:'superadmin',password:'QA-admin-password'})},null);
pass('Login admin dan ganti password awal');
const subjects=await ok('/subjects');assert(subjects.some(s=>s.code==='MAT'));
const uniqueName=`QA Sejarah ${Date.now()}`;const added=await ok('/subjects',{method:'POST',body:body({name:uniqueName})});assert.equal((await request('/subjects',{method:'POST',body:body({name:uniqueName.toUpperCase()})})).status,409);pass('Mapel unik tanpa membedakan kapital');
await ok('/items',{method:'POST',body:body({items:[{id:`QA-${Date.now()}`,subjectCode:added.code,subjectName:added.name,questionType:'pilihan_ganda',maxScore:1}]})});pass('Butir untuk mapel katalog baru');
let p=await ok('/projects',{method:'POST',body:body({name:`QA Admin Fitur ${new Date().toISOString()}`,description:'Data scratch untuk QA 14 Oktober 2026',startDate:'2026-10-14T00:00:00Z',endDate:'2026-10-30T23:59:59Z',wizardStep:4,subjects:[{code:'MAT',name:'Matematika',itemIds:['MAT-001','MAT-002','MAT-003'],picName:'PIC Matematika QA',panelistCount:2,panelistNames:['Panelis MAT A','Panelis MAT B']},{code:'BIN',name:'Bahasa Indonesia',itemIds:['BIN-001','BIN-002','BIN-003'],picName:'PIC Bahasa QA',panelistCount:2,panelistNames:['Panelis BIN A','Panelis BIN B']}]})});
let rows=await ok(`/projects/${p.id}/accounts`);
const inputs=rows.filter(r=>r.status!=='removed').map(r=>({slotId:r.slotId,name:r.name,role:r.role,subjectCode:r.subjectCode}));
const users=await ok(`/projects/${p.id}/accounts/generate`,{method:'POST',body:body({accounts:inputs})});const retry=await ok(`/projects/${p.id}/accounts/generate`,{method:'POST',body:body({accounts:inputs})});assert.deepEqual(retry.map(u=>u.id),users.map(u=>u.id));pass('Pembuatan akun idempotent');
p=await ok(`/projects/${p.id}`);await ok(`/projects/${p.id}/activate`,{method:'POST'});pass('Aktivasi proyek dua mapel');
const sessions={};for(const u of users)sessions[u.id]=await participant(u.username);
const panels=users.filter(u=>u.role==='panelis');
for(const u of panels){for(const [i,suffix] of ['001','002','003'].entries()){if(u.subjectCode==='BIN'&&u.name.endsWith('B')&&i>0)continue;const itemId=`${u.subjectCode}-${suffix}`;const answer=i===0?{questionType:'pilihan_ganda',value:u.name.endsWith('A')?60:80}:{questionType:i===1?'pilihan_ganda_kompleks':'essay',distribution:Array.from({length:i===1?4:5},(_,n)=>n===(i===1?3:4)?100:0)};await ok(`/panelist/submissions/${itemId}`,{method:'PUT',body:body({roundNumber:1,answer})},sessions[u.id]);}}
let all=await ok(`/projects/${p.id}/monitoring/all`);assert(all.subjects.find(s=>s.subject.code==='MAT').statistics);assert.equal(all.subjects.find(s=>s.subject.code==='BIN').statistics,null);pass('Statistik lengkap dan parsial tidak dibuat menjadi nol');
p=await ok(`/projects/${p.id}`);const modified=structuredClone(p);modified.subjects[0].itemIds=['MAT-001'];assert.equal((await request(`/projects/${p.id}`,{method:'PUT',body:body(modified)})).status,409);pass('Edit butir ditolak setelah penilaian');
const matA=panels.find(u=>u.subjectCode==='MAT'&&u.name.endsWith('A'));
await ok(`/users/${matA.id}/active`,{method:'PATCH',body:body({active:false})});all=await ok(`/projects/${p.id}/monitoring/all`);assert.equal(all.subjects.find(s=>s.subject.code==='MAT').panelists.length,1);assert.equal((await request('/panelist/form',{},sessions[matA.id])).status,401);await ok(`/users/${matA.id}/active`,{method:'PATCH',body:body({active:true})});assert.equal((await request('/panelist/form',{},sessions[matA.id])).status,401);pass('Nonaktif hilang dari monitoring; token lama tetap ditolak setelah aktif kembali');
sessions[matA.id]=await ok('/auth/login',{method:'POST',body:body({username:matA.username,password:'QA-user-password'})},null);
await ok(`/users/${matA.id}/reset-password`,{method:'POST',body:body({confirmation:'RESET PASSWORD'})});assert.equal((await request('/panelist/form',{},sessions[matA.id])).status,401);assert.equal((await request('/auth/refresh',{method:'POST',body:body({refreshToken:sessions[matA.id].refreshToken})},null)).status,401);const reset=await ok('/auth/login',{method:'POST',body:body({username:matA.username,password:'12345*'})},null);assert.equal(reset.user.mustChangePassword,true);assert.equal((await request('/panelist/form',{},reset)).status,428);pass('Reset membatalkan access/refresh dan mewajibkan ganti password');
const pic=users.find(u=>u.subjectCode==='MAT'&&u.role==='pic_mapel');await ok('/pic/rounds/finalize',{method:'POST',body:body({roundNumber:1,confirmation:'FINALISASI PUTARAN'})},sessions[pic.id]);all=await ok(`/projects/${p.id}/monitoring/all?round=2`);assert.equal(all.subjects.find(s=>s.subject.code==='BIN').round.status,'unavailable');pass('Monitoring putaran berbeda antar-mapel');
assert.equal((await request('/subjects',{method:'POST',body:body({name:'Forbidden'})},sessions[pic.id])).status,403);assert.equal((await request(`/users/${matA.id}/active`,{method:'PATCH',body:body({active:false})},sessions[pic.id])).status,403);pass('Endpoint admin ditolak untuk PIC');
const result={date:'2026-10-14',database:'standard-setting-qa-admin-20261014',projectId:p.id,projectName:p.name,adminUsername:'superadmin',users:users.map(({id,username,name,role,subjectCode})=>({id,username,name,role,subjectCode})),cases};
await fs.writeFile('../qa-artifacts/admin-20261014/api-results.json',JSON.stringify(result,null,2));
console.log(`${cases.length} API cases PASS; fixture ${p.id}`);
