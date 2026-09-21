import assert from 'node:assert/strict';
import './live_record.ts';
import './setup_record.ts';
import {readFileSync} from 'node:fs';
import { MemoryWireLog } from '@nightseam/duplex';
import { DuplexError } from '@nightseam/runtime';
import * as binding from '@example/history-binding';
import * as client from '@example/history-client';
import * as foreignBinding from '@example/foreign-binding';
import * as foreignClient from '@example/foreign-client';

function target(){const values:number[]=[];const wire=client.toWire(()=>({methods:{},events:{tick:input=>{values.push(input.value);}}}),{});return {wire,values};}
async function until(test:()=>boolean){const end=Date.now()+5000;while(!test()){assert.ok(Date.now()<end,'delivery deadline');await new Promise(resolve=>setTimeout(resolve,1));}}
const main=target(),store=new MemoryWireLog();const recorded=await binding.record(main.wire,store,{},{});
await recorded.append({name:'tick',data:{value:1}});await recorded.append({name:'tick',data:{value:2}});assert.equal(await recorded.head(),2);await until(()=>main.values.length===2);assert.deepEqual(main.values,[1,2]);
const second=target();const follower=await recorded.follow(1,second.wire);assert.equal(follower.head,2);await recorded.append({name:'tick',data:{value:3}});await until(()=>second.values.length===2);assert.deepEqual(second.values,[2,3]);
if(false){
 // @ts-expect-error the outgoing tick payload is a record, not the opposite string.
 await recorded.append({name:'tick',data:'wrong side'});
 // @ts-expect-error undeclared event names cannot enter the event union.
 await recorded.append({name:'missing',data:{value:1}});
}
const reverseValues:string[]=[];const back=binding.toWire(()=>({methods:{subscribe:input=>input.after},events:{tick:value=>{reverseValues.push(value);}}}),{});const reverse=await client.record(back,new MemoryWireLog(),{},{});await reverse.append({name:'tick',data:'reverse'});await until(()=>reverseValues.length===1);assert.deepEqual(reverseValues,['reverse']);
const rejectedValues:number[]=[];const foreign=foreignClient.toWire(()=>({methods:{},events:{tick:input=>{rejectedValues.push(input.value);}}}),{});const rejectedStore=new MemoryWireLog();const mismatch=(error:unknown)=>error instanceof DuplexError&&error.code==='contract_mismatch';
await assert.rejects(binding.record(foreign,rejectedStore,{},{}),mismatch);assert.equal(await rejectedStore.head(new AbortController().signal),0);await assert.rejects(recorded.follow(0,foreign),mismatch);assert.deepEqual(rejectedValues,[]);
const accepted=await foreignBinding.record(foreign,new MemoryWireLog(),{},{});await accepted.append({name:'tick',data:{value:9}});await until(()=>rejectedValues.length===1);assert.deepEqual(rejectedValues,[9]);
accepted.close();reverse.close();follower.close();await follower.done;recorded.close();

function deferred(){let resolve!:()=>void;const promise=new Promise<void>(r=>{resolve=r;});return {promise,resolve};}
class HeldLog extends MemoryWireLog {
 entered=deferred();release=deferred();
 override async read(sequence:number,signal:AbortSignal){this.entered.resolve();await Promise.race([this.release.promise,new Promise<never>((_,reject)=>signal.addEventListener('abort',()=>reject(signal.reason),{once:true}))]);return super.read(sequence,signal);}
}
const table=JSON.parse(readFileSync(new URL('./recorded-wire.json',import.meta.url),'utf8')) as {cases:Array<{name:string;before:number[];during:number[];expected:number[];after:number;head:number}>};
for(const row of table.cases){
 const source=target(),store=new HeldLog();const record=await binding.record(source.wire,store,{maxQueuedMessages:8},{});try{
 for(const value of row.before)await record.append({name:'tick',data:{value}});const subscriber=target();const follow=await record.follow(row.after,subscriber.wire);assert.equal(follow.head,row.head);await store.entered.promise;
 for(const value of row.during)await record.append({name:'tick',data:{value}});assert.equal(await record.head(),row.before.length+row.during.length);store.release.resolve();await until(()=>subscriber.values.length===row.expected.length);assert.deepEqual(subscriber.values,row.expected);
 await record.append({name:'tick',data:{value:99}});await until(()=>subscriber.values.length>row.expected.length);assert.deepEqual(subscriber.values,[...row.expected,99]);follow.close();await follow.done;
 }finally{record.close();}
}
