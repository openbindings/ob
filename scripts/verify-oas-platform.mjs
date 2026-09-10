import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import http from 'node:http';
import net from 'node:net';
import assert from 'node:assert/strict';
import {spawn,execFileSync} from 'node:child_process';
import {createHash,randomBytes} from 'node:crypto';
import {createRequire} from 'node:module';
// This normal-process proof may create normal user configuration: run only on a disposable CI runner.
assert.equal(process.env.GITHUB_ACTIONS,'true','Use a disposable hosted runner, never a developer account');
const cohort=fs.realpathSync(process.argv[2]);
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'oas-platform-e2e-'));
const hash=raw=>createHash('sha256').update(raw).digest('hex');
const env={...process.env,GOTOOLCHAIN:process.env.GOTOOLCHAIN || 'go1.25.13',GOWORK:'off',OB_NO_UPDATE_CHECK:'1',OB_TEST_NO_CREDENTIAL_STORE:'1',OB_CREDENTIALS_FILE:path.join(dir,'credentials.json')};
const logs=[];let active,peer,browser;
function command(executable,args,cwd,input) {
  return new Promise((resolve,reject)=>{
    const child=spawn(executable,args,{cwd,env,stdio:['pipe','pipe','pipe']});let stdout='',stderr='';
    const timer=setTimeout(()=>child.kill('SIGKILL'),120000);
    child.stdout.on('data',v=>stdout+=v);child.stderr.on('data',v=>stderr+=v);child.on('error',reject);
    child.on('close',(code,signal)=>{clearTimeout(timer);logs.push({executable,args,cwd,code,signal,stdout,stderr});if(code===0)resolve(stdout);else reject(Error(JSON.stringify(logs.at(-1))))});
    child.stdin.end(input);
  });
}
function checkValue(raw,computed=false){
  assert.match(raw,/"id"\s*:\s*9223372036854775807\s*[,}]/);
  assert.match(raw,/"amount"\s*:\s*0\.12345678901234567890123456789\s*[,}]/);
  assert.match(raw,/"huge"\s*:\s*1e\+?400\s*[,}]/);
  assert.match(raw,/"tiny"\s*:\s*1e-400\s*[,}]/);
  const parsed=JSON.parse(raw);const value=parsed.kind==='output'?parsed.value:parsed;
  assert.equal(value.label,'😀 e\u0301');assert.equal(value.isLosslessNumber,true);
  if(computed)assert.match(raw,/"computed"\s*:\s*0\.3\s*[,}]/);
}
async function frames(url,token,iface,inputJSON) {
  return new Promise((resolve,reject)=>{
    const socket=new WebSocket(url,['openbindings.frames.v1','openbindings.bearer.'+Buffer.from(token).toString('base64url')]);
    const received=[];const timer=setTimeout(()=>{socket.close();reject(Error('frame deadline'))},5000);
    socket.addEventListener('error',()=>{clearTimeout(timer);reject(Error('websocket error'))});
    socket.addEventListener('open',()=>{socket.send(JSON.stringify({kind:'open',input:{interface:iface,operation:'echo'}}));socket.send('{"kind":"input","value":'+inputJSON+'}');socket.send('{"kind":"close"}')});
    socket.addEventListener('message',event=>{const raw=String(event.data);received.push(raw);const frame=JSON.parse(raw);if(frame.kind==='complete'||frame.kind==='error'){clearTimeout(timer);socket.close();resolve(received)}});
  });
}
try {
  const binary=path.join(dir,process.platform==='win32'?'ob.exe':'ob');
  if(process.platform==='win32')await command('go',['build','-mod=readonly','-o',binary,'./cmd/ob'],path.join(cohort,'ob'));
  else {
    env.OB_OUT=binary;env.OB_BIN_DIR=path.join(dir,'bin');env.OB_SKIP_WORKBENCH='1';env.OB_LOCAL_WORKSPACE='0';env.GOFLAGS='-mod=readonly';
    await command('bash',['scripts/dev-install.sh'],path.join(cohort,'ob'));
    assert.equal(fs.realpathSync(path.join(env.OB_BIN_DIR,'ob')),fs.realpathSync(binary));
    await command(path.join(env.OB_BIN_DIR,'ob'),['--help'],dir);
  }
  const payload='{"id":9223372036854775807,"amount":0.12345678901234567890123456789,"huge":1e400,"tiny":1e-400,"label":"😀 e\\u0301","isLosslessNumber":true}';
  const captured=[];
  peer=http.createServer(async(req,res)=>{let body='';for await(const chunk of req)body+=chunk;captured.push(body);res.setHeader('Content-Type','application/json');res.end(payload)});
  await new Promise(resolve=>peer.listen(0,'127.0.0.1',resolve));const peerURL='http://127.0.0.1:'+peer.address().port;
  const reservation=net.createServer();await new Promise(resolve=>reservation.listen(0,'127.0.0.1',resolve));const port=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
  const token=randomBytes(24).toString('hex');
  active=spawn(binary,['start','--port',String(port),'--strict-port'],{cwd:dir,env:{...env,OB_START_TOKEN:token},stdio:['ignore','pipe','pipe']});
  let serverOutput='';active.stdout.on('data',v=>serverOutput+=v);active.stderr.on('data',v=>serverOutput+=v);
  const base='http://127.0.0.1:'+port;
  for(let i=0;;i++){try {const ready=await fetch(base+'/healthz');if(ready.ok)break;}catch{}if(i===100)throw Error('ob start did not become ready: '+serverOutput);await new Promise(resolve=>setTimeout(resolve,50))}
  const schema={type:'object',properties:{id:{type:'integer'},amount:{type:'number'},huge:{type:'number'},tiny:{type:'number'},label:{type:'string'},isLosslessNumber:{type:'boolean'}},required:['id','amount','huge','tiny','label','isLosslessNumber']};
  const results=[];
  const automation=createRequire(path.join(process.env.OB_PLATFORM_BROWSER_ROOT,'package.json'))('playwright');
  browser=await automation.chromium.launch({headless:true});
  async function workbench(obi,input) {
    const page=await browser.newPage({viewport:{width:1760,height:1000}});
    const errors=[],messages=[];
    page.on('pageerror',error=>errors.push(error.message));
    page.on('websocket',socket=>socket.on('framereceived',frame=>messages.push(String(frame.payload))));
    try {
      await page.goto(base+'/#token='+token);
      await page.waitForFunction(()=>document.querySelector('#connection-status-text')?.textContent==='Ready');
      await page.locator('#acquire-open').click();
      await page.locator('#acquire-file').setInputFiles(obi);
      await page.locator('#acquire-replace').click();
      await page.locator('#acquire-dialog').waitFor({state:'hidden'});
      const operation=page.locator('ob-obi-explorer').locator('[part~="operation"]').filter({has:page.locator('.operation-key:text-is("echo")')});
      await operation.click();
      const workbench=page.locator('ob-operation-workbench:not([hidden])');
      await workbench.getByRole('textbox',{name:'Input for echo as JSON',exact:true}).fill(input);
      await page.locator('#sheet-run').click();
      await page.waitForFunction(()=>document.querySelector('#sheet-status')?.textContent?.includes('1 value'));
      const output=await workbench.locator('[part~="output"]').evaluate(element=>element.text);
      checkValue(output,true);
      assert.equal(await workbench.locator('.error').isVisible(),false);
      const exactFrames=messages.filter(raw=>raw.includes('9223372036854775807')&&JSON.parse(raw).kind==='output');
      assert(exactFrames.length>0,'no independently captured exact workbench output frame');
      checkValue(exactFrames.at(-1),true);
      assert.deepEqual(errors,[]);
      return {browser:browser.version(),inputEditor:true,rawResponse:true,outputEditor:true,errors};
    } catch(error) {
      logs.push({workbench:obi,error:String(error),body:await page.locator('body').innerText(),messages});throw error;
    } finally {await page.close();}
  }
  for(const version of ['2.0','3.0.4','3.1.1','3.2.0']) {
    const edition=version.slice(0,3),spec='openbindings.openapi-'+edition+'@1';
    const doc=version==='2.0'?{swagger:version,info:{title:'Exact peer',version:'1'},host:new URL(peerURL).host,schemes:['http'],consumes:['application/json'],produces:['application/json'],paths:{'/echo':{post:{operationId:'echo',parameters:[{in:'body',name:'body',required:true,schema}],responses:{200:{description:'ok',schema}}}}}}:{openapi:version,info:{title:'Exact peer',version:'1'},servers:[{url:peerURL}],paths:{'/echo':{post:{operationId:'echo',requestBody:{required:true,content:{'application/json':{schema}}},responses:{200:{description:'ok',content:{'application/json':{schema}}}}}}}};
    const artifact=path.join(dir,'openapi-'+version+'.json'),obi=path.join(dir,'interface-'+version+'.json');fs.writeFileSync(artifact,JSON.stringify(doc));
    await command(binary,['synthesize',spec+':'+artifact,'-o',obi],dir);
    const iface=JSON.parse(fs.readFileSync(obi));iface.transforms={...iface.transforms,exactOutput:'$ ~> |$|{"computed":0.1+0.2}|'};
    for(const binding of Object.values(iface.bindings))if(binding.operation==='echo')binding.outputTransform={$ref:'#/transforms/exactOutput'};
    fs.writeFileSync(obi,JSON.stringify(iface));
    const input=version==='2.0'?'{"body":'+payload+'}':payload;
    const ordinary=await command(binary,['op','invoke',obi,'echo','--input','-'],dir,input);checkValue(ordinary.trim(),true);
    const wire=await frames(base.replace('http:','ws:')+'/operations/invoke',token,iface,input);
    assert.equal(JSON.parse(wire.at(-1)).kind,'complete');const outputs=wire.filter(raw=>JSON.parse(raw).kind==='output');assert.equal(outputs.length,1);
    checkValue(outputs[0],true);
    const before=captured.length;
    const invalid=structuredClone(iface);invalid.transforms.exactOutput='{"bad":[function(){1}]}';
    const failure=await frames(base.replace('http:','ws:')+'/operations/invoke',token,invalid,input);
    assert.equal(JSON.parse(failure.at(-1)).kind,'error');assert(!failure.some(raw=>JSON.parse(raw).kind==='output'));assert.equal(captured.length,before+1);
    const beforeUI=captured.length;
    const ui=await workbench(obi,input);assert.equal(captured.length,beforeUI+1);
    results.push({version,cli:true,obStart:true,workbench:ui,namedOutputTransform:true,invalidResultRejected:true,frames:wire});
  }
  for(const raw of captured)checkValue(raw);
  const buildInfo=await command('go',['version','-m',binary],dir);
  const sourceRevisions={ob:execFileSync('git',['rev-parse','HEAD'],{cwd:path.join(cohort,'ob'),encoding:'utf8'}).trim()};
  assert(buildInfo.includes('vcs.revision='+sourceRevisions.ob),'binary VCS provenance differs from checkout');
  assert(!buildInfo.includes('vcs.modified=true'),'binary built from modified source');
  for(const line of buildInfo.split('\n')) {
    const fields=line.trim().split(/\s+/);
    if(fields[0]==='=>')assert(fields.length>=3 && /^v\d/.test(fields[2]),'filesystem replacement in binary: '+line);
  }
  const record={dir,sourceRevisions,buildInfo,binaryHash:hash(fs.readFileSync(binary)),host:{platform:process.platform,arch:process.arch,normalProcess:true,normalConfig:true,sourceOverlay:false,installed:process.platform!=='win32'},results,requests:captured.length,serverOutput,logs};
  fs.writeFileSync(path.join(dir,'RESULTS.json'),JSON.stringify(record,null,2)+'\n');console.log(JSON.stringify({...record,logs:undefined,serverOutput:undefined}));
}catch(error){fs.writeFileSync(path.join(dir,'FAILURE.json'),JSON.stringify({dir,error:String(error),stack:error.stack,logs},null,2));console.error(dir,error);process.exitCode=1;}
finally {
  if(browser)await browser.close();
  if(active&&active.exitCode===null){const done=new Promise(resolve=>active.once('exit',resolve));active.kill('SIGINT');const timer=setTimeout(()=>active.kill('SIGKILL'),2000);await done;clearTimeout(timer);}
  if(peer)await new Promise(resolve=>peer.close(resolve));
}
