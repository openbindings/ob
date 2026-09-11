import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
import {spawn,execFileSync} from 'node:child_process';

// The binary uses normal account configuration. Never substitute a developer
// account or a source/storage overlay for the disposable hosted-runner proof.
assert.equal(process.env.GITHUB_ACTIONS,'true','Use a disposable hosted runner, never a developer account');
assert.equal(process.platform,'linux');
assert.equal(process.arch,'x64');
const cohort=fs.realpathSync(process.argv[2]);
const platformResult=JSON.parse(fs.readFileSync(process.argv[3],'utf8'));
const evidence=path.resolve(process.argv[4]);
assert(!fs.existsSync(evidence),'Evidence directory must be fresh');
fs.mkdirSync(evidence,{recursive:true});
const elements=path.join(cohort,'elements');
const elementsSHA='eb590acafecb94c4701bf16e878f59212cbf032c';
const hash=bytes=>createHash('sha256').update(bytes).digest('hex');
const git=(folder,args)=>execFileSync('git',args,{cwd:folder,encoding:'utf8'}).trim();
const record={started:new Date().toISOString(),scope:'Same normal binary; exact copied Elements tests; no source/storage overlay',host:{platform:process.platform,arch:process.arch,disposableHostedRunner:true},commands:[],testInputs:{},assets:[]};
const save=()=>fs.writeFileSync(path.join(evidence,'RESULTS.json'),JSON.stringify(record,null,2)+'\n');
const env={...process.env,OB_NO_UPDATE_CHECK:'1',OB_TEST_NO_CREDENTIAL_STORE:'1',OB_CREDENTIALS_FILE:path.join(evidence,'credentials.json')};
const quote=value=>"'"+value.replaceAll("'","'\\''")+"'";
let probe;
async function command(label,executable,args,cwd=evidence){
  return new Promise((resolve,reject)=>{
    const row={label,executable,args,cwd,started:new Date().toISOString()};record.commands.push(row);save();
    const child=spawn(executable,args,{cwd,env,stdio:['ignore','pipe','pipe']});let stdout='',stderr='';
    const timer=setTimeout(()=>child.kill('SIGKILL'),300000);
    child.stdout.on('data',bytes=>stdout+=bytes);child.stderr.on('data',bytes=>stderr+=bytes);
    child.on('error',error=>{clearTimeout(timer);reject(error);});
    child.on('close',(code,signal)=>{clearTimeout(timer);Object.assign(row,{code,signal,stdout,stderr,finished:new Date().toISOString()});save();resolve(row);});
  });
}
function files(folder){return fs.readdirSync(folder,{withFileTypes:true}).flatMap(entry=>entry.isDirectory()?files(path.join(folder,entry.name)):[path.join(folder,entry.name)]);}
async function stop(child){if(!child||child.exitCode!==null)return;const done=new Promise(resolve=>child.once('exit',resolve));child.kill('SIGINT');const timer=setTimeout(()=>child.kill('SIGKILL'),2000);await done;clearTimeout(timer);}
function testsIn(suite){return [...(suite.specs??[]).flatMap(spec=>spec.tests.map(test=>({...test,title:spec.title}))),...(suite.suites??[]).flatMap(testsIn)];}
function verifySuite(row,count){
  assert.equal(row.code,0,`${row.label} failed; inspect retained stdout/stderr`);
  const report=JSON.parse(row.stdout),tests=report.suites.flatMap(testsIn);
  assert.equal(report.stats.expected,count);assert.equal(report.stats.skipped,0);assert.equal(report.stats.unexpected,0);assert.equal(report.stats.flaky,0);
  assert.deepEqual(report.errors,[]);assert.equal(tests.length,count);
  for(const test of tests){
    assert.equal(test.expectedStatus,'passed');assert.equal(test.status,'expected');assert.equal(test.results.length,1);
    assert.equal(test.results[0].status,'passed');assert.equal(test.results[0].retry,0);
    for(const annotation of [...(test.annotations??[]),...(test.results[0].annotations??[])]){
      assert(!['skip','fixme','fail'].includes(annotation.type),`${test.title}: ${annotation.type} bypass`);
      if(annotation.type==='known-open')assert.match(annotation.description??'',/^WB-PERF-01 navigate-to-ready /,'No functional known-open bypass is permitted');
    }
  }
  return {stats:report.stats,tests:tests.map(test=>({title:test.title,status:test.results[0].status,annotations:[...(test.annotations??[]),...(test.results[0].annotations??[])]}))};
}
try {
  assert.equal(git(elements,['rev-parse','HEAD']),elementsSHA);
  assert.equal(git(elements,['status','--porcelain','--untracked-files=no']),'','Selected Elements source must be clean');
  record.elementsSHA=elementsSHA;record.obSHA=git(path.join(cohort,'ob'),['rev-parse','HEAD']);
  const binary=path.join(fs.realpathSync(platformResult.dir),'ob');
  assert(path.dirname(binary).startsWith(fs.realpathSync(os.tmpdir())+path.sep+'oas-platform-e2e-'),'Binary must come from preceding platform proof');
  assert.equal(hash(fs.readFileSync(binary)),platformResult.binaryHash);
  assert.equal(platformResult.sourceRevisions.ob,record.obSHA);
  const info=await command('binary-build-info','go',['version','-m',binary]);assert.equal(info.code,0);
  assert.equal(info.stdout,platformResult.buildInfo);assert(info.stdout.includes('vcs.revision='+record.obSHA));assert(!info.stdout.includes('vcs.modified=true'));
  for(const line of info.stdout.split('\n')){const fields=line.trim().split(/\s+/);if(fields[0]==='=>')assert(fields.length>=3&&/^v\d/.test(fields[2]),'Filesystem replacement in tested binary');}
  record.binary={path:binary,sha256:platformResult.binaryHash,buildInfo:info.stdout};
  const runnerRoot=fs.realpathSync(process.env.OB_PLATFORM_BROWSER_ROOT);
  const require=createRequire(path.join(runnerRoot,'package.json'));
  const playwright=require('@playwright/test');assert.equal(require('@playwright/test/package.json').version,'1.61.1');
  fs.symlinkSync(path.join(runnerRoot,'node_modules'),path.join(evidence,'node_modules'),'dir');
  for(const directory of ['ob-start','journeys'])for(const source of files(path.join(elements,'tests',directory))){
    const relative=path.relative(elements,source),destination=path.join(evidence,relative);fs.mkdirSync(path.dirname(destination),{recursive:true});fs.copyFileSync(source,destination);
    record.testInputs[relative]=hash(fs.readFileSync(source));assert.equal(hash(fs.readFileSync(destination)),record.testInputs[relative]);
  }
  const browser=await playwright.chromium.launch({headless:true});record.browser={version:browser.version(),playwright:'1.61.1',executable:playwright.chromium.executablePath()};await browser.close();

  // Inspect actual bytes served by this binary, never a replacement UI server.
  probe=spawn(binary,['start','--port','20394','--strict-port','--token','asset-provenance-token'],{cwd:evidence,env,stdio:['ignore','pipe','pipe']});
  let probeOutput='';probe.stdout.on('data',bytes=>probeOutput+=bytes);probe.stderr.on('data',bytes=>probeOutput+=bytes);
  for(let attempt=0;;attempt++){
    try{if((await fetch('http://127.0.0.1:20394/healthz')).ok)break;}catch{}
    if(attempt===200||probe.exitCode!==null)throw new Error('Asset probe failed to start: '+probeOutput);
    await new Promise(resolve=>setTimeout(resolve,50));
  }
  const dist=path.join(cohort,'ob/internal/server/workbench/dist');
  for(const source of files(dist)){
    const relative=path.relative(dist,source);const url='http://127.0.0.1:20394/'+(relative==='index.html'?'':relative.split(path.sep).map(encodeURIComponent).join('/'));
    const response=await fetch(url);assert.equal(response.status,200);const servedHash=hash(Buffer.from(await response.arrayBuffer())),sourceHash=hash(fs.readFileSync(source));assert.equal(servedHash,sourceHash,'Embedded bytes differ: '+relative);record.assets.push({path:relative,sourceHash,servedHash});
  }
  record.assetsSourceBuild='Binary-to-selected-OB-dist equality only; Elements source-to-dist rebuild provenance is a separate gate';
  await stop(probe);probe=null;record.assetProbeOutput=probeOutput;
  function config(suite){
    const journeys=suite==='journeys',port=journeys?20397:20391,token=journeys?'journey-token':'test-token';
    const servers=[{command:quote(binary)+` start --port ${port} --strict-port --token ${token}`,url:`http://127.0.0.1:${port}/healthz`,reuseExistingServer:false,timeout:120000}];
    if(!journeys)servers.push({command:quote(binary)+' demo --port 20392 --grpc-port 20393',url:'http://127.0.0.1:20392/api/menu',reuseExistingServer:false,timeout:120000});
    const value={testDir:path.join(evidence,'tests',suite),fullyParallel:false,workers:1,retries:0,forbidOnly:true,reporter:'json',timeout:60000,outputDir:path.join(evidence,'test-results',suite),use:{baseURL:`http://127.0.0.1:${port}`,trace:'retain-on-failure'},webServer:servers,...(journeys?{globalSetup:path.join(evidence,'tests/journeys/global-setup.ts')}:{})};
    const filename=path.join(evidence,suite+'.config.mjs');fs.writeFileSync(filename,'export default '+JSON.stringify(value,null,2)+';\n');return filename;
  }
  const cli=require.resolve('@playwright/test/cli');
  const workbench=await command('workbench-36',process.execPath,[cli,'test','--config',config('ob-start')]);
  const journeys=await command('journeys-5',process.execPath,[cli,'test','--config',config('journeys')]);
  const telemetryPath=path.join(evidence,'test-results/journeys-telemetry.jsonl');
  record.telemetry=fs.existsSync(telemetryPath)?fs.readFileSync(telemetryPath,'utf8').trim().split('\n').filter(Boolean).map(line=>JSON.parse(line)):[];
  const budgets=JSON.parse(fs.readFileSync(path.join(evidence,'tests/journeys/budgets.json')));
  // Preserve this distinction even when Playwright itself reports a failed
  // performance assertion; a red exit must not be mislabeled a security bug.
  record.performance=record.telemetry.filter(row=>budgets[row.reasonCode]?.kind==='perf');
  record.functionalTelemetryFailures=record.telemetry.filter(row=>budgets[row.reasonCode]?.kind!=='perf'&&row.outcome!=='pass');
  record.performanceWithinBudget=record.performance.length===2&&record.performance.every(row=>row.outcome==='pass');
  record.workbench=verifySuite(workbench,36);record.journeys=verifySuite(journeys,5);
  assert(record.workbench.tests.some(test=>test.title==='legacy workspace restores the draft but never restores credential authority'),'Seeded legacy proof was not executed');
  const expectedMoments=['navigate-to-ready','run-to-output','one-visible-workbench','wrong-token-pill','wrong-token-error-names-credentials','rail-gutter-arrow-resize','tab-strip-reachable','tab-strip-stop-budget'];
  assert.deepEqual(record.telemetry.map(row=>row.moment).sort(),expectedMoments.sort());
  for(const row of record.telemetry){
    const budget=budgets[row.reasonCode];assert(budget,'Unknown telemetry reason');
    if(budget.kind==='perf'){
      assert(['pass','over-budget'].includes(row.outcome),'Performance errors/failures cannot be bypassed');
      if(row.outcome==='over-budget')assert.equal(row.moment,'navigate-to-ready','Only the existing first-paint allowance is retained');
    }else assert.equal(row.outcome,'pass','Functional telemetry cannot be skipped or known-open: '+row.moment);
  }
  assert.equal(hash(fs.readFileSync(binary)),record.binary.sha256,'Tested binary changed during qualification');
  record.functionalPass=true;
  record.gatePassed=true;
  record.verdict=record.performanceWithinBudget?'PASS':'FUNCTIONAL_PASS_WITH_EXISTING_FIRST_PAINT_PERFORMANCE_ALLOWANCE';
}catch(error){record.error=String(error);record.stack=error.stack;record.functionalPass??=null;record.gatePassed=false;record.verdict='FAIL';process.exitCode=1;}finally{
  await stop(probe);record.finished=new Date().toISOString();save();console.log(JSON.stringify({evidence,verdict:record.verdict,functionalPass:record.functionalPass,performanceWithinBudget:record.performanceWithinBudget,error:record.error}));
}
