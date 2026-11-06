/* Fazon Nexus · Cinema Planner — Google Routes API client (user-supplied key). */
// Google Routes API "Compute Route Matrix" from the browser with the user's OWN key (sent only in a header; no key or
// coordinates ever go in a URL). Self-contained so test_solver.js can drive it with a fake fetch.
// run({key, pts:[{lat,lng}], need:[{i,j,t,w}] (t: main mode wanted, w: WALK wanted), prof? (see prof(), default 'T'), fetch,
//      onProgress(done,total,note), onResult(prof|'W', i, j, minutes|null), depart?, sleep?, concurrency?}) -> {requests, error|null}
// late({key, asks:[{a,b,depart,prof}], fetch, onResult(ask, minutes|null), sleep?}) -> {error|null}: one "Compute Routes" request
// per journey, for a late journey home. A matrix response has no departure times, so it can't tell a 40-minute trip from
// a 40-minute trip after the first morning train; Google doesn't document whether its duration includes that wait.
function GROUTES(){
const URL_='https://routes.googleapis.com/distanceMatrix/v2:computeRouteMatrix';
const MASK='originIndex,destinationIndex,duration,condition,status';
const ROUTE_URL='https://routes.googleapis.com/directions/v2:computeRoutes';
const LATE_MASK='routes.duration,routes.legs.steps.transitDetails.stopDetails.departureTime,routes.legs.steps.transitDetails.stopDetails.arrivalTime';
const MAX_WAIT=90;                          // a first bus or train further off than this is no way home that night
const MAX_WALK=60;                          // a walk-only answer longer than this is no public transport, not a way home
const MODES=['BUS','LIGHT_RAIL','RAIL'];   // TransitTravelMode: RAIL = TRAIN + SUBWAY + LIGHT_RAIL; LIGHT_RAIL = tram
// Travel profile: names a set of times, so it is also the cache prefix. 'T' is the original key, which keeps old cached
// times valid. Google documents allowedTravelModes as preferred modes: a route may still use others, and a matrix
// response has no steps to check that with.
const ALLOW={T:MODES,Tt:['BUS','SUBWAY','TRAIN'],Tr:['BUS','SUBWAY','LIGHT_RAIL'],Ttr:['BUS','SUBWAY']};
const prof=(mode,noTram,noTrain)=>mode==='B'||mode==='D'?mode:'T'+(noTram?'t':'')+(noTrain?'r':'');
const walks=p=>p[0]==='T';                  // only transit takes WALK as an alternative: a bike or car has to come along
const MAX_TRANSIT=100,MAX_OTHER=625;        // documented per-request element caps (origins x destinations); 100 also
                                            // applies to TRAFFIC_AWARE_OPTIMAL, which BICYCLE/DRIVE never ask for
const WALK_KM=3,PER_MIN=2500;               // also ask for WALK below 3 km; stay under the 3,000 elements/min quota
const hav=(a,b)=>{const r=Math.PI/180,x=Math.sin((b.lat-a.lat)*r/2)**2+Math.cos(a.lat*r)*Math.cos(b.lat*r)*Math.sin((b.lng-a.lng)*r/2)**2;return 12742*Math.asin(Math.sqrt(x))};
const near=(a,b)=>hav(a,b)<WALK_KM;
const wp=p=>({waypoint:{location:{latLng:{latitude:p.lat,longitude:p.lng}}}});
// tomorrow 17:00 local time, RFC 3339 UTC — a representative early-evening departure
function departure(now){const d=new Date(now==null?Date.now():now);d.setDate(d.getDate()+1);d.setHours(17,0,0,0);return d.toISOString().replace(/\.\d{3}Z$/,'Z')}
// one origin per request (only missing pairs are billed); destinations chunked to the element cap
// BICYCLE/DRIVE send no departureTime or routingPreference: that is TRAFFIC_UNAWARE, so times ignore live traffic
function requests(need,pts,depart,p='T'){const by=new Map(),tr=walks(p),cap=tr?MAX_TRANSIT:MAX_OTHER;
  const how=tr?{travelMode:'TRANSIT',departureTime:depart,transitPreferences:{allowedTravelModes:ALLOW[p]}}:{travelMode:p==='B'?'BICYCLE':'DRIVE'};
  for(const n of need){const o=by.get(n.i)||by.set(n.i,{t:[],w:[]}).get(n.i);if(n.t)o.t.push(n.j);if(n.w&&tr)o.w.push(n.j)}
  const out=[];
  for(const [i,{t,w}] of [...by.entries()].sort((a,b)=>a[0]-b[0])){
    for(let k=0;k<t.length;k+=cap){const d=t.slice(k,k+cap);
      out.push({mode:p,i,dests:d,body:{origins:[wp(pts[i])],destinations:d.map(j=>wp(pts[j])),...how}})}
    for(let k=0;k<w.length;k+=MAX_OTHER){const d=w.slice(k,k+MAX_OTHER);
      out.push({mode:'W',i,dests:d,body:{origins:[wp(pts[i])],destinations:d.map(j=>wp(pts[j])),travelMode:'WALK'}})}}
  return out}
// response: a JSON array of RouteMatrixElement (the "stream"); tolerate one object or concatenated/newline-split objects
function elements(text){text=String(text||'').trim();if(!text)return [];
  try{const j=JSON.parse(text);return Array.isArray(j)?j:j&&typeof j==='object'?[j]:[]}catch(e){}
  const out=[];let depth=0,s=-1,q=false,bs=false;
  for(let k=0;k<text.length;k++){const ch=text[k];
    if(q){if(bs)bs=false;else if(ch==='\\')bs=true;else if(ch==='"')q=false;continue}
    if(ch==='"')q=true;else if(ch==='{'){if(!depth++)s=k}else if(ch==='}'&&depth&&!--depth){try{out.push(JSON.parse(text.slice(s,k+1)))}catch(e){}}}
  return out}
// -> Map destIndexInChunk -> minutes|null. Proto3 JSON omits zero ints, so a missing index means 0.
function parse(els,nd){const m=new Map();
  for(const el of els){if(!el||typeof el!=='object')continue;const oi=el.originIndex||0,di=el.destinationIndex||0;
    if(oi!==0||!(di>=0&&di<nd))continue;
    const ok=el.condition==='ROUTE_EXISTS'&&!(el.status&&el.status.code),sec=parseFloat(String(el.duration||''));
    const v=ok&&isFinite(sec)?Math.ceil(sec/60):null;
    if(!m.has(di)||(v!=null&&(m.get(di)==null||v<m.get(di))))m.set(di,v)}
  return m}
const errText=(status,body)=>{let msg='';try{const j=JSON.parse(body),e=Array.isArray(j)?j[0]&&j[0].error:j.error;msg=e&&(e.message||e.status)||''}catch(x){}
  return msg.replace(/\s+/g,' ').slice(0,220).replace(/[.\s]+$/,'')};
async function run(o){const f=o.fetch,sleep=o.sleep||(ms=>new Promise(r=>setTimeout(r,ms))),prog=o.onProgress||(()=>{});
  const reqs=requests(o.need,o.pts,o.depart||departure(),o.prof||'T'),N=reqs.length;let done=0,next=0,fatal=null,soft=null;const win=[];
  prog(0,N);
  async function one(q){const n=q.dests.length;
    for(;;){const now=Date.now();while(win.length&&now-win[0][0]>60000)win.shift();   // per-minute element budget
      const used=win.reduce((a,x)=>a+x[1],0);if(!win.length||used+n<=PER_MIN)break;
      prog(done,N,'waiting for Google’s per-minute limit');await sleep(60500-(now-win[0][0]))}
    win.push([Date.now(),n]);
    for(let attempt=0;;attempt++){if(fatal)return;let r,body='';
      const ac=typeof AbortController!=='undefined'?new AbortController():null,tm=ac&&setTimeout(()=>ac.abort(),30000);
      try{r=await f(URL_,{method:'POST',headers:{'Content-Type':'application/json','X-Goog-Api-Key':o.key,'X-Goog-FieldMask':MASK},
          body:JSON.stringify(q.body),signal:ac?ac.signal:undefined});body=await r.text()}
      catch(err){tm&&clearTimeout(tm);
        if(attempt<1){await sleep(1500);continue}
        fatal='Couldn’t reach Google’s Routes API (offline, or the request was blocked).';return}
      tm&&clearTimeout(tm);
      if(r.ok){const m=parse(elements(body),n);q.dests.forEach((j,k)=>o.onResult&&o.onResult(q.mode,q.i,j,m.has(k)?m.get(k):null));return}
      const msg=errText(r.status,body);
      if((r.status===429||r.status>=500)&&attempt<3){prog(done,N,r.status===429?'rate-limited, retrying':'retrying');await sleep(4000*2**attempt);continue}
      if(r.status===400||r.status===401||r.status===403||r.status===404){
        fatal=`Google rejected the request (HTTP ${r.status}${msg?': '+msg:''}). Check the API key is right, that the Routes API is enabled for its project, and that the key’s restrictions allow this page.`;return}
      soft=`Some journeys couldn’t be fetched from Google (HTTP ${r.status}${msg?': '+msg:''}).`;return}}
  const workers=Array.from({length:Math.min(o.concurrency||4,N)},async()=>{
    while(!fatal&&next<N){const q=reqs[next++];await one(q);done++;prog(done,N)}});
  await Promise.all(workers);
  return {requests:N,error:fatal||soft||null}}
const lateBody=q=>({origin:wp(q.a).waypoint,destination:wp(q.b).waypoint,travelMode:'TRANSIT',departureTime:q.depart,transitPreferences:{allowedTravelModes:ALLOW[q.prof]}});
// a Compute Routes response -> minutes from `depart`, or null: no route, or the first service is too far off. The wait for
// the first service counts, whether or not Google's duration has it. Without stop times (a walk-only route) it's the duration,
// up to MAX_WALK: Google answers a transit request with a long walk when nothing runs.
function lateMins(j,depart){const r=j&&Array.isArray(j.routes)&&j.routes[0],t0=Date.parse(depart);if(!r)return null;
  const sec=parseFloat(String(r.duration||''));if(!isFinite(sec))return null;
  const stops=[];for(const l of r.legs||[])for(const s of l.steps||[])if(s.transitDetails&&s.transitDetails.stopDetails)stops.push(s.transitDetails.stopDetails);
  if(!stops.length)return sec/60<=MAX_WALK?Math.ceil(sec/60):null;
  const a=Date.parse(stops[0].departureTime),z=Date.parse(stops[stops.length-1].arrivalTime);
  if((a-t0)/6e4>MAX_WAIT)return null;
  return Math.ceil(Math.max(sec/60,z>t0?(z-t0)/6e4:0))}
async function late(o){const sleep=o.sleep||(ms=>new Promise(r=>setTimeout(r,ms)));
  for(const q of o.asks)for(let attempt=0;;attempt++){let r,body='';
    const ac=typeof AbortController!=='undefined'?new AbortController():null,tm=ac&&setTimeout(()=>ac.abort(),30000);
    try{r=await o.fetch(ROUTE_URL,{method:'POST',headers:{'Content-Type':'application/json','X-Goog-Api-Key':o.key,'X-Goog-FieldMask':LATE_MASK},
        body:JSON.stringify(lateBody(q)),signal:ac?ac.signal:undefined});body=await r.text()}
    catch(err){tm&&clearTimeout(tm);
      if(attempt<1){await sleep(1500);continue}
      return {error:'Couldn’t reach Google’s Routes API (offline, or the request was blocked).'}}
    tm&&clearTimeout(tm);
    if(r.ok){let j;try{j=JSON.parse(body||'{}')}catch(e){return {error:'Google sent a reply that couldn’t be read.'}}
      o.onResult(q,lateMins(j,q.depart));break}
    if((r.status===429||r.status>=500)&&attempt<2){await sleep(4000*2**attempt);continue}
    const msg=errText(r.status,body);return {error:`Google rejected the request (HTTP ${r.status}${msg?': '+msg:''}).`}}
  return {error:null}}
return {run,requests,elements,parse,departure,near,prof,walks,late,lateBody,lateMins,URL:URL_,ROUTE_URL,MASK,LATE_MASK,MODES}}
