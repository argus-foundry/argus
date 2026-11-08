import {notice} from './dom.js';
import {log} from '../core/api.js';
export async function copyNotice(value, success='Copied.', {status=notice,signal}={}) {
 try {await navigator.clipboard.writeText(value);if(!signal?.aborted)status(success);log('clipboard','copied',{status:true});return true;}
 catch {if(!signal?.aborted)status('Copy failed. Select the value and copy it manually.',true);log('clipboard','copy failed',{status:false});return false;}
}
