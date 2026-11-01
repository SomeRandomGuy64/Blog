/* Fazon Nexus · Cinema Planner — same-film matching on the page. The server merges listings of one film, and sends what it
   did in D.aliases (older keys to current ones), D.notes ({k:{n:{key:title},s:[similar keys],u:badge}}) and each show's r (the
   listing's own key). A visitor's corrections live only in this browser (localStorage) and are applied over the server's,
   before anything reads D, so they win here and never reach anyone else. With the owner token (feedback.js's owner mode), a
   correction can go to the shared list instead.
   CINEMA_SAME(opts): opts.url() = the listings URL, opts.api = API base. Corrections: {m:{key:target},no:[pair],ok:[key]};
   m[k]=k keeps a listing apart. A target missing from today's listings leaves the correction saved but unused. */
function CINEMA_SAME(opts){
opts=opts||{};
const LSK='fazon-cinema-same',OWN='fazon-cinema-owner';
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const pair=(a,b)=>a<b?a+'|'+b:b+'|'+a;
const obj=v=>v&&typeof v==='object'&&!Array.isArray(v);
function clean(v){const m={};if(obj(v)&&obj(v.m))for(const k in v.m)if(typeof v.m[k]==='string')m[k]=v.m[k];
  const strs=x=>Array.isArray(x)?x.filter(s=>typeof s==='string'):[];return {m,no:strs(v&&v.no),ok:strs(v&&v.ok)}}
function load(){try{return clean(JSON.parse(localStorage.getItem(LSK)||'null'))}catch(e){return clean(null)}}
function save(L){try{localStorage.setItem(LSK,JSON.stringify(L))}catch(e){}}

// follow a chain of corrections; a loop stops where it closes
function follow(m,k){const seen=new Set();while(!seen.has(k)){seen.add(k);const t=m[k];if(t==null||t===k)break;k=t}return k}

let D=null,L=clean(null),dlg=null,cur=null;
const NAMES={};   // every title seen, by listing key: a merged listing has no film entry of its own
function apply(D,L){
  D.aliases=D.aliases||{};D.notes=D.notes||{};
  const name=NAMES;for(const k in D.notes){const n=D.notes[k].n;if(n)for(const r in n)name[r]=n[r]}
  for(const k in D.films)name[k]=name[k]||D.films[k];
  const m=L.m,moved=new Set(),went={};
  for(const s of D.shows){const o=s.r||s.k;let t;
    if(m[o]===o)t=o;else if(m[o]!=null)t=follow(m,o);else if(m[s.k]!=null&&m[s.k]!==s.k)t=follow(m,s.k);else continue;
    if(t!==o)t=D.aliases[t]||t;if(t===s.k)continue;
    if(t!==o&&!D.films[t])continue;
    moved.add(s.k);moved.add(t);
    const n=D.notes[s.k];if(n&&n.n){delete n.n[o]}
    if(t!==o){const x=D.notes[t]||(D.notes[t]={});x.n=x.n||{};x.n[t]=x.n[t]||name[t];x.n[o]=name[o]}
    if(!D.films[t])D.films[t]=name[o];
    if(o!==t)D.aliases[o]=t;else delete D.aliases[o];
    went[s.k]=t;
    s.r=o===t?undefined:o;s.k=t}
  const used=new Set(D.shows.map(s=>s.k));for(const k in D.films)if(!used.has(k)){delete D.films[k];if(went[k])D.aliases[k]=went[k]}   // only an emptied film forwards its picks
  for(const k of moved){const n=D.notes[k];if(n){n.u=false;if(n.n&&Object.keys(n.n).length<2)delete n.n}}
  for(const k in D.notes){const n=D.notes[k];
    if(n.s){const had=n.s.length;n.s=n.s.filter(o=>D.films[o]&&o!==k&&!L.no.includes(pair(k,o)));if(n.s.length<had&&!n.s.length&&!n.n)n.u=false}
    if(L.ok.includes(k))n.u=false}
  return D}

// where a saved key lives now
function keyOf(D,k){const a=D.aliases||{},seen=new Set();while(a[k]&&!seen.has(k)){seen.add(k);k=a[k]}return k}
const both=(a,b)=>(a.length&&b.length?[...new Set(a.concat(b))]:[]);   // an empty list means "any"
const common=(a,b)=>a.filter(x=>b.includes(x));
// saved picks follow their film; two that land on one film become one, in the higher place, allowing what either allowed
function remap(picks,D){const out=[],at=new Map();
  for(const p of picks){const k=keyOf(D,p.k),i=at.get(k);
    if(i==null){at.set(k,out.length);out.push(k===p.k?p:{...p,k});continue}
    const q=out[i];q.cin=both(q.cin,p.cin);q.st=both(q.st,p.st);q.req=common(q.req,p.req);q.exc=common(q.exc,p.exc);
    const dd=q.dd&&p.dd?common(q.dd,p.dd):null;q.dd=dd&&dd.length?dd:null}
  return out}

// ---------- on the page ----------
const token=()=>{try{return localStorage.getItem(OWN)||''}catch(e){return ''}};
const badge=k=>D&&D.notes&&D.notes[k]&&D.notes[k].u?`<button type="button" class="sameb" data-same="${esc(k)}" title="This match might be wrong. Check it." aria-label="Check this film match">?</button>`:'';
const button=k=>`<button type="button" class="ib" data-same="${esc(k)}">Same film as another listing?</button>`;
const row=(label,btns)=>`<div class="samer"><span>${esc(label)}</span><span class="sameas">${btns}</span></div>`;
const act=(a,v,label)=>`<button type="button" class="ib" data-sa="${a}" data-v="${esc(v)}">${esc(label)}</button>`;
function results(k,q){q=q.trim().toLowerCase();if(q.length<2)return '';
  const l=Object.keys(D.films).filter(o=>o!==k&&D.films[o].toLowerCase().includes(q)).slice(0,8);
  return l.length?l.map(o=>row(D.films[o],act('merge',o,'Same film'))).join(''):'<div class="meta">No listing matches that.</div>'}
function show(k){cur=k;const n=D.notes[k]||{},names=n.n?Object.keys(n.n):[],mine=Object.keys(L.m),own=token();
  dlg.innerHTML=`<h3>${esc(D.films[k])}</h3>
    ${n.u?'<p class="meta">The site isn’t sure about this match. Corrections here only change what you see in this browser.</p>':'<p class="meta">Corrections here only change what you see in this browser.</p>'}
    ${own?'<label class="sameown"><input type="checkbox" id="sameall" checked> Change this for everyone (owner)</label>':''}
    ${names.length>1?`<h4>Listed under these names</h4>${names.map(r=>row(n.n[r],r===k?'':act('split',r,'Not this film'))).join('')}`:''}
    ${n.u&&names.length>1?`<div class="samer"><span class="meta">If these are all right:</span>${act('ok',k,'Looks right')}</div>`:''}
    ${n.s&&n.s.length?`<h4>Might be the same film</h4>${n.s.map(o=>row(D.films[o]||o,act('merge',o,'Same film')+act('no',o,'Different'))).join('')}`:''}
    <h4>Find another listing</h4><input type="search" id="sameq" placeholder="Search film listings…" aria-label="Search film listings"><div id="samel"></div>
    ${mine.length?`<h4>Your corrections</h4>${mine.map(x=>row(NAMES[x]?(L.m[x]===x?NAMES[x]+' (kept apart)':NAMES[x]+' = '+(NAMES[L.m[x]]||L.m[x])):x,act('undo',x,'Undo'))).join('')}`:''}
    <div class="samem" id="samem" role="status"></div><div class="samef"><button type="button" class="ib" data-sa="close">Close</button></div>`;
  dlg.querySelector('#sameq').oninput=e=>{dlg.querySelector('#samel').innerHTML=results(k,e.target.value)};
  if(!dlg.open)dlg.showModal()}
async function shared(body){const r=await fetch(new URL('films/matches',opts.api).href,{method:'POST',cache:'no-store',
    headers:{Authorization:'Bearer '+token(),'Content-Type':'application/json'},body:JSON.stringify(body)});
  if(r.status!==204)throw new Error(r.status===401?'That owner token didn’t work.':(await r.text()).trim()||'HTTP '+r.status);
  await fetch(opts.url(),{cache:'reload'})}   // past the browser's 5-minute copy of the listings
async function run(a,v){if(a==='close'){dlg.close();return}
  const all=dlg.querySelector('#sameall'),k=cur,msg=dlg.querySelector('#samem');
  try{if(all&&all.checked&&a!=='undo'){msg.textContent='Saving for everyone…';
      await shared(a==='merge'?{op:'merge',from:v,to:k}:a==='split'?{op:'split',from:v}:a==='no'?{op:'apart',from:k,to:v}:{op:'confirm',from:k})}
    else{if(a==='merge')L.m[v]=k;else if(a==='split')L.m[v]=v;else if(a==='no')L.no.push(pair(k,v));else if(a==='ok')L.ok.push(k);else if(a==='undo')delete L.m[v];
      save(L)}
    location.reload()}
  catch(e){msg.textContent='Couldn’t save: '+(e&&e.message||e)}}
function start(data,root){D=data;L=load();apply(D,L);
  dlg=document.createElement('dialog');dlg.className='samed';root.appendChild(dlg);
  dlg.addEventListener('click',e=>{const b=e.target.closest('[data-sa]');if(b){e.preventDefault();run(b.dataset.sa,b.dataset.v)}else if(e.target===dlg)dlg.close()});
  document.addEventListener('click',e=>{const b=e.target.closest&&e.target.closest('[data-same]');if(!b)return;
    e.preventDefault();e.stopPropagation();show(b.dataset.same)},true);
  if(D.matchdb==='tmdb'){const p=document.createElement('p');p.className='meta samec';p.textContent='Film data from TMDB. This product uses the TMDB API but is not endorsed or certified by TMDB.';root.appendChild(p)}}
return {clean,apply,keyOf,remap,start,badge,button}}
