/* Fazon Nexus · Cinema Planner — Listings tab (search, day, format, chains, by film / by cinema).
   CINEMA_LISTINGS(D) wires up the listings UI for data D (the API's JSON) and returns the helpers the planner shares. */
function CINEMA_LISTINGS(D){
const P=new Set(D.premium||[]), $=id=>document.getElementById(id);
const {screenType,otherTags,isScreen,stCmp,pickAllows}=SCREENTYPES(P);
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const DT=CINEMA_DATES(),SOON=DT.add(D.from,13);   // "Next 2 weeks" ends here; dates past it are named with their month
const dayLong=d=>new Date(d+'T12:00').toLocaleDateString('en-GB',{weekday:'short',day:'numeric',month:'short'});
const dayName=d=>d>SOON?dayLong(d):new Date(d+'T12:00').toLocaleDateString('en-GB',{weekday:'short',day:'numeric'});
const stBadge=st=>st==='Standard'?'':`<span class="b p">${esc(st)}</span>`;
const tagBadges=x=>stBadge(screenType(x))+otherTags(x).map(t=>`<span class="b o">${esc(t)}</span>`).join('');
const S={q:'',day:'',fmt:'',view:'film',chains:new Set(D.cinemas.map(c=>c.chain))};
// region name of cinema i, only when the data spans several regions ('' otherwise, and with an older API)
const RN=new Map((Array.isArray(D.regions)?D.regions:[]).map(r=>[r&&r.key,r&&r.name]));
const multi=new Set(D.cinemas.map(c=>c.region).filter(Boolean)).size>1;
const rgn=i=>{const k=multi&&D.cinemas[i]&&D.cinemas[i].region;return k?String(RN.get(k)||k):''};

// ---------- data freshness ----------
// ISO timestamps only (a free-text "generated" string is shown as-is); per-cinema `updated` / `stale` come from the API.
const isoDate=s=>typeof s==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(s)&&!isNaN(Date.parse(s))?new Date(s):null;
const hhmm=d=>d.toLocaleTimeString('en-GB',{hour:'2-digit',minute:'2-digit'});
function when(d){const now=new Date(),a=new Date(now.getFullYear(),now.getMonth(),now.getDate()),b=new Date(d.getFullYear(),d.getMonth(),d.getDate()),
    days=Math.round((a-b)/864e5);
  return days===0?hhmm(d):days===1?'yesterday '+hhmm(d):d.toLocaleDateString('en-GB',{weekday:'short',day:'numeric',month:'short'})+' '+hhmm(d)}
function ago(d){const m=Math.max(0,(Date.now()-d.getTime())/6e4);
  if(m<1.5)return 'just now';if(m<90)return Math.round(m)+' min ago';const h=m/60;if(h<36)return Math.round(h)+' hours ago';
  const n=Math.round(h/24);return n+' day'+(n>1?'s':'')+' ago'}
const full=d=>d.toLocaleString('en-GB',{weekday:'short',day:'numeric',month:'short',hour:'2-digit',minute:'2-digit'});
// short subtle note for a cinema whose listings are older than the rest ('' when fresh)
function staleNote(c){const C=D.cinemas[c];if(!C||!C.stale)return '';const u=isoDate(C.updated);
  return `<span class="stl" title="${u?'Listings last refreshed '+esc(full(u)):'Listings could not be refreshed recently'}">${u?'last updated '+esc(ago(u)):'not recently updated'}</span>`}
const gen=isoDate(D.generated)||D.cinemas.map(c=>isoDate(c.updated)).filter(Boolean).sort((a,b)=>b-a)[0]||null;
const nStale=D.cinemas.filter(c=>c.stale).length;
$('sub').innerHTML=esc(`${dayLong(D.from)} – ${dayLong(D.to)} · ${D.shows.length.toLocaleString()} showings · `+
  `${Object.keys(D.films).length} films · ${D.cinemas.filter(c=>!c.error).length}/${D.cinemas.length} cinemas · `)+
  (gen?`<span title="${esc(full(gen))}">updated ${esc(when(gen))}</span>`:D.generated?'updated '+esc(D.generated):'');
const bad=D.cinemas.map((c,i)=>[c,i]).filter(([c])=>c.error||c.stale);
// "not fetched yet" = a region nobody had looked at lately: the server scrapes it as soon as it's asked for
const unf=c=>c.error==='not fetched yet';
if(bad.length){const nNew=bad.filter(([c])=>unf(c)).length,nErr=bad.filter(([c])=>c.error&&!unf(c)).length,parts=[];
  if(nErr)parts.push(`${nErr} cinema${nErr>1?'s':''} failed to load`);
  if(nNew)parts.push(`${nNew} cinema${nNew>1?'s':''} still being fetched (try again in about half an hour)`);
  if(bad.length-nErr-nNew)parts.push(`${bad.length-nErr-nNew} showing older listings`);
  $('errs').innerHTML=`<details><summary>${parts.join(' · ')}</summary>`+
    bad.map(([c,i])=>`<div>${esc(c.name)}${c.error?`: <span class="${unf(c)?'':'er'}">${esc(c.error)}</span>`:''} ${staleNote(i)}</div>`).join('')+'</details>';
  if(!nErr)$('errs').classList.add('soft')}

// ---------- filter bar ----------
const days=[...new Set(D.shows.map(s=>s.t.slice(0,10)))].sort();
// with a long window the list opens on the next 2 weeks; a search still looks at every date, so a far-off film can be found
if(days.length&&days[days.length-1]>SOON)S.day='soon';
$('day').innerHTML=(S.day?'<option value="soon">Next 2 weeks</option>':'')+'<option value="">All days</option>'+
  DT.months(days).map(([m,ds])=>`<optgroup label="${esc(m)}">`+ds.map(d=>`<option value="${d}">${dayLong(d)}</option>`).join('')+'</optgroup>').join('');
$('day').value=S.day;
const fc={},sc={};D.shows.forEach(s=>{s.x.forEach(x=>fc[x]=(fc[x]||0)+1);const st=screenType(s.x);sc[st]=(sc[st]||0)+1});
$('fmt').innerHTML='<option value="">All formats</option><option value="*">Premium screens only</option>'+
  '<optgroup label="Screen type (exact)">'+Object.keys(sc).sort(stCmp).map(x=>`<option value="st:${esc(x)}">${esc(x)} (${sc[x]})</option>`).join('')+'</optgroup>'+
  '<optgroup label="Any showing tagged">'+Object.keys(fc).sort((a,b)=>(isScreen(b)-isScreen(a))||fc[b]-fc[a]).map(x=>`<option value="tag:${esc(x)}">${esc(x)} (${fc[x]})</option>`).join('')+'</optgroup>';
const chains=[...new Set(D.cinemas.map(c=>c.chain))];
$('chains').innerHTML=chains.map(c=>`<button class="on" data-c="${esc(c)}">${esc(c)}</button>`).join('');
$('chains').onclick=e=>{const c=e.target.dataset.c;if(!c)return;
  if(e.shiftKey||e.metaKey||e.ctrlKey){S.chains=new Set([c])}else{S.chains.has(c)?S.chains.delete(c):S.chains.add(c)}
  if(!S.chains.size)S.chains=new Set(chains);
  [...$('chains').children].forEach(b=>b.classList.toggle('on',S.chains.has(b.dataset.c)));render()};
$('q').oninput=e=>{S.q=e.target.value.trim().toLowerCase();render()};
$('day').onchange=e=>{S.day=e.target.value;render()};
$('fmt').onchange=e=>{S.fmt=e.target.value;render()};
$('view').onclick=()=>{S.view=S.view==='film'?'cinema':'film';$('view').textContent='View: by '+S.view;render()};

// ---------- list ----------
// cinemas with nothing in the listings at all: still shown, so they read as "nothing on" and not as missing
const HAS=new Set(D.shows.map(s=>s.c));
const noneNote=c=>unf(c)?'still being fetched':c.error?'listings couldn’t be loaded':'no showings listed';
function rowsFor(shows){ // shows of one (film, cinema) -> grouped by format signature, then day
  const g=new Map();
  for(const s of shows){const k=screenType(s.x)+'\u0001'+otherTags(s.x).join('|');(g.get(k)||g.set(k,[]).get(k)).push(s)}
  return [...g.entries()].map(([k,ss])=>[k.split('\u0001'),ss]).sort((a,b)=>stCmp(a[0][0],b[0][0])||a[0][1].localeCompare(b[0][1])).map(([[st,o],ss])=>{
    const byDay=new Map();ss.sort((a,b)=>a.t<b.t?-1:1).forEach(s=>(byDay.get(s.t.slice(0,10))||byDay.set(s.t.slice(0,10),[]).get(s.t.slice(0,10))).push(s));
    return `<div class="grp">${stBadge(st)}${o?o.split('|').map(t=>`<span class="b o">${esc(t)}</span>`).join(''):''}`+[...byDay.entries()].map(([d,l])=>
      `<div class="day"><span class="dl">${dayName(d)}</span>`+l.map(s=>
        `<a class="t" ${s.u?`href="${esc(s.u)}" target="_blank" rel="noopener"`:''} title="${esc(s.s||'')}">${s.t.slice(11,16)}</a>`).join('')+'</div>').join('')+'</div>';
  }).join('');
}
function render(){
  const q=S.q;
  const shows=D.shows.filter(s=>{const c=D.cinemas[s.c];
    if(!S.chains.has(c.chain))return false;
    if(S.day==='soon'?!q&&s.t.slice(0,10)>SOON:S.day&&s.t.slice(0,10)!==S.day)return false;
    if(S.fmt==='*'&&screenType(s.x)==='Standard')return false;
    if(S.fmt.startsWith('st:')&&screenType(s.x)!==S.fmt.slice(3))return false;
    if(S.fmt.startsWith('tag:')&&!s.x.includes(S.fmt.slice(4)))return false;
    return !q||D.films[s.k].toLowerCase().includes(q)||c.name.toLowerCase().includes(q)||(multi&&rgn(s.c).toLowerCase().includes(q))});
  const outer=S.view==='film'?(s=>s.k):(s=>s.c), inner=S.view==='film'?(s=>s.c):(s=>s.k);
  const G=new Map();
  for(const s of shows){const a=outer(s),b=inner(s);const m=G.get(a)||G.set(a,new Map()).get(a);(m.get(b)||m.set(b,[]).get(b)).push(s)}
  const cards=[...G.entries()].map(([a,m])=>{
    const n=[...m.values()].reduce((t,l)=>t+l.length,0), prem=new Set();
    m.forEach(l=>l.forEach(s=>{const st=screenType(s.x);if(st!=='Standard')prem.add(st)}));
    const head=S.view==='film'?esc(D.films[a]):`<a href="${esc(D.cinemas[a].url)}" target="_blank" rel="noopener" class="cl">${esc(D.cinemas[a].name)}</a>`;
    const sub=S.view==='film'?`${m.size} cinema${m.size>1?'s':''} · ${n} showings`:`${rgn(a)?esc(rgn(a))+' · ':''}${m.size} films · ${n} showings`;
    return {sort:S.view==='film'?-m.size*1e4-n:a,
      head:`<span class="ttl">${head}</span><span class="meta">${sub}</span>${S.view==='film'?'':staleNote(a)}<span>${[...prem].sort(stCmp).map(stBadge).join('')}</span>`,
      body:()=>[...m.entries()].sort((x,y)=>S.view==='film'?D.cinemas[x[0]].name.localeCompare(D.cinemas[y[0]].name):D.films[x[0]].localeCompare(D.films[y[0]])).map(([b,l])=>{
        const who=S.view==='film'?`<a href="${esc(D.cinemas[b].url)}" target="_blank" rel="noopener">${esc(D.cinemas[b].name)}</a><span class="meta">${esc(D.cinemas[b].chain)}${rgn(b)?' · '+esc(rgn(b)):''}</span>${staleNote(b)}`:`<b>${esc(D.films[b])}</b>`;
        return `<div class="row"><div class="who">${who}</div><div>${rowsFor(l)}</div></div>`}).join('')};
  }).sort((x,y)=>x.sort<y.sort?-1:x.sort>y.sort?1:0);
  const none=D.cinemas.map((c,i)=>i).filter(i=>{const c=D.cinemas[i];return !HAS.has(i)&&S.chains.has(c.chain)&&(!q||c.name.toLowerCase().includes(q)||(multi&&rgn(i).toLowerCase().includes(q)))});
  if(S.view==='cinema'){for(const i of none)cards.push({sort:i,none:i});cards.sort((x,y)=>x.sort-y.sort)}
  $('count').textContent=`${cards.length} ${S.view==='film'?'films':'cinemas'} · ${shows.length.toLocaleString()} showings${S.day==='soon'&&q?' · all dates':''}`;
  const open=!!q||cards.length<=3;
  $('list').innerHTML=cards.length?'':'<div class="empty">Nothing matches.</div>';
  const frag=document.createDocumentFragment();
  for(const c of cards){
    if(c.none!=null){const C=D.cinemas[c.none],d=document.createElement('div');d.className='card nos';
      d.innerHTML=`<span class="ttl"><a href="${esc(C.url)}" target="_blank" rel="noopener" class="cl">${esc(C.name)}</a></span><span class="meta">${rgn(c.none)?esc(rgn(c.none))+' · ':''}${noneNote(C)}</span>`;frag.appendChild(d);continue}
    const d=document.createElement('details');d.className='card';
    d.innerHTML=`<summary>${c.head}</summary><div class="rows"></div>`;
    const fill=()=>{if(d.open&&!d.dataset.f){d.dataset.f=1;d.lastChild.innerHTML=c.body()}};
    d.addEventListener('toggle',fill);if(open){d.open=true;fill()}frag.appendChild(d)}
  $('list').appendChild(frag);
  const quiet=none.filter(i=>!D.cinemas[i].error);
  if(S.view==='film'&&quiet.length){const d=document.createElement('div');d.className='nosl';
    d.innerHTML=`No showings listed right now at ${quiet.map(i=>`<a href="${esc(D.cinemas[i].url)}" target="_blank" rel="noopener">${esc(D.cinemas[i].name)}</a>`).join(', ')}.`;$('list').appendChild(d)}
}
render();
return {screenType,otherTags,isScreen,stCmp,pickAllows,esc,tagBadges,staleNote,rgn};
}
