/* Fazon Nexus · Cinema Planner — Listings tab (search, day, format, chains, by film / by cinema).
   CINEMA_LISTINGS(D) wires up the listings UI for data D (the API's JSON) and returns the helpers the planner shares. */
function CINEMA_LISTINGS(D){
const P=new Set(D.premium||[]), $=id=>document.getElementById(id);
const {screenType,otherTags,isScreen,stCmp,pickAllows}=SCREENTYPES(P);
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const dayName=d=>new Date(d+'T12:00').toLocaleDateString('en-GB',{weekday:'short',day:'numeric'});
const stBadge=st=>st==='Standard'?'':`<span class="b p">${esc(st)}</span>`;
const tagBadges=x=>stBadge(screenType(x))+otherTags(x).map(t=>`<span class="b o">${esc(t)}</span>`).join('');
const S={q:'',day:'',fmt:'',view:'film',chains:new Set(D.cinemas.map(c=>c.chain))};

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
$('sub').innerHTML=esc(`${dayName(D.from)} – ${dayName(D.to)} · ${D.shows.length.toLocaleString()} showings · `+
  `${Object.keys(D.films).length} films · ${D.cinemas.filter(c=>!c.error).length}/${D.cinemas.length} cinemas · `)+
  (gen?`<span title="${esc(full(gen))}">updated ${esc(when(gen))}</span>`:D.generated?'updated '+esc(D.generated):'');
const bad=D.cinemas.map((c,i)=>[c,i]).filter(([c])=>c.error||c.stale);
if(bad.length){const nErr=bad.filter(([c])=>c.error).length,parts=[];
  if(nErr)parts.push(`${nErr} cinema${nErr>1?'s':''} failed to load`);
  if(bad.length-nErr)parts.push(`${bad.length-nErr} showing older listings`);
  $('errs').innerHTML=`<details><summary>${parts.join(' · ')}</summary>`+
    bad.map(([c,i])=>`<div>${esc(c.name)}${c.error?` — <span class="er">${esc(c.error)}</span>`:''} ${staleNote(i)}</div>`).join('')+'</details>';
  if(!nErr)$('errs').classList.add('soft')}

// ---------- filter bar ----------
const days=[...new Set(D.shows.map(s=>s.t.slice(0,10)))].sort();
$('day').innerHTML='<option value="">All days</option>'+days.map(d=>`<option value="${d}">${dayName(d)}</option>`).join('');
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
    if(S.day&&s.t.slice(0,10)!==S.day)return false;
    if(S.fmt==='*'&&screenType(s.x)==='Standard')return false;
    if(S.fmt.startsWith('st:')&&screenType(s.x)!==S.fmt.slice(3))return false;
    if(S.fmt.startsWith('tag:')&&!s.x.includes(S.fmt.slice(4)))return false;
    return !q||D.films[s.k].toLowerCase().includes(q)||c.name.toLowerCase().includes(q)});
  const outer=S.view==='film'?(s=>s.k):(s=>s.c), inner=S.view==='film'?(s=>s.c):(s=>s.k);
  const G=new Map();
  for(const s of shows){const a=outer(s),b=inner(s);const m=G.get(a)||G.set(a,new Map()).get(a);(m.get(b)||m.set(b,[]).get(b)).push(s)}
  const cards=[...G.entries()].map(([a,m])=>{
    const n=[...m.values()].reduce((t,l)=>t+l.length,0), prem=new Set();
    m.forEach(l=>l.forEach(s=>{const st=screenType(s.x);if(st!=='Standard')prem.add(st)}));
    const head=S.view==='film'?esc(D.films[a]):`<a href="${esc(D.cinemas[a].url)}" target="_blank" rel="noopener" class="cl">${esc(D.cinemas[a].name)}</a>`;
    const sub=S.view==='film'?`${m.size} cinema${m.size>1?'s':''} · ${n} showings`:`${m.size} films · ${n} showings`;
    return {sort:S.view==='film'?-m.size*1e4-n:a,
      head:`<span class="ttl">${head}</span><span class="meta">${sub}</span>${S.view==='film'?'':staleNote(a)}<span>${[...prem].sort(stCmp).map(stBadge).join('')}</span>`,
      body:()=>[...m.entries()].sort((x,y)=>S.view==='film'?D.cinemas[x[0]].name.localeCompare(D.cinemas[y[0]].name):D.films[x[0]].localeCompare(D.films[y[0]])).map(([b,l])=>{
        const who=S.view==='film'?`<a href="${esc(D.cinemas[b].url)}" target="_blank" rel="noopener">${esc(D.cinemas[b].name)}</a><span class="meta">${esc(D.cinemas[b].chain)}</span>${staleNote(b)}`:`<b>${esc(D.films[b])}</b>`;
        return `<div class="row"><div class="who">${who}</div><div>${rowsFor(l)}</div></div>`}).join('')};
  }).sort((x,y)=>x.sort<y.sort?-1:x.sort>y.sort?1:0);
  $('count').textContent=`${cards.length} ${S.view==='film'?'films':'cinemas'} · ${shows.length.toLocaleString()} showings`;
  const open=!!q||cards.length<=3;
  $('list').innerHTML=cards.length?'':'<div class="empty">Nothing matches.</div>';
  const frag=document.createDocumentFragment();
  for(const c of cards){const d=document.createElement('details');d.className='card';
    d.innerHTML=`<summary>${c.head}</summary><div class="rows"></div>`;
    const fill=()=>{if(d.open&&!d.dataset.f){d.dataset.f=1;d.lastChild.innerHTML=c.body()}};
    d.addEventListener('toggle',fill);if(open){d.open=true;fill()}frag.appendChild(d)}
  $('list').appendChild(frag);
}
render();
return {screenType,otherTags,isScreen,stCmp,pickAllows,esc,tagBadges,staleNote};
}
