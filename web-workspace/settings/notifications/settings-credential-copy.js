import {copyNotice} from '../../shared/clipboard.js';
export function copyCredential(value,status,signal){return copyNotice(value,'Copied. Treat the clipboard contents as a credential.',{status,signal});}
