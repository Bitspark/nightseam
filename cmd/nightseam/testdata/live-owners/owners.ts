import assert from 'node:assert/strict';
import { pipe } from '@nightseam/duplex';
import { DuplexPeer, DuplexError } from '@nightseam/runtime';
import { liveOver, type LiveScope } from '@nightseam/live';
import * as combinator from './api/ts/combinator-client/src/index.ts';
import * as owners from './api/ts/owners-client/src/index.ts';
import * as worker from './api/ts/worker-client/src/index.ts';

async function pair(imports: number) {
  const [a,b]=pipe();
  const pa=new DuplexPeer({role:'client'}),pb=new DuplexPeer({role:'server'});
  const sa=liveOver(pa,{maxExports:4,maxImports:4}),sb=liveOver(pb,{maxExports:4,maxImports:imports});
  await Promise.all([pa.attach(a),pb.attach(b)]);
  return {sa,sb,close:()=>{pa.close();pb.close();}};
}
async function zero(...scopes: LiveScope[]) {
  const end=Date.now()+5000;
  for(const scope of scopes)while(scope.counts().exports||scope.counts().imports){assert(Date.now()<end,'owner release leaked '+JSON.stringify(scope.counts()));await new Promise(resolve=>setTimeout(resolve,1));}
}
const isCode=(code:string)=>(error:unknown)=>error instanceof DuplexError&&error.code===code;

{
  const {sa,sb,close}=await pair(4);
  try {
    for(let i=0;i<7;i++){
      let a=sa.owner().child(),b=sb.owner().child();
      const job:worker.Job={ticket:'job',cancel:async()=>{}};
      const page=owners.importJobs(b,owners.exportJobs(a,{items:[job,job]}));
      for(const job of page.items)await job.cancel();
      assert.equal(sa.counts().exports,2,'native identity must not deduplicate exports');assert.equal(sb.counts().imports,2);
      b.release();await assert.rejects(()=>page.items[0]!.cancel(),isCode('reference_released'));a.release();await zero(sa,sb);
      a=sa.owner().child();b=sb.owner().child();
      const bundle:combinator.Bundle<worker.Job>={metadata:{seed:job},run:async n=>n+1};
      const raw=combinator.exportBundle(a,bundle,worker.exportJob);
      const got=combinator.importBundle(b,raw,worker.importJob);
      await got.metadata.seed.cancel();assert.equal(await got.run(3),4);
      b.release();a.release();await zero(sa,sb);
    }
  } finally {close();}
}
{
  const {sa,sb,close}=await pair(1);
  try {
    for(const borrow of [false,true]){
      const exporter=sa.owner().child();
      const raw=combinator.exportBundle(exporter,{metadata:{seed:async()=>{}},run:async(n:number)=>n},worker.exportReport);
      const held=sb.owner().child(),batch=sb.owner().child();
      const retained=borrow?worker.importReport(held,(raw as {metadata:{seed:unknown}}).metadata.seed):undefined;
      assert.throws(()=>combinator.importBundle(batch,raw,worker.importReport),isCode('too_many_imports'));
      assert.deepEqual(batch.counts(),{exports:0,imports:0});assert.equal(sb.counts().imports,borrow?1:0);
      batch.release();if(retained)await retained(7);
      held.release();exporter.release();await zero(sa,sb);
    }
  } finally {close();}
}
