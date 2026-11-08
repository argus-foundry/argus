import {log} from '../core/api.js';
import {composePaste} from './terminal-policy.js';

function pause(signal,milliseconds) {
  return new Promise((resolve,reject)=>{
    const cancel=()=>{clearTimeout(timer);signal.removeEventListener('abort',cancel);reject(new Error('Attachment submit canceled'));};
    const timer=setTimeout(()=>{signal.removeEventListener('abort',cancel);resolve();},milliseconds);
    signal.addEventListener('abort',cancel,{once:true});if(signal.aborted)cancel();
  });
}
async function settlePaste(input,baseline) {
  const deadline=Date.now()+5000;let last=baseline,changed=Date.now();
  while(Date.now()<deadline){
    input.assertReady();const state=input.observe();
    if(state?.bytes!==last){last=state?.bytes;changed=Date.now();}
    if(Number.isSafeInteger(last)&&last>baseline&&!state.pasting&&Date.now()-changed>=100)return true;
    await pause(input.signal,25);
  }
  return false;
}

export async function submitAttachment(input,text) {
  const baseline=input.observe()?.bytes;
  await input.post(composePaste(text,input.bracketed()));
  log('terminal-submit','attachment paste written',{bytes:new TextEncoder().encode(text).length});
  const settled=await settlePaste(input,baseline);
  log('terminal-submit',settled?'attachment paste settled':'attachment paste settle bound reached');
  await input.verifyLive();
  await input.post('\r');
  // The PTY has no submission acknowledgment protocol. Never infer a retry from its screen.
  log('terminal-submit','attachment submit accepted');
}
