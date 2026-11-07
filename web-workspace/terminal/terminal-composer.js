// Unknown layouts stay unknown so output activity cannot justify an Enter retry.
export function terminalComposerDraft(term) {
  const buffer=term?.buffer?.active;
  if(!buffer||!term.rows)return null;
  const lines=Array.from({length:term.rows},(_,row)=>buffer.getLine(buffer.baseY+row));
  let cursor=Math.max(0,Math.min(term.rows-1,buffer.cursorY)),prompt=-1,prefix='';
  const find=row=>lines[row]?.translateToString(true).match(/^\s*❯(?:[ \u00a0]|$)/)?.[0];
  for(let row=cursor;row>=0;row--){const match=find(row);if(match){prompt=row;prefix=match;break;}}
  if(prompt<0)for(let row=term.rows-1;row>cursor;row--){const match=find(row);if(match){prompt=row;prefix=match;cursor=row;break;}}
  if(prompt<0)return null;
  const parts=[],cells=[];
  for(let row=prompt;row<=cursor;row++){
    const line=lines[row],start=row===prompt?prefix.length:0;
    parts.push((line?.translateToString(true)||'').slice(start).trimEnd());
    for(let col=start;col<term.cols;col++){
      const cell=line?.getCell(col);
      if(cell?.getChars().trim())cells.push(cell);
    }
  }
  if(cells.length&&cells.every(cell=>cell.isDim()))return '';
  return parts.join('\n').trim();
}

export function terminalPastePending(term) {
  const buffer=term?.buffer?.active;
  if(!buffer)return false;
  for(let row=0;row<term.rows;row++){
    if(/^\s*Pasting…\s*$/.test(buffer.getLine(buffer.baseY+row)?.translateToString(true)||''))return true;
  }
  return false;
}
