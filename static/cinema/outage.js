/* Fazon Nexus · Cinema Planner — outage message.
   CINEMA_OUTAGE(D) looks at the API's per-cinema `stale` flags and says whether a whole chain (or a large share
   of all cinemas) has stopped updating, which means something is broken rather than one cinema's website being down.
   Returns null when all is well, else {chains:[names], all:bool, since:Date|null, n:count}.
   Cinemas in a region that hasn't been fetched yet aren't counted: that is a normal wait, not an outage. */
function CINEMA_OUTAGE(D){
  const cs=(D&&Array.isArray(D.cinemas)?D.cinemas:[]).filter(c=>c&&c.error!=='not fetched yet');
  if(!cs.length)return null;
  const by=new Map();
  for(const c of cs){const k=c.chain||'Independent';if(!by.has(k))by.set(k,[]);by.get(k).push(c)}
  // a chain counts once at least 3 of its cinemas are here and at least half of them are stale
  const chains=[...by].filter(([k,l])=>k!=='Independent'&&l.length>=3&&l.filter(c=>c.stale).length*2>=l.length).map(([k])=>k).sort();
  // `all` = a big share of the cinemas OUTSIDE those chains is stale too (or with no chain out, of all of them)
  const stale=cs.filter(c=>c.stale),rest=stale.filter(c=>!chains.includes(c.chain)),all=cs.length>=8&&rest.length*4>=cs.length;
  if(!chains.length&&!all)return null;
  const hit=all?stale:stale.filter(c=>chains.includes(c.chain));
  // last time any of the affected cinemas did update = when the problem started
  const t=hit.map(c=>Date.parse(c.updated)).filter(n=>!isNaN(n));
  return {chains,all,since:t.length?new Date(Math.max(...t)):null,n:hit.length};
}
// message text for an outage (plain text; the caller escapes it)
function CINEMA_OUTAGE_TEXT(o){
  if(!o)return '';
  const list=a=>a.length<2?a.join(''):a.slice(0,-1).join(', ')+' and '+a[a.length-1];
  const who=!o.chains.length?'Many cinemas':o.all?`Times from ${list([...o.chains,'many other cinemas'])}`:`${list(o.chains)} times`;
  const when=o.since?' since '+o.since.toLocaleString('en-GB',{weekday:'short',day:'numeric',month:'short',hour:'2-digit',minute:'2-digit'}):' recently';
  return `${who} haven't updated${when}, so some showings may be out of date. This has been reported automatically and is being looked at.`;
}
