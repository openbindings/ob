import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
import {spawn,execFileSync} from 'node:child_process';

// Corroboration only: public outages and service/schema drift are recorded,
// never turned into a replacement for the deterministic or historical corpus.
assert.equal(process.env.GITHUB_ACTIONS,'true','Use a disposable hosted account');
const platform=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
const evidence=path.resolve(process.argv[3]);assert(!fs.existsSync(evidence));fs.mkdirSync(evidence,{recursive:true});
const binary=path.join(platform.dir,process.platform==='win32'?'ob.exe':'ob');
const hash=bytes=>createHash('sha256').update(bytes).digest('hex');
assert.equal(hash(fs.readFileSync(binary)),platform.binaryHash);
assert.equal(execFileSync('go',['version','-m',binary],{encoding:'utf8'}),platform.buildInfo);
const record={started:new Date().toISOString(),binaryHash:platform.binaryHash,obSHA:platform.sourceRevisions.ob,maxExternalGETs:30,usedBudget:2,priorGETs:2,priorRun:'34533162907: one PokeAPI artifact GET before harness E2BIG. 34533848052: one artifact GET before harness outputLocation spelling error; no API invocation.',requests:[],commands:[],sources:[]};
const save=()=>fs.writeFileSync(path.join(evidence,'RESULTS.json'),JSON.stringify(record,null,2)+'\n');
function reserve(count){assert(record.usedBudget+count<=30,'External request budget exceeded');record.usedBudget+=count;save();}
async function get(url){
  reserve(1);const request={url,at:new Date().toISOString()};record.requests.push(request);
  try{const response=await fetch(url,{redirect:'manual',signal:AbortSignal.timeout(20000)});const bytes=Buffer.from(await response.arrayBuffer());Object.assign(request,{status:response.status,sha256:hash(bytes),length:bytes.length});save();return {status:response.status,text:bytes.toString('utf8')};}
  catch(error){request.error=String(error);save();return null;}
}
async function command(name,args,network=false,stdin){
  // Each command is a fresh process and client. The selected native client uses
  // RedirectManual; embedded input has no external references. Reserve TWO GETs
  // per invocation conservatively, not a claim of server-observed wire counts.
  if(network)reserve(2);
  return new Promise((resolve,reject)=>{
    const row={name,args,network,reservedGETs:network?2:0,...(stdin===undefined?{}:{stdinBytes:Buffer.byteLength(stdin),stdinSHA256:hash(stdin)})};record.commands.push(row);save();
    const child=spawn(binary,args,{cwd:evidence,env:{...process.env,OB_NO_UPDATE_CHECK:'1',OB_TEST_NO_CREDENTIAL_STORE:'1',OB_CREDENTIALS_FILE:path.join(evidence,'credentials.json')},stdio:[stdin===undefined?'ignore':'pipe','pipe','pipe']});
    let stdout='',stderr='';const timer=setTimeout(()=>child.kill('SIGKILL'),30000);
    child.stdout.on('data',b=>stdout+=b);child.stderr.on('data',b=>stderr+=b);child.on('error',reject);
    child.on('close',(code,signal)=>{clearTimeout(timer);Object.assign(row,{code,signal,stdout,stderr});save();resolve(row);});
    if(stdin!==undefined){child.stdin.on('error',error=>{row.stdinError=String(error);save();});child.stdin.end(stdin);}
  });
}
function externalReferences(text){
  // Read-only closure preflight, not a YAML parser. Refuse ambiguous spellings
  // and any non-fragment reference rather than permit unbudgeted acquisition.
  if(text.trimStart().startsWith('{')){
    const bad=[];const visit=value=>{if(!value||typeof value!=='object')return;for(const [key,member]of Object.entries(value)){if(key==='$ref'&&(typeof member!=='string'||!member.startsWith('#')))bad.push(member);else visit(member);}};
    visit(JSON.parse(text));return bad;
  }
  const occurrences=[...text.matchAll(/["']?\$ref["']?\s*:\s*(?:"([^"]*)"|'([^']*)'|([^\s,}\]]+))/g)];
  if(occurrences.length!==[...text.matchAll(/\$ref/g)].length)return ['ambiguous YAML reference spelling'];
  return occurrences.map(match=>match[1]??match[2]??match[3]).filter(value=>!value.startsWith('#'));
}
const sources=[
  ['poke','openbindings.openapi-3.1@1','https://raw.githubusercontent.com/PokeAPI/pokeapi/refs/heads/master/openapi.yml'],
  ['weather','openbindings.openapi-3.1@1','https://raw.githubusercontent.com/open-meteo/open-meteo/main/openapi/forecast.yml'],
  ['petstore','openbindings.openapi-3.0@1','https://petstore3.swagger.io/api/v3/openapi.json'],
];
try{
  const available={};
  for(const [name,expectedBindingSpec,url]of sources){
    const response=await get(url);const item={name,url,status:response?.status};record.sources.push(item);
    if(!response||response.status!==200){item.disposition='INCONCLUSIVE_ACQUISITION';continue;}
    fs.writeFileSync(path.join(evidence,name+'.source'),response.text);
    const advertised=response.text.trimStart().startsWith('{')?JSON.parse(response.text).openapi:response.text.match(/^openapi:\s*["']?(3\.[012]\.\d+)/m)?.[1];
    const edition=typeof advertised==='string'?advertised.match(/^(3\.[012])\./)?.[1]:undefined;
    if(!edition){item.disposition='INCONCLUSIVE_UNRECOGNIZED_EDITION';continue;}
    const bindingSpec='openbindings.openapi-'+edition+'@1';Object.assign(item,{advertised,bindingSpec,expectedBindingSpec});
    const refs=externalReferences(response.text);item.externalReferences=refs;
    if(refs.length){item.disposition='INCONCLUSIVE_UNBUDGETED_REFERENCE_CLOSURE';continue;}
    const obi=path.join(evidence,name+'.obi.json');
    const source={bindingSpec,location:url,content:response.text};
    // Existing stdin artifact lane preserves the exact fetched bytes and the
    // original source base without exceeding the OS's per-argument limit.
    // ParseSource options carry literal values, not URLSearchParams decoding.
    // These fixed source URLs have no '&' separator; assert before passing them.
    assert(!url.includes('&'));
    const synthesis=await command(name+'-synthesize',['synthesize',bindingSpec+':-?outputLocation='+url,'-o',obi],false,response.text);
    if(synthesis.code!==0){item.disposition='SYNTHESIS_REFUSAL_REQUIRES_CLASSIFICATION';continue;}
    const validation=await command(name+'-validate',['validate',obi]);
    item.disposition=validation.code===0?'SYNTHESIZED_AND_VALIDATED':'VALIDATION_FAILURE';
    const generated=JSON.parse(fs.readFileSync(obi,'utf8'));
    item.operations=Object.keys(generated.operations??{});item.sourceBases=Object.values(generated.sources??{}).map(value=>value.location);
    assert(item.sourceBases.includes(url),'Source base lost');available[name]={obi,source,generated};
  }
  if(available.poke){
    for(const [name,operation,input]of [['poke-page','pokemon_list',{limit:2}],['poke-next-page','pokemon_list',{limit:2,offset:2}],['poke-detail-strict','pokemon_retrieve',{id:'pikachu'}]]){
      if(available.poke.generated.operations?.[operation])await command(name,['op','invoke',available.poke.obi,operation,'--input',JSON.stringify(input)],true);
    }
    const binding=Object.entries(available.poke.generated.bindings??{}).find(([,value])=>value.operation==='pokemon_retrieve');
    if(binding)await command('poke-detail-explicit-raw',['binding','invoke',available.poke.obi,binding[0],'--input','{"id":"pikachu"}'],true);
    // Import the exact fetched raw artifact through the real embedded UI. No
    // public request is needed for this file import; its retrieval hash is above.
    const app=spawn(binary,['start','--port','20399','--strict-port','--token','public-corroboration-token'],{cwd:evidence,env:{...process.env,OB_NO_UPDATE_CHECK:'1',OB_TEST_NO_CREDENTIAL_STORE:'1',OB_CREDENTIALS_FILE:path.join(evidence,'credentials.json')},stdio:['ignore','pipe','pipe']});
    let appOutput='';app.stdout.on('data',b=>appOutput+=b);app.stderr.on('data',b=>appOutput+=b);let browser;
    try{
      for(let n=0;;n++){try{if((await fetch('http://127.0.0.1:20399/healthz')).ok)break;}catch{}if(n===200||app.exitCode!==null)throw Error('public UI startup: '+appOutput);await new Promise(resolve=>setTimeout(resolve,50));}
      const automation=createRequire(path.join(process.env.OB_PLATFORM_BROWSER_ROOT,'package.json'))('playwright');browser=await automation.chromium.launch({headless:true});const page=await browser.newPage();const errors=[];page.on('pageerror',error=>errors.push(error.message));
      await page.goto('http://127.0.0.1:20399/#token=public-corroboration-token');await page.waitForFunction(()=>document.querySelector('#connection-status-text')?.textContent==='Ready');
      await page.locator('#acquire-open').click();await page.locator('#acquire-file').setInputFiles({name:'pokeapi.yml',mimeType:'text/yaml',buffer:fs.readFileSync(path.join(evidence,'poke.source'))});await page.locator('#acquire-replace').click();await page.locator('#acquire-dialog').waitFor({state:'hidden'});
      const operation=page.locator('ob-obi-explorer').locator('.operation-key').getByText('pokemon_list',{exact:true});await operation.waitFor({state:'visible'});await operation.click();await page.locator('#sheet-run').waitFor({state:'visible'});assert.deepEqual(errors,[]);record.publicWorkbench={rawArtifactImport:true,operationExploration:true,browser:browser.version(),errors};
    }finally{if(browser)await browser.close();if(app.exitCode===null){const done=new Promise(resolve=>app.once('exit',resolve));app.kill('SIGINT');const timer=setTimeout(()=>app.kill('SIGKILL'),2000);await done;clearTimeout(timer);}record.publicWorkbenchLog=appOutput;save();}
  }
  if(available.weather){
    const binding=Object.values(available.weather.generated.bindings??{}).find(value=>value.operation==='v1.forecast.get');
    if(binding)await command('weather-explicit-public-server',['binding','invoke','--input',JSON.stringify({source:available.weather.source,selector:binding.selector,input:{parameters:{latitude:52.52,longitude:13.41,forecast_days:1}},context:{configuration:{server:{url:'https://api.open-meteo.com'}}}})],true);
  }
  if(available.petstore&&available.petstore.generated.operations?.getOrderById)await command('petstore-read-only-order',['op','invoke',available.petstore.obi,'getOrderById','--input','{"orderId":10}'],true);
  for(const [name,url]of [['non-oas','https://pokeapi.co/api/v2/pokemon/pikachu'],['missing','https://raw.githubusercontent.com/open-meteo/open-meteo/main/openapi.yml']]){
    const response=await get(url);if(!response)continue;
    assert(!url.includes('&'));
    const row=await command(name+'-rejection',['synthesize','openbindings.openapi-3.1@1:-?outputLocation='+url],false,response.text);
    assert.notEqual(row.code,0,name+' unexpectedly synthesized');
  }
  record.verdict='COLLECTED_REQUIRES_PRIMARY_CLASSIFICATION';record.budgetAccounting='Direct fetches counted; each CLI invocation reserves two GETs, no redirects, no external refs, update checks disabled. Not server-observed counts.';
}catch(error){record.error=String(error);record.verdict='FAILED';process.exitCode=1;}finally{record.finished=new Date().toISOString();save();console.log(JSON.stringify({evidence,verdict:record.verdict,usedBudget:record.usedBudget,sources:record.sources.map(({name,status,disposition})=>({name,status,disposition})),commands:record.commands.map(({name,code})=>({name,code})),error:record.error}));}
