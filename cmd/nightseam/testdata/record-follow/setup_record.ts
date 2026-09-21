import assert from 'node:assert/strict';
import {MemoryWireLog} from '@nightseam/duplex';
import * as binding from '@example/history-binding';
import * as client from '@example/history-client';

const controller=new AbortController();
class CancelledHead extends MemoryWireLog{
 override async head(signal:AbortSignal):Promise<number>{controller.abort();signal.throwIfAborted();throw new Error('Head did not receive the setup signal');}
}
const values:number[]=[];
const wire=client.toWire(()=>({methods:{},events:{tick:value=>{values.push(value.value);}}}),{});
await assert.rejects(binding.record(wire,new CancelledHead(),{},{},{signal:controller.signal}),error=>error instanceof DOMException&&error.name==='AbortError');
const log=new MemoryWireLog();const recorder=await binding.record(wire,log,{},{});
try{
 await assert.rejects(recorder.append({name:'tick',data:{value:'invalid' as unknown as number}}));
 assert.equal(await recorder.head(),0);assert.deepEqual(values,[]);
 await recorder.append({name:'tick',data:{value:7}});assert.equal(await recorder.head(),1);
 const deadline=Date.now()+5000;while(values.length===0){assert.ok(Date.now()<deadline);await new Promise(resolve=>setTimeout(resolve,1));}
 assert.deepEqual(values,[7]);
}finally{recorder.close();}
