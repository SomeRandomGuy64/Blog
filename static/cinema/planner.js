/* Fazon Nexus · Cinema Planner — Schedule tab (picks, options, settings, Google travel times, solver plumbing, itinerary).
   CINEMA_PLANNER(D, L): D = the API's listings JSON, L = helpers returned by CINEMA_LISTINGS(D).
   Uses the globals SOLVER (solver.js, also stringified into a Blob Web Worker) and GROUTES (routes.js). */
function CINEMA_PLANNER(D,L){
const {screenType,isScreen,stCmp,pickAllows,esc,tagBadges,staleNote}=L,rgn=L.rgn||(()=>''),DT=CINEMA_DATES();
const $=id=>document.getElementById(id), PERDAY=5, LSK='fazon-cinema-planner', MODEK='fazon-cinema-mode';
const store={get(k){try{const v=localStorage.getItem(k);return v?JSON.parse(v):null}catch(e){return null}},
             set(k,v){try{localStorage.setItem(k,JSON.stringify(v))}catch(e){}}};
const pad=n=>String(n).padStart(2,'0');
const hm=m=>{m=Math.round(m);const d=Math.floor(m/1440),r=m-d*1440;return pad(Math.floor(r/60))+':'+pad(r%60)+(d>0?'<sup>+1</sup>':'')};
const dur=m=>{m=Math.round(m);return m<60?m+' min':Math.floor(m/60)+' h'+(m%60?' '+(m%60)+' min':'')};
const dnum=s=>Date.UTC(+s.slice(0,4),+s.slice(5,7)-1,+s.slice(8,10))/864e5;
const toMin=(t,day)=>(dnum(t.slice(0,10))-dnum(day))*1440+(+t.slice(11,13))*60+(+t.slice(14,16));
const parseHM=s=>{const m=/^(\d{1,2}):(\d{2})/.exec(s||'');return m?+m[1]*60+ +m[2]:null};
const wdOf=day=>new Date(day+'T12:00:00Z').getUTCDay();
const dayLbl=day=>new Date(day+'T12:00:00Z').toLocaleDateString('en-GB',{weekday:'short',day:'numeric',month:'short',timeZone:'UTC'});
const dshort=day=>dayLbl(day).replace(/ \w+$/,'').replace(',','');   // "Mon 12"
const dadd=(day,n)=>new Date((dnum(day)+n)*864e5).toISOString().slice(0,10);
const isDay=s=>typeof s==='string'&&/^\d{4}-\d\d-\d\d$/.test(s);
const normPC=s=>String(s||'').toUpperCase().replace(/[^A-Z0-9]/g,'');
const fmtPC=s=>{s=normPC(s);return s.length>3?s.slice(0,-3)+' '+s.slice(-3):s};

// ---------- static data ----------
// The plan covers DAYS = start … start+len-1 (start: any day with listings, len 1–10), clamped to the data's D.from..D.to.
// DAYS, each showing's day index (SH[].di) and the film index FI are rebuilt by setWindow() whenever those change.
const TD=(d=>d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate()))(new Date()),MAXLEN=10;   // today (local)
// Pick limit: PERDAY films per planned day, capped at MAXP (5–47; the solver handles at most 47 films). Shortening the plan never deletes picks: the ones past the limit
// stay in the list (marked) but aren't planned. MAXP is the hard ceiling (also when loading saved picks).
const MAXP=47,limit=()=>Math.min(MAXP,Math.max(1,DAYS.length)*PERDAY);
const ndays=n=>n+' day'+(n===1?'':'s');
const D0=D.from>TD?D.from:TD,SOPT=[];   // first plannable day; start-date choices
SOPT.push(...DT.startDays(D.shows,D0,D.to));
const NC=D.cinemas.length,NC1=NC+1,H=NC,CIDX=new Map(D.cinemas.map((c,i)=>[c.name,i]));
const now=Date.now();
const SH=D.shows.map((s,i)=>{const day=s.t.slice(0,10),st=toMin(s.t,day);
  let en=s.e?toMin(s.e,day):NaN;const est=!s.e||!!s.es;if(!(en>st))en=st+20+(s.d>0?s.d:120);
  const past=new Date(s.t).getTime()<now;
  return {i,k:s.k,c:s.c,day,di:-1,st,en,est,past,d0:day,st0:st,en0:en,enS:en,estS:est}});
// Showings starting before 05:00 belong to the previous evening when that day is in the plan (a 02:00 film on the 11th
// is 26:00 on the 10th), so a late "Home by" can reach them; otherwise they stay on their own date.
const EARLY=300;
// A day's "Home by" in minutes from that day's midnight: past the leave time, or in those same small hours, it's the next morning.
const homeBy=w=>{const e=parseHM(w.e)??0,l=parseHM(w.l)??1440;return l<=e||l<EARLY?l+1440:l};
let DAYS=[],FI={},FILMS=[];
function setWindow(){const s=PS.set;s.start=DT.nearest(SOPT,s.start)||TD;
  s.len=Math.min(MAXLEN,Math.max(1,Math.round(+s.len)||7));
  DAYS=[];for(let i=0;i<s.len;i++){const d=dadd(s.start,i);if(d>D.to)break;DAYS.push(d)}
  const DI=new Map(DAYS.map((d,i)=>[d,i]));FI={};
  for(const h of SH){const pv=h.st0<EARLY&&dadd(h.d0,-1);
    if(pv&&DI.has(pv)){h.day=pv;h.st=h.st0+1440;h.en=h.en0+1440}else{h.day=h.d0;h.st=h.st0;h.en=h.en0}
    h.di=DI.has(h.day)?DI.get(h.day):-1;if(h.di<0||h.past)continue;const f=FI[h.k]||(FI[h.k]={shows:[],cin:new Map(),fmt:new Map(),stc:new Map()});
    const x=D.shows[h.i].x,st=screenType(x);f.stc.set(st,(f.stc.get(st)||0)+1);
    f.shows.push(h);f.cin.set(h.c,(f.cin.get(h.c)||0)+1);for(const v of x)if(v==='3D'||!isScreen(v))f.fmt.set(v,(f.fmt.get(v)||0)+1)}
  FILMS=Object.keys(FI).sort((a,b)=>FI[b].shows.length-FI[a].shows.length)}
const title=k=>D.films[k]||k;

// ---------- travel ----------
// Built-in times are straight-line estimates (D.travel.mins uses the same formula as estMin). With the user's own
// Google Maps key (Settings), real times for the chosen way of travelling are fetched in the browser for exactly the
// legs a plan needs and cached in localStorage; every leg carries a flag saying whether it is an estimate (shown with ~).
// Estimates are public transport whatever the choice, which is why cycling, driving and avoidance need a key.
const TV=D.travel||null, EST=Object.assign({walk_km_max:1.5,walk_min_per_km:17,transit_base_min:15,transit_min_per_km:2.6},(TV&&TV.estimate)||{});
const hav=(a,b,c,d)=>{const r=Math.PI/180,x=Math.sin((c-a)*r/2)**2+Math.cos(a*r)*Math.cos(c*r)*Math.sin((d-b)*r/2)**2;return 12742*Math.asin(Math.sqrt(x))};
const estMin=km=>Math.round(km<EST.walk_km_max?km*EST.walk_min_per_km:EST.transit_base_min+km*EST.transit_min_per_km);
const hasLL=o=>!!o&&typeof o.lat==='number'&&typeof o.lng==='number';
const pIdx=TV&&Array.isArray(TV.places)?D.cinemas.map(c=>TV.places.indexOf(c.name)):null;
const CLL=D.cinemas.map((c,i)=>{if(hasLL(c))return {lat:c.lat,lng:c.lng};
  const q=pIdx&&pIdx[i]>=0&&Array.isArray(TV.coords)&&TV.coords[pIdx[i]];
  return q&&typeof q[0]==='number'&&typeof q[1]==='number'?{lat:q[0],lng:q[1]}:null});
const CC=new Int32Array(NC*NC).fill(-1);  // cinema -> cinema minutes (estimates)
for(let a=0;a<NC;a++)for(let b=0;b<NC;b++){const A=CLL[a],B=CLL[b],pa=pIdx?pIdx[a]:-1,pb=pIdx?pIdx[b]:-1;
  const v=pa>=0&&pb>=0&&Array.isArray(TV.mins)&&TV.mins[pa]?TV.mins[pa][pb]:null;
  CC[a*NC+b]=a===b?0:typeof v==='number'?Math.round(v):A&&B?estMin(hav(A.lat,A.lng,B.lat,B.lng)):-1}
// home row: precomputed (D.home postcode match) or straight-line estimate from the geocoded postcode
function homeTravel(){
  const s=PS.set;if(s.noHome)return {none:true,to:new Int32Array(NC),fr:new Int32Array(NC)};
  const g=s.geo;if(!g||normPC(g.pc)!==normPC(s.pc))return null;
  const to=new Int32Array(NC).fill(-1),fr=new Int32Array(NC).fill(-1);
  const hi=TV&&Array.isArray(TV.places)?(TV.places.indexOf('Home')>=0?TV.places.indexOf('Home'):TV.places.length-1):-1;
  if(D.home&&D.home.postcode&&normPC(D.home.postcode)===normPC(s.pc)&&hi>=0&&pIdx&&Array.isArray(TV.mins)&&TV.mins[hi]){
    for(let c=0;c<NC;c++){const p=pIdx[c];if(p<0)continue;const a=TV.mins[hi][p],b=TV.mins[p]&&TV.mins[p][hi];
      to[c]=typeof a==='number'?Math.round(a):-1;fr[c]=typeof b==='number'?Math.round(b):-1}}
  for(let c=0;c<NC;c++){const C=CLL[c];if(C&&to[c]<0&&fr[c]<0)to[c]=fr[c]=estMin(hav(g.lat,g.lng,C.lat,C.lng))}
  return {to,fr,lat:g.lat,lng:g.lng}}
// a day's own start or end point: straight-line estimates like home's (same shape as homeTravel(), so homeLeg() reads it too)
function ptTravel(p){const to=new Int32Array(NC).fill(-1);
  for(let c=0;c<NC;c++){const C=CLL[c];if(C)to[c]=estMin(hav(p.lat,p.lng,C.lat,C.lng))}
  return {to,fr:to,lat:p.lat,lng:p.lng}}
// Google times cache: {"T|lat,lng>lat,lng": [minutes|null (no route), day]} (+ "W|…" walking for short hops). The prefix
// is the travel profile (routes.js prof()): T, Tt/Tr/Ttr (avoiding trams/trains), B cycling, D driving. tp = the one in use.
const GK='fazon-cinema-gkey',GC='fazon-cinema-gtimes',GTTL=30,TODAY=Math.floor(Date.now()/864e5);
const lsGet=k=>{try{return localStorage.getItem(k)}catch(e){return null}};
const lsSet=(k,v)=>{try{if(v==null)localStorage.removeItem(k);else localStorage.setItem(k,v);return true}catch(e){return false}};
let gkey=String(lsGet(GK)||'').trim(),gErr=null,gBusy=false,tp='T';
let GCACHE=store.get(GC);if(!GCACHE||typeof GCACHE!=='object'||Array.isArray(GCACHE))GCACHE={};
const llk=p=>p.lat.toFixed(4)+','+p.lng.toFixed(4);
const gk=(m,A,B)=>m+'|'+llk(A)+'>'+llk(B);
const gHit=k=>{const v=GCACHE[k];return Array.isArray(v)&&TODAY-v[1]<=GTTL&&(v[0]===null||typeof v[0]==='number')?v:null};
function gGet(A,B){if(!gkey)return undefined;const t=gHit(gk(tp,A,B));if(!t)return undefined;const w=tp[0]==='T'&&gHit(gk('W',A,B));
  const vs=[t[0],w&&w[0]].filter(v=>typeof v==='number');return vs.length?Math.min(...vs):null}  // null: no route -> estimate
function gSave(){const ks=Object.keys(GCACHE);
  for(const k of ks)if(!gHit(k))delete GCACHE[k];
  for(let i=0;i<4;i++){if(lsSet(GC,JSON.stringify(GCACHE)))return;   // quota: drop the oldest half and retry
    const ks2=Object.keys(GCACHE).sort((a,b)=>GCACHE[a][1]-GCACHE[b][1]);ks2.slice(0,Math.ceil(ks2.length/2)).forEach(k=>delete GCACHE[k])}}
const HLL=ht=>ht&&!ht.none&&typeof ht.lat==='number'?{lat:ht.lat,lng:ht.lng}:null;
function homeLeg(ht,c,back){if(ht.none)return [0,0];const h=HLL(ht),C=CLL[c];
  const v=h&&C?(back?gGet(C,h):gGet(h,C)):undefined;
  return typeof v==='number'?[v,0]:[back?ht.fr[c]:ht.to[c],1]}
// raw (unclosed) leg matrix over cinemas + home (index NC): T minutes (-1 = none), E 1 = estimate
function rawTravel(ht){const T=new Int32Array(NC1*NC1).fill(-1),E=new Uint8Array(NC1*NC1).fill(1);
  for(let a=0;a<NC;a++)for(let b=0;b<NC;b++){const i=a*NC1+b;T[i]=CC[a*NC+b];
    if(a===b)E[i]=0;else if(gkey&&CLL[a]&&CLL[b]){const v=gGet(CLL[a],CLL[b]);if(typeof v==='number'){T[i]=v;E[i]=0}}}
  for(let c=0;c<NC;c++){const [t,te]=homeLeg(ht,c,false),[f,fe]=homeLeg(ht,c,true);
    T[H*NC1+c]=t;E[H*NC1+c]=te;T[c*NC1+H]=f;E[c*NC1+H]=fe}
  T[H*NC1+H]=0;E[H*NC1+H]=0;return {T,E}}

// ---------- persisted state ----------
// availability: set.days[0..6] = per-weekday template, set.dates["YYYY-MM-DD"] = a date the user edited (wins)
const DEF={days:{},dates:{},start:'',len:7,buf:10,late:0,maxDay:'',maxLeg:'',obj:'most',pc:'',geo:null,gc:{},noHome:false,cin:[],ch:[],st:[],ads:{},travel:'T',noTram:false,noTrain:false,pts:{},rank:[]};
for(let w=0;w<7;w++)DEF.days[w]={on:true,e:'10:00',l:'00:00'};
const saved=store.get(LSK)||{};
const PS={picks:[],set:Object.assign({},DEF,saved.set||{})};
PS.set.days=Object.assign({},DEF.days,PS.set.days||{});if(!PS.set.gc||typeof PS.set.gc!=='object')PS.set.gc={};
{const o=PS.set.dates,n={};if(o&&typeof o==='object')for(const d in o)if(isDay(d)&&d>=TD&&o[d]&&typeof o[d]==='object')n[d]=Object.assign({},DEF.days[0],o[d]);PS.set.dates=n}
// a date's own start / end points: set.pts["YYYY-MM-DD"] = {s,e}, each {pc,lat,lng,area} or, until looked up, {pc,err?}. Past dates go, like set.dates.
{const o=PS.set.pts,n={};if(o&&typeof o==='object'&&!Array.isArray(o))for(const d in o)if(isDay(d)&&d>=TD&&o[d]&&typeof o[d]==='object'){const r={};
  for(const k of ['s','e']){const p=o[d][k];if(p&&typeof p.pc==='string'&&p.pc)r[k]=hasLL(p)?{pc:p.pc,lat:p.lat,lng:p.lng,area:typeof p.area==='string'?p.area:''}:{pc:p.pc,err:p.err}}
  if(r.s||r.e)n[d]=r}PS.set.pts=n}
const ptOf=(day,k)=>{const o=PS.set.pts[day];return o&&o[k]||null};
const ptSame=p=>!PS.set.noHome&&normPC(p.pc)===normPC(PS.set.pc);   // the home postcode typed again is simply home
const dayPt=(day,k)=>{const p=ptOf(day,k);return p&&hasLL(p)&&!ptSame(p)?p:null};
const ptBad=day=>{if(!avail(day).on)return false;const a=ptOf(day,'s'),b=ptOf(day,'e');return !!(a&&!hasLL(a)||b&&!hasLL(b))};   // never quietly plan from home instead
// each plan day's own points; null = home, or in no-home mode the first / last cinema
const dayEnds=()=>DAYS.map(day=>avail(day).on?{s:dayPt(day,'s'),e:dayPt(day,'e')}:{s:null,e:null});
// global cinema list (Settings): names of the cinemas every film is limited to ([] = any). Names not in the current
// listings (another region's cinemas, one that's closed) are kept but neither shown nor applied: kn() is what counts,
// and when none of them is here the list means "any". "Any cinema" clears them all. Per-pick cinema lists work the same.
const str=L=>Array.isArray(L)?[...new Set(L.filter(v=>typeof v==='string'))]:[];
PS.set.cin=str(PS.set.cin);
const kn=L=>L.filter(n=>CIDX.has(n));
// global cinema types (= chains) and screen types (Settings) every film is limited to ([] = any). Screen types not in
// the current listings are kept (they come and go) and stay clearable; chains not here are kept like cinemas (knch()).
const CHAINS=[...new Set(D.cinemas.map(c=>c.chain||''))].sort((a,b)=>(b==='Independent')-(a==='Independent')||a.localeCompare(b));
PS.set.ch=str(PS.set.ch);PS.set.st=str(PS.set.st);
const knch=L=>L.filter(n=>CHAINS.includes(n));
const avail=day=>PS.set.dates[day]||PS.set.days[wdOf(day)];
// adverts per cinema (Settings): set.ads = {cinema name: minutes}; names not in the current listings are kept like cinemas.
// A cinema listed there has its films end at start + minutes + runtime instead of the API's end time.
{const o=PS.set.ads,n={};if(o&&typeof o==='object'&&!Array.isArray(o))for(const k in o){const v=o[k];if(typeof v==='number'&&v>=0&&v<=90)n[k]=Math.round(v)}PS.set.ads=n}
function applyAds(){const A=PS.set.ads,o=D.cinemas.map(c=>A[c.name]);
  for(const h of SH){const m=o[h.c],d=D.shows[h.i].d;if(m==null){h.en0=h.enS;h.est=h.estS}else{h.en0=h.st0+m+(d>0?d:120);h.est=true}}}
applyAds();
// ranking (Settings): set.rank = your order, best first, of {ch} chains, {c} cinemas and {c,s} screens of a cinema. Names not in the current
// listings (screens: not among that cinema's shows' `s`) are kept but neither shown nor applied, like the Settings cinema lists; rkOn() is what counts.
// A showing's penalty is the place of the most specific entry that covers it (screen, then cinema, then chain), spread evenly over 0..PREFMAX minutes,
// PREFMAX with no entry. It's charged per film on top of travel and waiting, so a less-liked cinema wins only by saving that much time.
const PREFMAX=90,SCR=new Map();
for(const s of D.shows)if(s.s){const n=D.cinemas[s.c].name;(SCR.get(n)||SCR.set(n,new Set()).get(n)).add(s.s)}
const rkKey=x=>x.s!=null?'s\u0001'+x.c+'\u0001'+x.s:x.c!=null?'c\u0001'+x.c:'h\u0001'+x.ch;
const rkName=x=>x.s!=null?x.c+' · '+x.s:x.c!=null?x.c:x.ch||'Other';
const rkKnown=x=>x.s!=null?!!SCR.get(x.c)&&SCR.get(x.c).has(x.s):x.c!=null?CIDX.has(x.c):CHAINS.includes(x.ch);
{const o=PS.set.rank,seen=new Set(),n=[];if(Array.isArray(o))for(const x of o){if(!x||typeof x!=='object')continue;
  const r=typeof x.c==='string'&&x.c?(typeof x.s==='string'&&x.s?{c:x.c,s:x.s}:{c:x.c}):typeof x.ch==='string'?{ch:x.ch}:null;
  if(r&&!seen.has(rkKey(r))){seen.add(rkKey(r));n.push(r)}}PS.set.rank=n}
const rkOn=()=>PS.set.rank.filter(rkKnown);
// -> null without a ranking, else D.shows index -> penalty in minutes
function prefs(){const L=rkOn(),by=new Map();if(!L.length)return null;L.forEach((x,i)=>by.set(rkKey(x),Math.round(PREFMAX*i/L.length)));
  return i=>{const s=D.shows[i],c=D.cinemas[s.c];return by.get(rkKey({c:c.name,s:s.s||''}))??by.get(rkKey({c:c.name}))??by.get(rkKey({ch:c.chain||''}))??PREFMAX}}
// per-pick excluded dates (p.dd, null = any); an old weekday list (p.wd = allowed weekdays) becomes the dates it excluded
const ddOf=p=>{let r=null;if(Array.isArray(p.dd))r=p.dd.filter(d=>isDay(d)&&d>=TD);
  else if(Array.isArray(p.wd)){r=[];for(let d=D0;d<=D.to;d=dadd(d,1))if(!p.wd.includes(wdOf(d)))r.push(d)}
  return r&&r.length?[...new Set(r)].sort():null};
if(Array.isArray(saved.picks))for(const p of saved.picks.slice(0,MAXP))if(p&&typeof p.k==='string')
  PS.picks.push({k:p.k,t:p.t||title(p.k),cin:str(p.cin),st:Array.isArray(p.st)?p.st.filter(v=>typeof v==='string'):[],
    req:Array.isArray(p.req)?p.req:[],exc:Array.isArray(p.exc)?p.exc:[],dd:ddOf(p)});
setWindow();
if(!PS.set.pc&&D.home&&D.home.postcode&&hasLL(D.home)){PS.set.pc=fmtPC(D.home.postcode);
  PS.set.geo={pc:PS.set.pc,lat:D.home.lat,lng:D.home.lng,area:(D.home.label||'').replace(/\s*\([^)]*\)\s*$/,'')||'Home'}}
// how you travel (Settings): without a key it's public transport with no avoidance, whatever is saved for when one returns
if(!['T','B','D'].includes(PS.set.travel))PS.set.travel='T';
const tmSync=()=>{const s=PS.set;tp=GROUTES().prof(gkey?s.travel:'T',gkey&&s.noTram,gkey&&s.noTrain)};
tmSync();
const tAvoid=p=>p==='Tt'?'trams':p==='Tr'?'trains':'trams and trains';
const tName=p=>p==='B'?'Google cycling times':p==='D'?'Google driving times':'Google transit times'+(p==='T'?'':` (avoiding ${tAvoid(p)} where possible)`);
const estW=p=>p==='T'?'straight-line estimates':p[0]==='T'?`straight-line estimates that can’t avoid ${tAvoid(p)}`:'straight-line public transport estimates';
const OPEN=new Set();let lastRes=null,dirty=false;
function save(){store.set(LSK,{picks:PS.picks,set:PS.set})}
function changed(){save();if(lastRes&&!dirty){dirty=true;$('pout').classList.add('stale');
  $('pout').insertAdjacentHTML('afterbegin','<div class="stalenote" id="pstale">Picks or settings changed. Press “Plan my schedule” again.</div>')}updGo()}

// ---------- mode toggle ----------
const HASH={plan:'#schedule',dev:'#in-development'};
function setMode(m){if(!['list','plan','dev'].includes(m))m='list';
  $('lbar').hidden=$('lmain').hidden=m!=='list';$('pmain').hidden=m!=='plan';$('dmain').hidden=m!=='dev';
  [...$('mode').children].forEach(b=>b.classList.toggle('on',b.dataset.m===m));store.set(MODEK,m);
  try{history.replaceState(null,'',HASH[m]||location.pathname+location.search)}catch(e){}}
$('mode').onclick=e=>{const m=e.target.dataset.m;if(m)setMode(m)};
setMode(Object.keys(HASH).find(k=>HASH[k]===location.hash)||store.get(MODEK)||'list');

// ---------- banner ----------
function banner(){const msgs=[],L=lastRes&&!dirty&&lastRes.legs;
  if(gErr)msgs.push(['err',gErr]);
  if(!gkey)msgs.push(['','Travel times are straight-line public transport estimates (shown with ~). Add your own Google Maps API key in Settings for real public transport, cycling or driving times.']);
  else if(L&&L.n&&!L.est)msgs.push(['ok',`Using ${tName(tp)} for every journey in this plan.`]);
  else if(L&&L.est&&!(gErr&&L.est===L.n))msgs.push(['',`Using ${tName(tp)}, except ${L.est} of ${L.n} journeys (marked ~) that are ${estW(tp)} because Google found no route or couldn’t be reached.`]);
  else if(!L)msgs.push(['ok',`${tName(tp)} will be fetched for the journeys your plan needs (cached in this browser).`]);
  $('pbanner').innerHTML=msgs.map(([c,m])=>`<div class="banner ${c}">${esc(m)}</div>`).join('')}

// ---------- picker ----------
let hl=0;
function sugg(){const q=$('pq').value.trim().toLowerCase(),have=new Set(PS.picks.map(p=>p.k)),N=limit();
  if(PS.picks.length>=N){$('psugg').innerHTML=`<div class="meta">Maximum of ${N} films${PS.picks.length>N?'':' reached'} for a ${Math.max(1,DAYS.length)}-day plan.</div>`;$('psugg').hidden=false;return}
  // with Settings cinemas / cinema types / screen types: counts are the showings they allow, most first; films with none only show up when searched
  const g=gSet(),gc=new Map(FILMS.map(k=>[k,gCount(FI[k],g)]));
  let l=FILMS.filter(k=>!have.has(k)&&(!q||title(k).toLowerCase().includes(q))&&(q||gc.get(k)[0]>0));
  if(g||PS.set.st.length)l.sort((a,b)=>gc.get(b)[0]-gc.get(a)[0]);
  hl=Math.min(hl,Math.max(0,l.length-1));
  $('psugg').innerHTML=l.length?l.map((k,i)=>{const [n,c]=gc.get(k);return `<div data-k="${esc(k)}" class="${i===hl?'hl':''}${n?'':' dim'}"><span>${esc(title(k))}</span><span class="meta">${n?`${n} · ${c} cin.`:'none allowed by your Settings'}</span></div>`}).join(''):'<div class="meta">No matching films with showings.</div>';
  $('psugg').hidden=false}
function addFilm(k){if(!k||PS.picks.length>=limit()||PS.picks.some(p=>p.k===k))return;
  PS.picks.push({k,t:title(k),cin:[],st:[],req:[],exc:[],dd:null});$('pq').value='';hl=0;renderPicks();changed();sugg()}
$('pq').onfocus=sugg;$('pq').oninput=()=>{hl=0;sugg()};
$('pq').onkeydown=e=>{const it=[...$('psugg').querySelectorAll('[data-k]')];
  if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();hl=(hl+(e.key==='ArrowDown'?1:-1)+it.length)%Math.max(1,it.length);it.forEach((x,i)=>x.classList.toggle('hl',i===hl));it[hl]&&it[hl].scrollIntoView({block:'nearest'})}
  else if(e.key==='Enter'){e.preventDefault();it[hl]&&addFilm(it[hl].dataset.k)}
  else if(e.key==='Escape'){$('psugg').hidden=true;$('pq').blur()}};
$('psugg').onmousedown=e=>{e.preventDefault();const d=e.target.closest('[data-k]');if(d)addFilm(d.dataset.k)};
$('pq').onblur=()=>setTimeout(()=>{$('psugg').hidden=true},120);

// ---------- pick list ----------
// cinemas every film may use per Settings: the cinema list intersected with the cinema types (null = any)
function gSet(){const cin=kn(PS.set.cin),ch=knch(PS.set.ch);if(!cin.length&&!ch.length)return null;
  const r=new Set(cin.length?cin.map(n=>CIDX.get(n)):D.cinemas.keys());
  if(ch.length)for(const c of r)if(!ch.includes(D.cinemas[c].chain||''))r.delete(c);return r}
// cinemas a pick may use: its own Options choice intersected with gSet() (null = any; an empty Set = none)
const cinSet=p=>{const g=gSet(),pc=kn(p.cin);if(!pc.length)return g;const r=new Set();
  for(const n of pc){const c=CIDX.get(n);if(!g||g.has(c))r.add(c)}return r};
const gstOK=x=>!PS.set.st.length||PS.set.st.includes(screenType(x));   // Settings screen types
const allows=(p,x,c,day,cs)=>gstOK(x)&&pickAllows(p,x,c,day,cs);
// [showings, cinemas] of film F that the Settings allow (g = gSet())
const gCount=(F,g)=>{if(!PS.set.st.length){if(!g)return [F.shows.length,F.cin.size];let n=0,k=0;for(const [c,v] of F.cin)if(g.has(c)){n+=v;k++}return [n,k]}
  let n=0;const k=new Set();for(const h of F.shows)if((!g||g.has(h.c))&&gstOK(D.shows[h.i].x)){n++;k.add(h.c)}return [n,k.size]};
function gNote(p){const g=gSet(),gs=PS.set.st,F=FI[p.k];if((!g&&!gs.length)||!F)return null;   // why the Settings rule this pick out
  if(g&&kn(p.cin).length&&!kn(p.cin).some(n=>g.has(CIDX.get(n))))return 'None of this film’s chosen cinemas are allowed by your Settings';
  if(gs.length&&p.st.length&&!p.st.some(v=>gs.includes(v)))return 'None of this film’s chosen screen types are allowed by your Settings';
  if(!gCount(F,g)[0])return `No showings at the ${g&&gs.length?'cinemas and screen types':g?'cinemas':'screen types'} chosen in Settings`;return null}
function matchCount(p){const F=FI[p.k];if(!F)return [0,0];const cs=cinSet(p);let m=0;
  for(const h of F.shows)if(allows(p,D.shows[h.i].x,h.c,h.day,cs))m++;
  return [m,F.shows.length]}
const goneMsg=k=>!D.films[k]?'Not in the current listings':SH.some(h=>h.k===k&&!h.past)?'No showings in the chosen dates':'No upcoming showings left';
const ddIn=p=>p.dd?DAYS.filter(d=>p.dd.includes(d)):[];   // excluded dates inside the plan's window
function pickMeta(p){const F=FI[p.k];if(!F)return goneMsg(p.k);
  const bits=[],[m,n]=matchCount(p);bits.push(m===n?`${n} showings`:`${m} of ${n} showings`);
  const pc=kn(p.cin);if(pc.length)bits.push(pc.length===1?pc[0]:pc.length+' cinemas');
  if(p.st.length)bits.push(p.st.join(' / ')+' only');
  if(p.req.length)bits.push(p.req.join('/')+' only');if(p.exc.length)bits.push('no '+p.exc.join('/'));
  const ex=ddIn(p);if(ex.length)bits.push(ex.length===DAYS.length?'no days':'not '+ex.map(dshort).join(', '));
  const gn=gNote(p);if(gn)bits[0]=gn;return bits.join(' · ')}
function optsHTML(p,pi){const F=FI[p.k];if(!F)return '<div class="meta">This film has no showings in the chosen dates. It is skipped when planning; remove it, change the dates, or keep it for when it returns.</div>';
  const ht=homeTravel(),g=gSet(),cins=[...F.cin.keys()].sort((a,b)=>(g?(!g.has(a))-(!g.has(b)):0)||(ht&&!ht.none?((ht.to[a]<0?1e9:ht.to[a])-(ht.to[b]<0?1e9:ht.to[b])):D.cinemas[a].name.localeCompare(D.cinemas[b].name)));
  const sel=new Set(p.cin);
  // cinemas / screen types the Settings rule out come last (cinemas) or struck through, greyed out; they can only be unticked
  const cl=cins.map(c=>{const nm=D.cinemas[c].name,[t,te]=ht&&!ht.none?homeLeg(ht,c,false):[null,0],out=g&&!g.has(c);
    return `<label data-n="${esc(nm.toLowerCase())}"${out?' class="off" title="Not allowed by your Settings cinemas / cinema types"':''}><input type="checkbox" data-act="cin" value="${esc(nm)}"${sel.has(nm)?' checked':''}${out&&!sel.has(nm)?' disabled':''}> <span>${esc(nm)}</span><span class="meta">${out?'not allowed in Settings · ':''}${t!=null?(t<0?'no route · ':(te?'~':'')+t+' min · '):''}${F.cin.get(c)}</span></label>`}).join('');
  const gs=PS.set.st,sts=[...F.stc.keys()];for(const v of p.st)if(!F.stc.has(v))sts.push(v);sts.sort(stCmp);
  const stchips=sts.map(v=>{const on=p.st.includes(v),out=gs.length&&!gs.includes(v);return `<button class="chip${on?' on':''}${out?' off':''}" data-act="st" data-v="${esc(v)}" aria-pressed="${on}"${out?` title="Not allowed by your Settings screen types"${on?'':' disabled'}`:''}>${esc(v)} <span class="cn">${F.stc.get(v)||0}</span></button>`}).join('');
  const fm=[...F.fmt.keys()];for(const v of p.req.concat(p.exc))if(!fm.includes(v))fm.push(v);   // keep old/absent tags clearable
  fm.sort((a,b)=>((b==='3D')-(a==='3D'))||(F.fmt.get(b)||0)-(F.fmt.get(a)||0)||a.localeCompare(b));
  const fchips=fm.length?fm.map(x=>`<button class="chip${p.req.includes(x)?' req':p.exc.includes(x)?' exc':''}" data-act="fmt" data-v="${esc(x)}" title="Click: require → exclude → either">${esc(x)}</button>`).join(''):'<span class="meta">No other tags</span>';
  const fdays=[...new Set(F.shows.map(h=>h.day))].sort();
  const dchips=DAYS.map(day=>{const has=fdays.includes(day),on=!p.dd||!p.dd.includes(day);
    return `<button class="chip${on?'':' off'}" data-act="dd" data-v="${day}" title="${esc(dayLbl(day))}${has?'':' · no showings this day'}"${has?'':' disabled'}>${esc(dshort(day))}</button>`}).join('');
  return `<div class="opt"><div class="ol">Cinemas <span class="meta">(none ticked = any${g?' allowed by Settings':''}${ht&&!ht.none?' · sorted by distance from home':''})</span></div>
   ${cins.length>8?`<input type="search" class="cf" placeholder="Filter cinemas…" data-act="cf">`:''}<div class="clist">${cl}</div>
   ${kn(p.cin).length?'<button class="ib" data-act="cinclr" style="margin-top:4px">Clear (any cinema)</button>':''}</div>
   <div class="opt"><div class="ol">Screen type <span class="meta">(none ticked = any${gs.length?' allowed by Settings':''})</span></div><div class="chips stc">${stchips}</div></div>
   <div class="opt"><div class="ol">Other tags <span class="meta">(click: <span style="color:var(--ok)">require</span> → <span style="color:var(--err)">exclude</span> → either)</span></div><div class="chips">${fchips}</div></div>
   <div class="opt" style="margin-bottom:0"><div class="ol">Days</div><div class="chips">${dchips}</div></div>`}
const limEl=document.createElement('div');limEl.id='plimit';limEl.className='pcst bad plimit';limEl.setAttribute('role','status');limEl.hidden=true;$('picks').before(limEl);
function renderPicks(){
  const L=PS.picks,N=limit(),over=L.length>N;$('pcount').textContent=`${L.length}/${N}`;$('pcount').classList.toggle('over',over);
  $('pcount').title=`Up to ${PERDAY} films per plan day, ${MAXP} at most (${N} for ${ndays(DAYS.length)})`;
  $('pq').disabled=L.length>=N;$('pq').placeholder=L.length>=N?`Maximum of ${N} films`:'Add a film…';
  limEl.hidden=!over;limEl.textContent=over?`Only the top ${N} picks are planned for ${ndays(DAYS.length)}. Lengthen the plan or remove some.`:'';
  $('picks').innerHTML=L.map((p,i)=>{const gone=!FI[p.k],open=OPEN.has(p.k),ov=i>=N;
    return `<li class="pick${gone?' gone':''}${ov?' over':''}" data-i="${i}"${ov?` title="Over the ${N}-film limit: not planned"`:''}><div class="prow"><span class="grip" draggable="true" title="Drag to reorder">⋮⋮</span><input class="rk" type="text" inputmode="numeric" value="${i+1}" data-act="rk" aria-label="Priority of ${esc(p.t)}" title="Type a number to move this film">
     <div class="pt"><b title="${esc(p.t)}">${esc(gone?p.t:title(p.k))}</b><span class="meta">${esc(pickMeta(p))}</span></div>
     <div class="pbtn"><button data-act="up" title="Higher priority" aria-label="Move up"${i?'':' disabled'}>↑</button><button data-act="dn" title="Lower priority" aria-label="Move down"${i<L.length-1?'':' disabled'}>↓</button>
     <button data-act="opt" class="${open?'on':''}" title="Cinemas, screen types, tags, days">Options</button><button data-act="rm" title="Remove" aria-label="Remove">✕</button></div></div>
     ${open?`<div class="popts">${optsHTML(p,i)}</div>`:''}</li>`}).join('');
  $('phint').hidden=!L.length;
  if(!L.length)$('picks').innerHTML=`<li class="meta" style="padding:8px 2px">No films yet. Search above and add up to ${N}, most wanted first.</li>`;
  updGo()}
function refreshMeta(li,p){li.querySelector('.pt .meta').textContent=pickMeta(p)}
function move(i,j){const L=PS.picks;if(j<0||j>=L.length||i===j)return;const [x]=L.splice(i,1);L.splice(j,0,x);renderPicks();changed()}
$('picks').addEventListener('click',e=>{const b=e.target.closest('[data-act]'),li=e.target.closest('li.pick');if(!b||!li)return;
  const i=+li.dataset.i,p=PS.picks[i],a=b.dataset.act;
  if(a==='up')move(i,i-1);else if(a==='dn')move(i,i+1);
  else if(a==='rm'){PS.picks.splice(i,1);OPEN.delete(p.k);renderPicks();changed()}
  else if(a==='opt'){OPEN.has(p.k)?OPEN.delete(p.k):OPEN.add(p.k);renderPicks()}
  else if(a==='cinclr'){p.cin=[];renderPicks();changed()}
  else if(a==='st'){const v=b.dataset.v,on=!p.st.includes(v);p.st=on?p.st.concat(v):p.st.filter(x=>x!==v);
    b.classList.toggle('on',on);b.setAttribute('aria-pressed',on);refreshMeta(li,p);changed()}
  else if(a==='fmt'){const v=b.dataset.v;
    if(p.req.includes(v)){p.req=p.req.filter(x=>x!==v);p.exc.push(v);b.className='chip exc'}
    else if(p.exc.includes(v)){p.exc=p.exc.filter(x=>x!==v);b.className='chip'}
    else{p.req.push(v);b.className='chip req'}refreshMeta(li,p);changed()}
  else if(a==='dd'){const d=b.dataset.v,s=p.dd||[],off=!s.includes(d);
    p.dd=off?s.concat(d).sort():s.filter(x=>x!==d);if(!p.dd.length)p.dd=null;b.classList.toggle('off',off);refreshMeta(li,p);changed()}});
$('picks').addEventListener('change',e=>{const t=e.target;if(t.dataset.act!=='cin')return;const li=t.closest('li.pick'),p=PS.picks[+li.dataset.i];
  p.cin=t.checked?[...new Set(p.cin.concat(t.value))]:p.cin.filter(n=>n!==t.value);refreshMeta(li,p);changed()});
$('picks').addEventListener('input',e=>{const t=e.target;if(t.dataset.act!=='cf')return;const q=t.value.trim().toLowerCase();
  t.parentNode.querySelectorAll('.clist label').forEach(l=>{l.hidden=!!q&&!l.dataset.n.includes(q)})});
// typed priority number: moves the film there; anything that isn't a number puts the old one back
$('picks').addEventListener('change',e=>{const t=e.target;if(t.dataset.act!=='rk')return;const i=+t.closest('li.pick').dataset.i,n=parseInt(t.value,10);
  if(!(n>0)||String(n)!==t.value.trim()){t.value=i+1;return}t.value=Math.min(n,PS.picks.length);move(i,Math.min(n,PS.picks.length)-1)});
$('picks').addEventListener('focusin',e=>{if(e.target.dataset.act==='rk')e.target.select()});
$('picks').addEventListener('keydown',e=>{if(e.target.dataset.act==='rk'&&e.key==='Enter')e.target.blur()});
// drag & drop (desktop); ↑/↓ everywhere
let dragI=-1;
$('picks').addEventListener('dragstart',e=>{const li=e.target.closest('li.pick');if(!li)return;dragI=+li.dataset.i;
  try{e.dataTransfer.setData('text/plain',String(dragI));e.dataTransfer.effectAllowed='move';e.dataTransfer.setDragImage(li,20,20)}catch(x){}});
const clearDrag=()=>$('picks').querySelectorAll('.dragover-top,.dragover-bot').forEach(x=>x.classList.remove('dragover-top','dragover-bot'));
$('picks').addEventListener('dragover',e=>{if(dragI<0)return;const li=e.target.closest('li.pick');if(!li)return;e.preventDefault();clearDrag();
  const r=li.getBoundingClientRect();li.classList.add(e.clientY<r.top+r.height/2?'dragover-top':'dragover-bot')});
$('picks').addEventListener('drop',e=>{if(dragI<0)return;const li=e.target.closest('li.pick');e.preventDefault();clearDrag();if(!li){dragI=-1;return}
  const r=li.getBoundingClientRect();let j=+li.dataset.i+(e.clientY<r.top+r.height/2?0:1);if(j>dragI)j--;const i=dragI;dragI=-1;move(i,j)});
$('picks').addEventListener('dragend',()=>{dragI=-1;clearDrag()});

// ---------- settings: plan dates ----------
$('pstart').innerHTML=DT.months(SOPT).map(([m,ds])=>`<optgroup label="${esc(m)}">`+ds.map(d=>`<option value="${d}">${esc(dayLbl(d))}${d===TD?' (today)':''}</option>`).join('')+'</optgroup>').join('');
$('plen').innerHTML=Array.from({length:MAXLEN},(_,i)=>`<option value="${i+1}">${i+1} day${i?'s':''}</option>`).join('');
$('pstart').disabled=SOPT.length<2;
function dateNote(){const s=PS.set,n=DAYS.length,el=$('pdnote');$('pstart').value=s.start;$('plen').value=s.len;
  if(!n){el.className='pcst bad';el.textContent='No upcoming listings to plan with.';return}
  const rng=n>1?dayLbl(DAYS[0])+' – '+dayLbl(DAYS[n-1]):dayLbl(DAYS[0]);
  const t=[n<s.len?`Listings only go up to ${dayLbl(D.to)}, so the plan covers ${n} day${n>1?'s':''} (${rng}).`:`Plan covers ${rng}.`];
  const far=DT.farNote(D,DAYS[n-1],TD);if(far)t.push(far);
  el.className='pcst';el.textContent=t.join(' ')}
function winChange(){setWindow();dateNote();renderDays();renderGC();renderPicks();changed()}
$('pstart').onchange=()=>{PS.set.start=$('pstart').value;winChange()};
$('plen').onchange=()=>{PS.set.len=+$('plen').value;winChange()};
// availability per date (an untouched date follows its weekday's template)
const hbNote=(day,x)=>x.on&&parseHM(x.l)>0&&homeBy(x)>=1440?'next day, '+dshort(dadd(day,1)):'';   // midnight itself needs no note
// a day's own start and end postcodes open in a row under it ("Start/end" button)
const PTO=new Set(),ptBusy=new WeakSet(),PTK=[['s','Start'],['e','End']];
const ptB=day=>{const a=ptOf(day,'s'),b=ptOf(day,'e');return a&&b?'Own both':a?'Own start':b?'Own end':'Start/end'};
function ptStat(day,k){const p=ptOf(day,k);if(!p)return ['',''];
  if(ptBusy.has(p))return ['','Looking up '+fmtPC(p.pc)+'…'];
  if(hasLL(p))return ['ok',ptSame(p)?'✓ Same as home':'✓ '+fmtPC(p.pc)+(p.area?' · '+p.area:'')];
  return ['bad',p.err==='fmt'?'That doesn’t look like a UK postcode.':p.err==='nf'?'Postcode '+fmtPC(p.pc)+' not found.':
    p.err==='net'?'Couldn’t reach postcodes.io to look up '+fmtPC(p.pc)+' (offline?). Press Enter to try again.':'Not looked up yet. Press Enter to try again.']}
const ptField=(day,[k,lb],noHome)=>{const p=ptOf(day,k),[c,m]=ptStat(day,k);
  return `<label>${lb}<input type="text" data-pt="${k}" value="${esc(p?p.pc:'')}" placeholder="${noHome?(k==='s'?'First cinema':'Last cinema'):'Home'}" maxlength="12" autocomplete="off" spellcheck="false" aria-label="${lb} postcode on ${esc(dayLbl(day))}"><span class="pcst ${c}" data-ps="${k}">${esc(m)}</span></label>`};
// repaint one day's status lines only: a lookup can finish while the next box is being typed in
function ptRow(day){const T=$('pdays'),r=T.querySelector(`tr[data-pd="${day}"]`),b=T.querySelector(`tr[data-d="${day}"] .ptb`);
  if(b){b.textContent=ptB(day);b.classList.toggle('on',!!PS.set.pts[day])}
  if(r)for(const [k] of PTK){const [c,m]=ptStat(day,k),el=r.querySelector(`[data-ps="${k}"]`),i=r.querySelector(`[data-pt="${k}"]`),p=ptOf(day,k);el.className='pcst '+c;el.textContent=m;if(p&&i!==document.activeElement)i.value=p.pc}}
function renderDays(){const noHome=!!PS.set.noHome;
  $('pdays').innerHTML='<tr><th></th><th>Day</th><th>Leave from</th><th>Home by</th></tr>'+DAYS.map(day=>{const x=avail(day),open=x.on&&(PTO.has(day)||ptBad(day));
    return `<tr class="${x.on?'':'off'}" data-d="${day}"><td><input type="checkbox" data-f="on"${x.on?' checked':''} aria-label="Free on ${esc(dayLbl(day))}"></td><td class="dn">${esc(dayLbl(day))}${x.on?`<button type="button" class="ptb${PS.set.pts[day]?' on':''}" aria-expanded="${open}" title="Start or finish this day somewhere else">${esc(ptB(day))}</button>`:''}</td>
     <td><input type="time" data-f="e" value="${esc(x.e)}"${x.on?'':' disabled'}></td><td><input type="time" data-f="l" value="${esc(x.l)}"${x.on?'':' disabled'} title="Earlier than the leave time, or before 05:00, means the next morning"><span class="hbn">${esc(hbNote(day,x))}</span></td></tr>
     <tr class="ptr" data-pd="${day}"${open?'':' hidden'}><td colspan="4"><div class="ptg">${PTK.map(K=>ptField(day,K,noHome)).join('')}</div><div class="pth">Empty = ${noHome?'the first or last cinema':'home'}. “Leave from” is then when you can leave the start, “Home by” when you must reach the end.</div></td></tr>`}).join('')+
   (DAYS.length>1?'<tr><td></td><td colspan="3"><button class="ib" id="pcopy">Copy first day’s times to all</button></td></tr>':'')}
// cinemas for all films (Settings): sorted by distance from home when known, else grouped by chain
// (cinemas whose chain the Settings cinema types rule out are greyed; counts honour the Settings screen types)
function renderGC(){const sel=new Set(PS.set.cin),ht=homeTravel(),near=ht&&!ht.none,cnt=new Int32Array(NC),gch=PS.set.ch;
  for(const h of SH)if(h.di>=0&&!h.past&&gstOK(D.shows[h.i].x))cnt[h.c]++;
  const tt=c=>near?homeLeg(ht,c,false):null,ch=c=>D.cinemas[c].chain||'',nm=c=>D.cinemas[c].name;
  // without a home: grouped by chain, then by region when the listings span several ("Odeon · North West")
  const RO=new Map((Array.isArray(D.regions)?D.regions:[]).map((r,i)=>[r&&r.key,i])),ro=c=>RO.get(D.cinemas[c].region)??99;
  const grp=c=>ch(c)+(rgn(c)?'\u0001'+rgn(c):''),gl=g=>{const [a,b]=g.split('\u0001');return (a||'Other')+(b?' · '+b:'')};
  const ix=D.cinemas.map((_,i)=>i),T=near?ix.map(c=>tt(c)):null,kch=knch(gch);
  ix.sort(near?(a,b)=>((T[a][0]<0?1e9:T[a][0])-(T[b][0]<0?1e9:T[b][0]))||nm(a).localeCompare(nm(b)):(a,b)=>ch(a).localeCompare(ch(b))||ro(a)-ro(b)||nm(a).localeCompare(nm(b)));
  let html='',last=null;
  for(const c of ix){if(!near&&grp(c)!==last){last=grp(c);html+=`<div class="chn">${esc(gl(last))}</div>`}
    const t=T&&T[c],out=kch.length&&!kch.includes(ch(c));
    html+=`<label data-n="${esc((nm(c)+' '+ch(c)+' '+rgn(c)).toLowerCase())}" data-ch="${esc(grp(c))}"${out?' class="off" title="Its cinema type isn’t ticked below"':near&&rgn(c)?` title="${esc(rgn(c))}"`:''}><input type="checkbox" data-act="gc" value="${esc(nm(c))}"${sel.has(nm(c))?' checked':''}> <span>${esc(nm(c))}</span><span class="meta">${t?(t[0]<0?'no route · ':(t[1]?'~':'')+t[0]+' min · '):''}${cnt[c]} showing${cnt[c]===1?'':'s'}</span></label>`}
  $('pgclist').innerHTML=html;gcFilter();gcSum();renderGF()}
// cinema types (chains) and screen types for all films (Settings); counts = upcoming showings in the plan's dates
function renderGF(){const s=PS.set,nc=new Map(),ns=new Map();
  for(const h of SH)if(h.di>=0&&!h.past){const c=D.cinemas[h.c].chain||'',v=screenType(D.shows[h.i].x);nc.set(c,(nc.get(c)||0)+1);ns.set(v,(ns.get(v)||0)+1)}
  const chip=(v,on,n)=>`<button type="button" class="chip${on?' on':''}" data-v="${esc(v)}" aria-pressed="${on}" title="${n||0} upcoming showing${n===1?'':'s'} in the plan’s dates">${esc(v||'Other')} <span class="cn">${n||0}</span></button>`;
  const chips=(L,on,n)=>`<div class="chips">${L.map(v=>chip(v,on.includes(v),n.get(v))).join('')}</div>`;
  const clr=(L,t)=>L.length?`<button type="button" class="ib gclr" data-clr="1">${t}</button>`:'';
  $('pgch').innerHTML=chips(CHAINS,s.ch,nc)+clr(knch(s.ch),'Any type');
  const sts=[...ns.keys()];for(const v of s.st)if(!ns.has(v))sts.push(v);sts.sort(stCmp);
  const prem=sts.filter(v=>v!=='Standard');   // Standard alone, then the premium / special screens
  $('pgst').innerHTML=(sts.includes('Standard')?chips(['Standard'],s.st,ns):'')+
    (prem.length?`<div class="gsub">Premium &amp; special screens</div>${chips(prem,s.st,ns)}`:'')+clr(s.st,'Any screen')}
for(const [id,k] of [['pgch','ch'],['pgst','st']])$(id).onclick=e=>{const b=e.target.closest('button');if(!b)return;const s=PS.set;
  if(b.dataset.clr)s[k]=[];else{const v=b.dataset.v;s[k]=s[k].includes(v)?s[k].filter(x=>x!==v):s[k].concat(v)}
  renderGC();gcChanged()};
// one-line summaries on the collapsed "Limit every film to" rows: the choice itself when short, else a count
function gcSum(){const s=PS.set,sum=(id,L,any,many)=>{const el=$(id);el.classList.toggle('any',!L.length);
    el.textContent=!L.length?any:L.length<=2?L.join(', '):L.length+' '+many;el.title=L.join(', ')};
  sum('pgcsum',kn(s.cin),'Any cinema','cinemas');sum('pgchsum',knch(s.ch),'Any type','types');sum('pgstsum',s.st,'Any screen','screen types')}
function listFilter(q,el){q=q.trim().toLowerCase();const seen=new Set();
  el.querySelectorAll('label').forEach(l=>{l.hidden=!!q&&!l.dataset.n.includes(q);if(!l.hidden)seen.add(l.dataset.ch)});
  el.querySelectorAll('.chn').forEach(h=>{const n=h.nextElementSibling;h.hidden=!seen.has(n&&n.dataset.ch)})}
const gcFilter=()=>listFilter($('pgcf').value,$('pgclist'));
function gcChanged(){gcSum();renderPicks();changed()}
$('pgcf').oninput=gcFilter;
$('pgclist').addEventListener('change',e=>{const t=e.target;if(t.dataset.act!=='gc')return;const L=PS.set.cin;
  PS.set.cin=t.checked?[...new Set(L.concat(t.value))]:L.filter(n=>n!==t.value);gcChanged()});
$('pgcall').onclick=()=>{const add=[...$('pgclist').querySelectorAll('label:not([hidden]) input')].map(i=>i.value);
  PS.set.cin=[...new Set(PS.set.cin.concat(add))];renderGC();gcChanged()};
$('pgcclr').onclick=()=>{PS.set.cin=[];renderGC();gcChanged()};
// adverts per cinema (Settings), grouped by chain; an empty box = the cinema's default (D.cinemas[].ads, the placeholder)
function renderAds(){const A=PS.set.ads,ch=c=>D.cinemas[c].chain||'',nm=c=>D.cinemas[c].name;
  const ix=D.cinemas.map((_,i)=>i).sort((a,b)=>ch(a).localeCompare(ch(b))||nm(a).localeCompare(nm(b)));
  let html='',last=null;
  for(const c of ix){if(ch(c)!==last){last=ch(c);html+=`<div class="chn">${esc(last||'Other')}</div>`}
    const v=A[nm(c)],def=D.cinemas[c].ads;
    html+=`<label data-n="${esc((nm(c)+' '+ch(c)+' '+rgn(c)).toLowerCase())}" data-ch="${esc(ch(c))}"${v!=null?' class="set"':''}><span>${esc(nm(c))}</span>${rgn(c)?`<span class="meta">${esc(rgn(c))}</span>`:''}<input type="number" min="0" max="90" step="5" inputmode="numeric" data-act="ad" data-c="${esc(nm(c))}" value="${v??''}" placeholder="${typeof def==='number'?def:'default'}" aria-label="Minutes of adverts and trailers at ${esc(nm(c))}"></label>`}
  $('padlist').innerHTML=html;listFilter($('padf').value,$('padlist'));adSum()}
function adSum(){const A=PS.set.ads,L=D.cinemas.filter(c=>A[c.name]!=null).map(c=>`${c.name} ${A[c.name]} min`),el=$('padsum');
  el.classList.toggle('any',!L.length);el.textContent=!L.length?'Default for every cinema':L.length<=2?L.join(', '):L.length+' cinemas changed';el.title=L.join(', ')}
function adChanged(){applyAds();setWindow();adSum();renderPicks();changed()}
$('padf').oninput=()=>listFilter($('padf').value,$('padlist'));
$('padlist').addEventListener('change',e=>{const t=e.target;if(t.dataset.act!=='ad')return;const v=t.value.trim(),n=t.dataset.c;
  if(v===''||isNaN(+v)){delete PS.set.ads[n];t.value=''}else{PS.set.ads[n]=Math.min(90,Math.max(0,Math.round(+v)));t.value=PS.set.ads[n]}
  t.parentNode.classList.toggle('set',PS.set.ads[n]!=null);adChanged()});
$('padclr').onclick=()=>{PS.set.ads={};renderAds();adChanged()};
// ranking (Settings): add from one search box, reorder by rank number or ↑ ↓. Reordering happens among the entries shown; hidden ones stay where they are.
const RKA=[...CHAINS.map(v=>({x:{ch:v},m:'Cinema type'})),...D.cinemas.map((c,i)=>({x:{c:c.name},m:rgn(i)||'Cinema'})).sort((a,b)=>a.x.c.localeCompare(b.x.c)),
  ...[...SCR].flatMap(([c,S])=>[...S].sort((a,b)=>a.localeCompare(b,'en',{numeric:true})).map(s=>({x:{c,s},m:'Screen'})))];
for(const a of RKA)a.h=rkName(a.x).toLowerCase();
let rkHl=0,rkHits=[];
function rkSugg(){const q=$('prkq').value.trim().toLowerCase().split(/\s+/).filter(Boolean),have=new Set(PS.set.rank.map(rkKey));
  rkHits=RKA.filter(a=>!have.has(rkKey(a.x))&&(q.length?q.every(w=>a.h.includes(w)):a.x.s==null)).slice(0,60);rkHl=Math.min(rkHl,Math.max(0,rkHits.length-1));
  $('prksugg').innerHTML=rkHits.length?rkHits.map((a,i)=>`<div data-i="${i}" class="${i===rkHl?'hl':''}"><span>${esc(rkName(a.x))}</span><span class="meta">${esc(a.m)}</span></div>`).join(''):'<div class="meta">Nothing to add by that name.</div>';
  $('prksugg').hidden=false}
function renderRank(){const L=rkOn(),n=L.length,el=$('prksum');
  $('prklist').innerHTML=L.map((x,i)=>{const nm=esc(rkName(x));return `<li class="pick" data-i="${i}"><div class="prow"><input class="rk" type="text" inputmode="numeric" value="${i+1}" data-act="rk" aria-label="Rank of ${nm}" title="Type a number to move this">
    <div class="pt"><b title="${nm}">${nm}</b><span class="meta">${x.s!=null?'Screen':x.c!=null?'Cinema':'Cinema type'}</span></div>
    <div class="pbtn"><button type="button" data-act="up" title="Rank higher" aria-label="Move up"${i?'':' disabled'}>↑</button><button type="button" data-act="dn" title="Rank lower" aria-label="Move down"${i<n-1?'':' disabled'}>↓</button><button type="button" data-act="rm" title="Remove" aria-label="Remove">✕</button></div></div></li>`}).join('');
  $('prkclr').hidden=!PS.set.rank.length;el.classList.toggle('any',!n);el.textContent=!n?'No preference':n<=2?L.map(rkName).join(', '):n+' ranked';el.title=L.map(rkName).join(', ')}
function rkMove(i,j){const R=PS.set.rank,L=rkOn();if(j<0||j>=L.length||i===j)return;const x=L[i],t=L[j];R.splice(R.indexOf(x),1);R.splice(R.indexOf(t)+(j>i?1:0),0,x);renderRank();changed()}
function rkAdd(a){if(!a)return;PS.set.rank.push({...a.x});$('prkq').value='';rkHl=0;renderRank();changed();rkSugg()}
$('prkq').onfocus=rkSugg;$('prkq').oninput=()=>{rkHl=0;rkSugg()};$('prkq').onblur=()=>setTimeout(()=>{$('prksugg').hidden=true},120);
$('prkq').onkeydown=e=>{const it=[...$('prksugg').querySelectorAll('[data-i]')];
  if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();rkHl=(rkHl+(e.key==='ArrowDown'?1:-1)+it.length)%Math.max(1,it.length);it.forEach((x,i)=>x.classList.toggle('hl',i===rkHl));it[rkHl]&&it[rkHl].scrollIntoView({block:'nearest'})}
  else if(e.key==='Enter'){e.preventDefault();rkAdd(rkHits[rkHl])}
  else if(e.key==='Escape'){$('prksugg').hidden=true;$('prkq').blur()}};
$('prksugg').onmousedown=e=>{e.preventDefault();const d=e.target.closest('[data-i]');if(d)rkAdd(rkHits[+d.dataset.i])};
$('prklist').addEventListener('click',e=>{const b=e.target.closest('[data-act]'),li=e.target.closest('li.pick');if(!b||!li)return;const i=+li.dataset.i,a=b.dataset.act;
  if(a==='up')rkMove(i,i-1);else if(a==='dn')rkMove(i,i+1);
  else if(a==='rm'){const R=PS.set.rank;R.splice(R.indexOf(rkOn()[i]),1);renderRank();changed()}});
// typed rank number: same rules as the pick list's
$('prklist').addEventListener('change',e=>{const t=e.target;if(t.dataset.act!=='rk')return;const i=+t.closest('li.pick').dataset.i,n=parseInt(t.value,10),m=rkOn().length;
  if(!(n>0)||String(n)!==t.value.trim()){t.value=i+1;return}t.value=Math.min(n,m);rkMove(i,Math.min(n,m)-1)});
$('prklist').addEventListener('focusin',e=>{if(e.target.dataset.act==='rk')e.target.select()});
$('prklist').addEventListener('keydown',e=>{if(e.target.dataset.act==='rk'&&e.key==='Enter')e.target.blur()});
$('prkclr').onclick=()=>{PS.set.rank=[];renderRank();changed()};
const own=d=>PS.set.dates[d]||(PS.set.dates[d]=Object.assign({},avail(d)));
$('pdays').addEventListener('change',e=>{const t=e.target,tr=t.closest('tr[data-d]');if(!tr)return;const x=own(tr.dataset.d);
  if(t.dataset.f==='on'){x.on=t.checked;renderDays()}else if(t.value){x[t.dataset.f]=t.value;tr.querySelector('.hbn').textContent=hbNote(tr.dataset.d,x)}changed()});
$('pdays').addEventListener('click',e=>{const pb=e.target.closest('.ptb');
  if(pb){const day=pb.closest('tr[data-d]').dataset.d,r=$('pdays').querySelector(`tr[data-pd="${day}"]`);r.hidden=!r.hidden;pb.setAttribute('aria-expanded',!r.hidden);if(r.hidden)PTO.delete(day);else PTO.add(day);return}
  if(e.target.id!=='pcopy')return;const f=Object.assign({},avail(DAYS[0]));
  for(const d of DAYS){const x=own(d);x.e=f.e;x.l=f.l}for(let w=0;w<7;w++){PS.set.days[w].e=f.e;PS.set.days[w].l=f.l}renderDays();changed()});
async function ptSet(day,k,raw){const n=normPC(raw),S=PS.set,c=ptOf(day,k);if(!n&&!c)return;const o=S.pts[day]||(S.pts[day]={});
  if(!n){delete o[k];if(!o.s&&!o.e)delete S.pts[day];ptRow(day);changed();return}
  if(c&&normPC(c.pc)===n&&(hasLL(c)||ptBusy.has(c)))return;
  const p={pc:fmtPC(n)};o[k]=p;
  if(!PCRE.test(n)){p.pc=String(raw).trim().slice(0,12);p.err='fmt';ptRow(day);changed();return}
  let g=pcKnown(n,p.pc);
  if(!g){ptBusy.add(p);ptRow(day);changed();
    try{g=await pcFetch(p.pc);if(!g)p.err='nf'}catch(err){p.err='net'}
    ptBusy.delete(p);if(ptOf(day,k)!==p)return}   // changed again meanwhile
  if(g)Object.assign(p,{pc:fmtPC(g.pc||p.pc),lat:g.lat,lng:g.lng,area:g.area||''});
  ptRow(day);changed()}
$('pdays').addEventListener('change',e=>{const t=e.target,k=t.dataset.pt;if(k)ptSet(t.closest('tr[data-pd]').dataset.pd,k,t.value)});
$('pdays').addEventListener('keydown',e=>{const t=e.target,k=t.dataset.pt;if(!k||e.key!=='Enter')return;e.preventDefault();ptSet(t.closest('tr[data-pd]').dataset.pd,k,t.value)});
const numF=[['pbuf','buf'],['plate','late'],['pmaxd','maxDay'],['pmaxl','maxLeg']];
for(const [id,k] of numF){$(id).value=PS.set[k];$(id).onchange=()=>{const v=$(id).value.trim();PS.set[k]=v===''?(k==='buf'||k==='late'?0:''):Math.max(0,+v||0);changed()}}
$('pobj').value=PS.set.obj;$('pobj').onchange=()=>{PS.set.obj=$('pobj').value;changed()};
// postcode
function pcStatus(){const s=PS.set,el=$('ppcst');$('pnohome').checked=!!s.noHome;$('ppc').disabled=$('ppcgo').disabled=!!s.noHome;
  if(s.noHome){el.className='pcst';el.textContent='Each day starts at the first cinema; times are for the first film and the last film’s end. A day with its own start or end postcode uses that instead.';return}
  if(s.geo&&normPC(s.geo.pc)===normPC(s.pc)){el.className='pcst ok';el.textContent='✓ '+fmtPC(s.geo.pc)+(s.geo.area?' · '+s.geo.area:'')}
  else if(!s.pc){el.className='pcst';el.textContent='Enter the postcode you’ll leave from and return to.'}}
let pcBusy=false;
const PCRE=/^[A-Z]{1,2}[0-9][A-Z0-9]?[0-9][A-Z]{2}$/;
function pcKnown(n,pc){const s=PS.set;
  return D.home&&normPC(D.home.postcode)===n&&hasLL(D.home)?{pc,lat:D.home.lat,lng:D.home.lng,area:(D.home.label||'').replace(/\s*\([^)]*\)\s*$/,'')||'Home'}:s.gc[n]||(s.geo&&normPC(s.geo.pc)===n?s.geo:null)}
// postcodes.io: -> {pc,lat,lng,area}, or null if there's no such postcode; throws if it can't be reached
async function pcFetch(pc){const s=PS.set,ac=typeof AbortController!=='undefined'?new AbortController():null,tm=ac&&setTimeout(()=>ac.abort(),8000);
  const r=await fetch('https://api.postcodes.io/postcodes',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({postcodes:[pc]}),signal:ac&&ac.signal});
  tm&&clearTimeout(tm);if(!r.ok)throw new Error('HTTP '+r.status);
  const j=await r.json(),res=j&&j.result&&j.result[0]&&j.result[0].result;
  if(!res||typeof res.latitude!=='number')return null;
  const g={pc:res.postcode||pc,lat:res.latitude,lng:res.longitude,area:[res.admin_ward,res.admin_district].filter(Boolean).join(', ')};
  s.gc[normPC(pc)]=g;const ks=Object.keys(s.gc);if(ks.length>8)delete s.gc[ks[0]];return g}
async function setPC(){const raw=$('ppc').value,n=normPC(raw),s=PS.set,el=$('ppcst');if(pcBusy)return;
  if(!n){s.pc='';s.geo=null;pcStatus();renderDays();changed();renderGC();renderPicks();banner();return}
  if(!PCRE.test(n)){el.className='pcst bad';el.textContent='That doesn’t look like a UK postcode.';return}
  s.pc=fmtPC(n);$('ppc').value=s.pc;
  let g=pcKnown(n,s.pc);
  if(!g){pcBusy=true;el.className='pcst';el.textContent='Looking up '+s.pc+'…';
    try{g=await pcFetch(s.pc);
      if(!g){pcBusy=false;s.geo=null;el.className='pcst bad';el.textContent='Postcode '+s.pc+' not found.';save();banner();return}
    }catch(err){pcBusy=false;el.className='pcst bad';el.textContent='Couldn’t reach postcodes.io to look up '+s.pc+' (offline?). Try again.'+(s.geo?' Still using '+fmtPC(s.geo.pc)+'.':'');
      if(s.geo)s.pc=fmtPC(s.geo.pc);return}
    pcBusy=false}
  s.geo=g;pcStatus();renderDays();banner();renderGC();renderPicks();changed()}
$('ppc').value=PS.set.pc||'';$('ppcgo').onclick=setPC;$('ppc').onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();setPC()}};
$('ppc').onchange=setPC;
$('pnohome').onchange=e=>{PS.set.noHome=e.target.checked;pcStatus();renderDays();banner();renderGC();renderPicks();changed()};
function updGo(){const ht=homeTravel(),n=PS.picks.filter(p=>FI[p.k]).length,bad=DAYS.find(ptBad);
  $('pgo').disabled=!n||!ht||!!bad||running;if(!gBusy)$('pgo').textContent=running?(Math.min(n,limit())>28?`Planning… (up to ${Math.round(budgetFor(Math.min(n,limit()))*(rkOn().length?2:1)/1000)} s)`:'Planning…'):!ht?'Set a starting postcode first':bad?`Check the postcode on ${dshort(bad)}`:'Plan my schedule'}
// Google Maps key (own key, this browser only)
function keyStatus(){const el=$('pkeyst'),n=Object.keys(GCACHE).length;$('pkeyforget').disabled=!gkey&&!n;
  el.className='pcst'+(gkey?' ok':'');
  el.textContent=gkey?`✓ Key saved in this browser only${n?` · ${n} cached journey time${n>1?'s':''}`:''}.`:n?`${n} cached journey times (unused without a key).`:''}
function setKey(v){v=String(v||'').trim();if(v===gkey)return;gkey=v;lsSet(GK,v||null);keyStatus();tmChanged()}
$('pkey').value=gkey;
$('pkey').onchange=()=>setKey($('pkey').value);
$('pkey').onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();setKey($('pkey').value)}};
$('pkeyshow').onclick=()=>{const k=$('pkey'),show=k.type==='password';k.type=show?'text':'password';$('pkeyshow').textContent=show?'Hide':'Show';$('pkeyshow').setAttribute('aria-pressed',show)};
$('pkeyforget').onclick=()=>{$('pkey').value='';GCACHE={};lsSet(GC,null);gkey='x';setKey('')};
// how you travel: changing it swaps which cached times count; the next plan fetches only what that choice lacks
function tmUI(){const s=PS.set,on=gkey?s.travel:'T',el=$('ptmst');
  $('ptm').querySelectorAll('input').forEach(r=>{r.disabled=r.value!=='T'&&!gkey;r.checked=r.value===on;r.parentNode.classList.toggle('off',r.disabled)});
  $('ptav').hidden=on!=='T';
  for(const [id,k] of [['pnotram','noTram'],['pnotrain','noTrain']]){$(id).disabled=!gkey;$(id).checked=!!gkey&&s[k];$(id).parentNode.classList.toggle('off',!gkey)}
  el.textContent=!gkey?'Cycling, driving and avoiding trams or trains need a Google Maps key (below): the built-in estimates are public transport only and can’t tell trams from trains.'
    :tp==='D'?'Driving times don’t include traffic or parking, so allow for those in your buffer.'
    :tp==='T'||tp==='B'?'':`Google treats this as a preference, so a journey can still use ${tAvoid(tp)} when there’s no practical alternative, and “route” links in Google Maps may suggest them.`;
  el.hidden=!el.textContent}
function tmChanged(){gErr=null;tmSync();tmUI();emptyOut();changed();renderGC();renderPicks();banner()}
$('ptm').onchange=e=>{PS.set.travel=e.target.value;tmChanged()};
for(const [id,k] of [['pnotram','noTram'],['pnotrain','noTrain']])$(id).onchange=e=>{PS.set[k]=e.target.checked;tmChanged()};

// ---------- solver plumbing ----------
let running=false,worker=null,noWorker=false;
// showings each pick allows that could fit their day's window (travel aside), and the cinemas they use
function candidates(){const s=PS.set,buf=+s.buf||0,late=+s.late||0;
  const win=DAYS.map(day=>{const w=avail(day);if(!w||!w.on)return null;return {e:parseHM(w.e)??0,l:homeBy(w)}});
  const used=new Set(),info=[],per=[],N=limit();
  PS.picks.forEach((p,pi)=>{const F=FI[p.k],r={pi,p,match:0,win:0,homeBy:0,hb:[],reason:null,si:-1},L=[];info.push(r);per.push(L);
    if(pi>=N){r.reason='limit';return}   // past the pick limit for this plan length: not planned
    if(!F){r.reason='gone';return}const cs=cinSet(p);
    for(const h of F.shows){if(!allows(p,D.shows[h.i].x,h.c,h.day,cs))continue;r.match++;
      const w=win[h.di];if(!w||w.e>h.st+late)continue;if(h.en+buf>w.l){r.homeBy++;r.hb.push(h);continue}L.push(h);used.add(h.c)}});
  return {win,used,info,per,buf,late}}
// solver time limit: 2.5 s up to 28 films, then +0.625 s per film (14.4 s at 47) since bigger searches need longer to finish
const budgetFor=n=>n>28?2500+(n-28)*625:2500;
function buildProblem(){const s=PS.set,maxLeg=+s.maxLeg||0,ht=homeTravel();
  const {win,used,info,per,buf,late}=candidates(),{T,E}=rawTravel(ht),ends=dayEnds(),pen=prefs();
  // shortest-path closure via candidate cinemas (a journey may route past another cinema): keeps feasibility monotone,
  // which the solver relies on. Home is never an intermediate (in no-home mode its legs are 0 and would act as a
  // teleport); cinemas without candidates aren't either, so their unfetched estimates can't undercut fetched times.
  for(const k of used)for(let a=0;a<NC1;a++){const ak=T[a*NC1+k];if(ak<0)continue;
    for(let b=0;b<NC1;b++){const kb=T[k*NC1+b];if(kb<0)continue;const v=ak+kb,i=a*NC1+b,ab=T[i];if(ab<0||v<ab){T[i]=v;E[i]=E[a*NC1+k]|E[k*NC1+b]}}}
  // a day's own start / end legs get the same closure, but only via that day's candidate cinemas: those are all the day
  // needs, and the only ones fetchTimes() asks about, so an unfetched estimate can't undercut a fetched time. Its points
  // are never intermediates either. Same order as above: closure first, then the journey limit.
  const dayC=DAYS.map(()=>new Set());for(const L of per)for(const h of L)dayC[h.di].add(h.c);
  const legs=(pt,cs,back)=>{const L=new Int32Array(NC),Le=new Uint8Array(NC);for(let c=0;c<NC;c++)[L[c],Le[c]]=homeLeg(pt,c,back);
    const v=L.slice(),e=Le.slice();
    for(const k of cs){if(L[k]<0)continue;for(const c of cs){const i=back?c*NC1+k:k*NC1+c,kc=T[i];if(kc<0)continue;
      const w=L[k]+kc;if(v[c]<0||w<v[c]){v[c]=w;e[c]=Le[k]|E[i]}}}
    if(maxLeg>0)for(let c=0;c<NC;c++)if(v[c]>maxLeg)v[c]=-1;return [v,e]};
  // dl[d] = {s,e: the day's points, a,ae / b,be: minutes and estimate flags by cinema from s / to e, null = T's home legs}
  const dl=ends.map(({s:p,e:q},d)=>{const r={s:p,e:q,a:null,ae:null,b:null,be:null};
    if(p)[r.a,r.ae]=legs(ptTravel(p),dayC[d],false);if(q)[r.b,r.be]=legs(ptTravel(q),dayC[d],true);return r});
  const dayTo=(d,c)=>dl[d].a?dl[d].a[c]:T[H*NC1+c],dayFr=(d,c)=>dl[d].b?dl[d].b[c]:T[c*NC1+H];
  if(maxLeg>0)for(let a=0;a<NC1;a++)for(let b=0;b<NC1;b++)if(a!==b&&T[a*NC1+b]>maxLeg)T[a*NC1+b]=-1;
  const films=[];
  info.forEach((r,pi)=>{if(r.reason)return;const days=DAYS.map(()=>[]);
    for(const h of per[pi]){const w=win[h.di],a=dayTo(h.di,h.c),b=dayFr(h.di,h.c);if(a<0||b<0)continue;
      if(w.e+a>h.st+late)continue;if(h.en+buf+b>w.l){r.homeBy++;r.hb.push(h);continue}r.win++;const o={i:h.i,c:h.c,st:h.st,en:h.en},q=pen&&pen(h.i);if(q)o.p=q;days[h.di].push(o)}
    if(!r.match)r.reason='constraints';else if(!r.win)r.reason='window';else{r.si=films.length;films.push({days})}});
  const own=ends.some(x=>x.s||x.e)?{ds:dl.map(x=>x.a),de:dl.map(x=>x.b)}:{};
  return {P:{films,win,T,nc:NC,buf,late,maxDay:+s.maxDay||0,mode:s.obj,budget:budgetFor(films.length),...own},info,ht,win,T,E,dl,buf,late,mode:s.obj,days:DAYS,lim:limit(),tp,pen}}
// fetch Google times (plus walking, for transit) for every leg the plan can use that isn't cached yet: between cinemas with
// candidate showings, each day's own start / end with that day's cinemas, and home with all of them if some day still uses
// it (the closure in buildProblem routes home's legs via every candidate cinema). Never between home and a point, or two points.
async function fetchTimes(ht){const {used,per,buf,late}=candidates(),cs=[...used].filter(c=>CLL[c]),nodes=cs.map(c=>CLL[c]),h=HLL(ht);
  // cinema a -> b can only be used if, on some day, a candidate showing at a ends (plus buffer) before one at b starts
  const ND=DAYS.length,minEn=new Float64Array(NC*ND).fill(Infinity),maxSt=new Float64Array(NC*ND).fill(-Infinity);
  for(const L of per)for(const x of L){const i=x.c*ND+x.di;if(x.en<minEn[i])minEn[i]=x.en;if(x.st>maxSt[i])maxSt[i]=x.st}
  const link=(a,b)=>{for(let d=0;d<ND;d++)if(minEn[a*ND+d]+buf<=maxSt[b*ND+d]+late)return true;return false};
  if(h)nodes.push(h);
  const hi=cs.length,ids=new Map(),id=q=>{const k=llk(q);if(!ids.has(k)){ids.set(k,nodes.length);nodes.push({lat:q.lat,lng:q.lng})}return ids.get(k)};
  const want=new Set(),on=(c,d)=>minEn[c*ND+d]<Infinity;
  dayEnds().forEach((x,d)=>{if(!cs.some(c=>on(c,d)))return;
    cs.forEach((c,i)=>{if(x.s){if(on(c,d))want.add(id(x.s)+'>'+i)}else if(h)want.add(hi+'>'+i);
      if(x.e){if(on(c,d))want.add(i+'>'+id(x.e))}else if(h)want.add(i+'>'+hi)})});
  const G=GROUTES(),need=[],p=tp;   // p: results are filed under the choice they were asked for, even if it changes meanwhile
  for(let i=0;i<nodes.length;i++)for(let j=0;j<nodes.length;j++){if(i===j)continue;const A=nodes[i],B=nodes[j];
    if(i<cs.length&&j<cs.length?!link(cs[i],cs[j]):!want.has(i+'>'+j))continue;
    const t=!gHit(gk(p,A,B)),w=G.walks(p)&&G.near(A,B)&&!gHit(gk('W',A,B));if(t||w)need.push({i,j,t,w})}
  if(!need.length){gErr=null;return}
  gBusy=true;const prog=(d,n,note)=>{$('pgo').textContent=`Fetching travel times… ${d}/${n}${note?' ('+note+')':''}`};
  let k=0;
  const res=await G.run({key:gkey,pts:nodes,need,prof:p,fetch:(u,o)=>fetch(u,o),onProgress:prog,
    onResult:(m,i,j,v)=>{GCACHE[gk(m,nodes[i],nodes[j])]=[v,TODAY];if(++k%200===0)gSave()}});
  gSave();gBusy=false;keyStatus();
  gErr=res.error?`${res.error} Using ${estW(p)} for the journeys that are missing.`:null}
function solveAsync(P){return new Promise(res=>{
  const local=()=>setTimeout(()=>{let r;try{r=SOLVER().solve(P)}catch(err){r={error:String(err)}}res(r)},20);
  if(noWorker||typeof Worker==='undefined')return local();
  try{if(!worker){const src='const S=('+SOLVER.toString()+')();self.onmessage=e=>{let r;try{r=S.solve(e.data)}catch(err){r={error:String(err&&err.stack||err)}}self.postMessage(r)};';
      worker=new Worker(URL.createObjectURL(new Blob([src],{type:'text/javascript'})))}
    worker.onmessage=e=>res(e.data);
    worker.onerror=e=>{e.preventDefault&&e.preventDefault();noWorker=true;try{worker.terminate()}catch(x){}worker=null;local()};
    worker.postMessage(P)}catch(err){noWorker=true;worker=null;local()}})}
$('pgo').onclick=async()=>{if(running||DAYS.some(ptBad))return;const ht0=homeTravel();if(!ht0)return;
  running=true;updGo();const t=performance.now();
  if(gkey)try{await fetchTimes(ht0)}catch(err){gBusy=false;gErr=`Couldn’t fetch Google travel times (${String(err&&err.message||err)}). Using ${estW(tp)}.`}
  if(gkey&&OPEN.size)renderPicks();updGo();const ctx=buildProblem();
  const r=ctx.P.films.length?await solveAsync(ctx.P):{status:'optimal',days:[],chosen:[],unfit:{},ms:0,nodes:0};
  r.wall=performance.now()-t;running=false;lastRes={r,ctx};dirty=false;$('pout').classList.remove('stale');renderResult();updGo();
  if(window.matchMedia('(max-width:820px)').matches)$('pout').scrollIntoView({behavior:'smooth',block:'start'})};

// ---------- results ----------
const gmap=(o,d,p)=>'https://www.google.com/maps/dir/?api=1'+(o?'&origin='+o.lat+','+o.lng:'')+(d?'&destination='+d.lat+','+d.lng:'')+'&travelmode='+(p==='B'?'bicycling':p==='D'?'driving':'transit');
function renderResult(){const {r,ctx}=lastRes,out=$('pout');
  if(r.error){out.innerHTML=`<div class="banner err">Planner error: ${esc(r.error)}</div>`;lastRes.legs=null;banner();return}
  const {ht,win,T,E,dl,buf}=ctx,noHome=ht.none,anyPt=dl.some(x=>x.s||x.e),anyEnd=dl.some(x=>x.e);
  const nPick=ctx.info.length,nGot=r.chosen.length;let totTr=0,html='',nLeg=0,nEst=0;
  const tilde=e=>{nLeg++;if(e){nEst++;return '~'}return ''};
  // what a leg's time is: transit needs no word; a cycling/driving plan's fallback estimate is not a cycling/driving time
  const how=e=>ctx.tp[0]==='T'?'':e?' (public transport estimate)':ctx.tp==='B'?' cycle':' drive';
  const days=r.days.slice().sort((a,b)=>a.d-b.d);
  for(const dd of days){const w=win[dd.d],o=dl[dd.d],free0=noHome&&!o.s,free1=noHome&&!o.e,seq=dd.seq.map(i=>SH[i]);let tl='',trv=0,t0=null,tEnd=null,dayEst=false;
    seq.forEach((h,j)=>{const s=D.shows[h.i],C=CLL[h.c],nm=D.cinemas[h.c].name;let arr;
      if(j===0){if(free0){arr=h.st;t0=h.st}else{const t=o.a?o.a[h.c]:T[H*NC1+h.c],fe=o.a?o.ae[h.c]:E[H*NC1+h.c],leave=Math.max(w.e,h.st-t);arr=leave+t;t0=leave;trv+=t;
          const ti=tilde(fe);dayEst=dayEst||!!ti;
          tl+=`<div class="ev home">Leave ${o.s?esc(fmtPC(o.s.pc)):'home'} <b>${hm(leave)}</b></div><div class="ev leg">${ti}${dur(t)}${how(fe)} to ${esc(nm)}${C?` · <a href="${gmap(o.s,C,ctx.tp)}" target="_blank" rel="noopener">route</a>`:''}</div>`}}
      else{const p=seq[j-1],P0=CLL[p.c],dep=p.en+buf,t=T[p.c*NC1+h.c];arr=dep+t;trv+=t;
        const wait=h.st-arr,ti=t===0?'':tilde(E[p.c*NC1+h.c]);dayEst=dayEst||!!ti;
        tl+=`<div class="ev leg">${t===0?`Same cinema · next film ${wait>0?'in '+dur(h.st-p.en):'straight after'}`:`Leave ${hm(dep)} · ${ti}${dur(t)}${how(E[p.c*NC1+h.c])} to ${esc(nm)}${P0&&C?` · <a href="${gmap(P0,C,ctx.tp)}" target="_blank" rel="noopener">route</a>`:''}`}${t>0&&wait>=15?` · ${dur(wait)} spare`:''}</div>`}
      const lateBy=Math.round(arr-h.st),bd=tagBadges(s.x);
      tl+=`<div class="ev film"><div><span class="ftm">${hm(h.st)}–${h.est?'~':''}${hm(h.en)}</span> <span class="fti">${esc(title(h.k))}</span></div>
        <div class="fmeta">${esc(nm)}${staleNote(h.c)?' '+staleNote(h.c):''}${s.s?' · '+esc(s.s):''}${s.u?` · <a class="book" href="${esc(s.u)}" target="_blank" rel="noopener">Book</a>`:''}</div>
        ${bd?`<div>${bd}</div>`:''}${lateBy>0?`<div class="note">Arrives ~${lateBy} min after the listed start (during the ads/trailers)</div>`:''}</div>`;
      tEnd=h.en});
    const L=seq[seq.length-1],LC=CLL[L.c];
    if(!free1){const t=o.b?o.b[L.c]:T[L.c*NC1+H],fe=o.b?o.be[L.c]:E[L.c*NC1+H],dep=L.en+buf,ti=tilde(fe),eL=o.e&&esc(fmtPC(o.e.pc));trv+=t;tEnd=dep+t;dayEst=dayEst||!!ti;
      tl+=`<div class="ev leg">Leave ${hm(dep)} · ${ti}${dur(t)}${how(fe)} ${eL?'to '+eL:'home'}${LC?` · <a href="${gmap(LC,o.e,ctx.tp)}" target="_blank" rel="noopener">route</a>`:''}</div><div class="ev home">${eL?'Finish at '+eL:'Home by'} <b>${seq.some(h=>h.est)||dayEst?'~':''}${hm(tEnd)}</b></div>`}
    totTr+=trv;
    html+=`<div class="iday"><div class="ih"><b>${esc(dayLbl(ctx.days[dd.d]))}</b><span class="meta">${seq.length} film${seq.length>1?'s':''} · ${free0&&free1?'':'out '}${hm(t0)}–${hm(tEnd)}${free0&&free1?'':' · '+(dayEst?'~':'')+dur(trv)+' travel'}</span></div><div class="tl">${tl}</div></div>`}
  lastRes.legs={n:nLeg,est:nEst};
  // unfitted
  const why=x=>{const p=x.p;
    if(x.reason==='limit')return `Over the ${ctx.lim}-film limit for a ${Math.max(1,ctx.days.length)}-day plan.`;
    if(x.reason==='gone')return goneMsg(p.k)+'.';
    if(x.reason==='constraints'&&gNote(p))return gNote(p)+'.';
    const s=PS.set;
    if(x.reason==='constraints')return kn(p.cin).length||p.st.length||p.req.length||p.exc.length||ddIn(p).length||kn(s.cin).length||knch(s.ch).length||s.st.length?'No upcoming showings match your cinema / screen type / tag / day choices.':'No upcoming showings.';
    if(x.reason==='window')return `None of its ${x.match} matching showing${x.match>1?'s':''}${p.st.length?' ('+p.st.join(' / ')+')':''} fit your available times${anyPt?' with travel from and to each day’s start and end':noHome?'':' with travel from and back home'}${+PS.set.maxLeg?' and journey limit':''}`+
      (!x.homeBy?'':(x.homeBy===x.match?(x.match>1?' (all of them end':' (it ends'):` (${x.homeBy} of them ${x.homeBy>1?'end':'ends'}`)+' too late for your “Home by” time)')+'.';
    const u=r.unfit[x.si];if(!u)return '';
    if(ctx.mode==='strict'||!u.higher)return 'Clashes with your higher-priority picks.';
    return 'Only fits by dropping other picks. Leaving it out lets you see more films (or higher-ranked ones).'};
  const miss=ctx.info.filter(x=>x.si<0||!r.chosen.includes(x.si)),nRk=ctx.pen?days.reduce((n,dd)=>n+dd.seq.filter(i=>ctx.pen(i)<PREFMAX).length,0):0;
  const st=r.status==='optimal'?`<span class="st opt" title="Search finished: no plan sees more (or higher-priority) films, and none of those ${ctx.pen?'better balances your ranking with travel and waiting':'needs less travel and waiting'}">optimal</span>`
    :r.status==='films'?`<span class="st opt" title="Proven: no plan sees more (or higher-priority) films. ${ctx.pen?'Your ranking, travel and waiting were balanced':'Travel and waiting were minimised'} as far as the time limit allowed">best films · travel near-optimal</span>`
    :`<span class="st part" title="The search stopped at the time limit before it could prove this is the best possible; it is the best plan it found">best found in time</span>`;
  const head=`<div class="rsum"><div class="big">Seeing ${nGot} of ${nPick} film${nPick>1?'s':''}${days.length?` over ${days.length} day${days.length>1?'s':''}`:''}${st}</div>
    <div class="meta">${noHome&&!anyPt||!days.length?'':(nEst?'~':'')+dur(totTr)+' total travel · '}${PS.set.obj==='strict'?'strict priority':'most films'}${ctx.pen&&days.length?` · ${nRk} of ${nGot} at your ranked choices`:''} · solved in ${(r.wall/1000).toFixed(2)} s</div></div>`;
  // showings of the picks that only the "Home by" time ruled out (it defaults to midnight, which hides late showings)
  const nHB=ctx.info.reduce((n,x)=>n+x.homeBy,0),allMid=ctx.days.every(d=>{const a=avail(d);return !a.on||a.l==='00:00'});
  const hb=nHB?`<div class="banner">${nHB} showing${nHB>1?'s':''} of your picks ${nHB>1?'were':'was'} left out because ${nHB>1?'they finish':'it finishes'} too late ${anyEnd?'to reach the day’s end point by':noHome?'for':'to get home by'} your “Home by” time.
    ${allMid?'“Home by” is midnight unless you change it':'You can change it'} under <b>When you’re free</b> in Settings.
    <details class="hbl"><summary>Show ${nHB>1?'which showings':'which showing'}</summary><div class="hbw">${ctx.info.filter(x=>x.hb.length).map(x=>`<b>${esc(FI[x.p.k]?title(x.p.k):x.p.t)}</b>${miss.includes(x)?'':' <span class="meta">(in your plan at another time)</span>'}`+
      x.hb.slice().sort((a,b)=>a.day<b.day?-1:a.day>b.day?1:a.st-b.st).map(h=>`<div>${esc(dayLbl(h.day))} ${hm(h.st)}–${hm(h.en)} · ${esc(D.cinemas[h.c].name)}</div>`).join('')).join('')}</div></details></div>`:'';
  html=head+hb+(days.length?html:'<div class="pempty">Nothing could be scheduled. See below.</div>');
  if(miss.length)html+=`<div class="unf"><h3>Not scheduled (${miss.length})</h3>${miss.map(x=>`<div><b>${x.pi+1}. ${esc(FI[x.p.k]?title(x.p.k):x.p.t)}</b><span class="meta">${esc(why(x))}</span></div>`).join('')}</div>`;
  out.innerHTML=`<div class="res">${html}</div>`;banner()}

function emptyOut(){if(!lastRes)$('pout').innerHTML='<div class="pempty">Add the films you want to see, set when you’re free, then press <b>Plan my schedule</b>.<br><br>The planner finds the schedule over your chosen dates that fits the most of your picks (or follows strict priority), using '+(tp==='B'?'cycling':tp==='D'?'driving':'public transport and walking')+' times between cinemas.</div>'}
dateNote();renderDays();renderGC();renderAds();renderRank();pcStatus();keyStatus();tmUI();banner();renderPicks();emptyOut();
}
