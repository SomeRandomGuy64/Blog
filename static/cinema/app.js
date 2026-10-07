/* Fazon Nexus · Cinema Planner — boot: fetch the listings from the API, show loading / error states, then
   start the Listings (listings.js) and Schedule (planner.js) tabs.
   API URL: the wrapper's data-api (front-matter param `api`). For local testing, ?api=<url> overrides it, but only for
   http://localhost or http://127.0.0.1 URLs. */
(function(){
const root=document.getElementById('cinema-app');if(!root)return;
const $=id=>document.getElementById(id);
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
function apiURL(){let u=root.dataset.api||'';
  try{const o=new URLSearchParams(location.search).get('api');
    if(o){const x=new URL(o,location.href);
      if(x.protocol==='http:'&&(x.hostname==='localhost'||x.hostname==='127.0.0.1'))u=x.href;
      else console.warn('Cinema Planner: ignoring ?api= (only http://localhost or http://127.0.0.1 URLs are allowed)')}}catch(e){}
  return u}
const API=apiURL(),st=$('cstatus'),body=$('cbody');
let loading=false;
function showError(title,detail,retry){
  st.innerHTML=`<div class="cerr" role="alert"><b>${esc(title)}</b><span class="meta">${esc(detail)}</span>`+
    (retry?'<button type="button" id="cretry">Try again</button>':'')+'</div>';
  st.hidden=false;const b=$('cretry');if(b){b.onclick=load;b.focus({preventScroll:true})}}
function valid(D){return D&&typeof D==='object'&&Array.isArray(D.cinemas)&&Array.isArray(D.shows)&&D.films&&typeof D.films==='object'&&
  typeof D.from==='string'&&typeof D.to==='string'}
async function load(){if(loading)return;loading=true;
  st.innerHTML='<div class="cload" role="status"><span class="spin" aria-hidden="true"></span>Loading listings…</div>';st.hidden=false;
  let D;
  try{if(!API)throw new Error('no API address configured');
    const ac=typeof AbortController!=='undefined'?new AbortController():null,tm=ac&&setTimeout(()=>ac.abort(),30000);
    let r;try{r=await fetch(API,{headers:{Accept:'application/json'},signal:ac?ac.signal:undefined})}
    catch(err){throw new Error(err&&err.name==='AbortError'?'the server took too long to answer':'couldn’t reach the listings server (offline, or it’s down)')}
    finally{tm&&clearTimeout(tm)}
    if(!r.ok)throw new Error(`the listings server answered HTTP ${r.status}`);
    try{D=await r.json()}catch(err){throw new Error('the listings server sent something that isn’t valid listings data')}
    if(!valid(D))throw new Error('the listings data is incomplete');
  }catch(err){loading=false;showError('Couldn’t load the cinema listings.',String(err&&err.message||err)+'.',true);return}
  loading=false;
  try{st.hidden=true;st.innerHTML='';body.hidden=false;
    const L=CINEMA_LISTINGS(D);CINEMA_PLANNER(D,L)}
  catch(err){console.error(err);body.hidden=true;
    showError('Something went wrong displaying the listings.',String(err&&err.message||err),false)}}
load();
})();
