// Temporary private compatibility relocation. No evaluator semantics change.
// This preserves the CLI's released Gnata v0.2.2 Graph consumer while the
// ordinary Core-transform entry point qualifies its separate candidate.
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const module='github.com/recolabs/gnata',version='v0.2.2';
const metadata=JSON.parse(execFileSync('go',['mod','download','-json',module+'@'+version],{cwd:root,env:{...process.env,GOWORK:'off'},encoding:'utf8'}));
if(metadata.Error || !metadata.Dir || !fs.readFileSync(path.join(root,'go.sum'),'utf8').includes(`${module} ${version} ${metadata.Sum}\n`)) throw Error('Legacy module identity is not in the CLI lockfile');
const target=path.join(root,'internal/graphjsonata');
const prefix='github.com/openbindings/ob/internal/graphjsonata';
const hash=b=>createHash('sha256').update(b).digest('hex');
const inputs={},outputs={},files={};
function visit(dir,relative='') {
  for(const item of fs.readdirSync(dir,{withFileTypes:true})) {
    const rel=path.join(relative,item.name);
    if(item.isDirectory()) { if(['functions','internal'].includes(rel)||relative.startsWith('internal'))visit(path.join(dir,item.name),rel); }
    else if(item.name==='LICENSE'||item.name.endsWith('.go')&&!item.name.endsWith('_test.go')) {
      const raw=fs.readFileSync(path.join(dir,item.name));inputs[rel]=hash(raw);
      const data=raw.toString().replaceAll('"'+module+'/', '"'+prefix+'/');
      files[rel]=data;outputs[rel]=hash(data);
    }
  }
}
visit(metadata.Dir);
const previous=path.join(target,'SOURCE.json');
if(fs.existsSync(previous))for(const [file,digest]of Object.entries(JSON.parse(fs.readFileSync(previous)).outputs))if(hash(fs.readFileSync(path.join(target,file)))!==digest)throw Error('Modified legacy relocation: '+file);
for(const [rel,data]of Object.entries(files)){fs.mkdirSync(path.dirname(path.join(target,rel)),{recursive:true});fs.writeFileSync(path.join(target,rel),data);}
const manifest={module,version,sum:metadata.Sum,goModSum:metadata.GoModSum,purpose:'Private unchanged Graph runtime; not official fidelity evaluator or independent product',inputs,outputs};
fs.writeFileSync(previous,JSON.stringify(manifest,null,2)+'\n');
console.log(JSON.stringify({module,version,sum:metadata.Sum,files:Object.keys(files).length,sourceManifest:previous}));
