/* Fazon Nexus · Cinema Planner — regions: which parts of the UK the listings cover.
   CINEMA_REGIONS() returns the pure helpers (saved choice, API URL, summaries; tested in cinema-frontend-tests) and
   picker(D, note), which wires the header's region picker for listings data D. The API sends D.regions (every region in
   display order with its cinema count) and D.region (the keys this document covers); an older API sends neither and the
   picker stays hidden. Applying a new choice saves it (fazon-cinema-regions) and reloads the page: the Listings and
   Schedule tabs bind their handlers once and aren't built to be re-initialised. */
function CINEMA_REGIONS(){
const KEY='fazon-cinema-regions',WARN=200;
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
// region keys: plausible strings only, de-duplicated and sorted (so the API, and any cache in front of it, sees one spelling)
const clean=v=>Array.isArray(v)?[...new Set(v.filter(k=>typeof k==='string'&&/^[a-z0-9-]{1,40}$/.test(k)))].sort():[];
function load(){try{return clean(JSON.parse(localStorage.getItem(KEY)||'[]'))}catch(e){return []}}
function save(keys){const k=clean(keys);try{if(k.length)localStorage.setItem(KEY,JSON.stringify(k));else localStorage.removeItem(KEY)}catch(e){}}
// the listings URL for these keys (none = the server's default regions); other query parameters are kept, commas left readable
function url(api,keys){const k=clean(keys),u=new URL(api,typeof location!=='undefined'?location.href:undefined);
  if(k.length)u.searchParams.set('regions',k.join(','));else u.searchParams.delete('regions');
  u.search=u.searchParams.toString().replace(/%2C/gi,',');return u.href}
// every region in the data, [{key,name,cinemas}] in display order, or null (an older API without regions)
const list=D=>{const L=D&&Array.isArray(D.regions)?D.regions.filter(r=>r&&typeof r.key==='string'&&r.key)
  .map(r=>({key:r.key,name:String(r.name||r.key),cinemas:+r.cinemas>0?Math.round(+r.cinemas):0})):[];return L.length?L:null};
// the keys a document covers: D.region, else the regions its cinemas name
const covered=D=>clean(D&&Array.isArray(D.region)?D.region:(D&&Array.isArray(D.cinemas)?D.cinemas:[]).map(c=>c&&c.region));
// "North West", "North West + Wales", "North West + 2 more", "All of the UK" (names in display order)
function summary(keys,regions){const s=new Set(keys),R=regions||[],L=R.filter(r=>s.has(r.key));
  if(!L.length)return keys.length?keys.length+' region'+(keys.length>1?'s':''):'No region';
  if(R.length>1&&L.length===R.length)return 'All of the UK';
  return L.length===1?L[0].name:L.length===2?L[0].name+' + '+L[1].name:L[0].name+' + '+(L.length-1)+' more'}
const total=(keys,regions)=>{const s=new Set(keys);return (regions||[]).reduce((t,r)=>t+(s.has(r.key)?r.cinemas:0),0)};

// ---------- header picker ----------
// note: shown under the button (e.g. the saved choice was dropped because the server no longer knows a region)
function picker(D,note){const R=list(D),$=id=>document.getElementById(id),root=$('rgn');if(!R||!root)return;
  const cur=covered(D),saved=load(),btn=$('rgnb'),pan=$('rgnp');
  $('rgnv').textContent=summary(cur,R);btn.title='Showing cinemas in: '+(R.filter(r=>cur.includes(r.key)).map(r=>r.name).join(', ')||'the default regions');
  if(note){$('rgnn').textContent=note;$('rgnn').hidden=false}
  pan.innerHTML='<fieldset><legend>Regions to load</legend><div class="rgl">'+R.map(r=>{const on=cur.includes(r.key),z=!r.cinemas;
      return `<label${z?' class="off"':''}><input type="checkbox" value="${esc(r.key)}"${on?' checked':''}${z&&!on?' disabled':''}> <span>${esc(r.name)}</span>`+
        `<span class="meta">${z?'none yet':r.cinemas+' cinema'+(r.cinemas===1?'':'s')}</span></label>`}).join('')+'</div></fieldset>'+
    '<div class="rgt" id="rgnt" aria-live="polite"></div><div class="rga">'+
    (saved.length?'<button type="button" class="ib" data-a="def" title="Forget your choice and load the default regions">Default</button>':'')+
    '<button type="button" class="ib" data-a="no">Cancel</button><button type="button" class="rgo" data-a="ok">Apply</button></div>';
  const boxes=[...pan.querySelectorAll('input')],ok=pan.querySelector('[data-a="ok"]'),sel=()=>boxes.filter(i=>i.checked).map(i=>i.value).sort();
  function upd(){const s=sel(),n=total(s,R);ok.disabled=!s.length;
    $('rgnt').innerHTML=s.length?esc(`${s.length} region${s.length>1?'s':''} · ${n.toLocaleString()} cinema${n===1?'':'s'}`)+
      (n>WARN?' <span class="rgw">· large selections load slower</span>':''):'<span class="rgw">Pick at least one region.</span>'}
  function show(v){pan.hidden=!v;btn.setAttribute('aria-expanded',String(v));root.classList.toggle('open',v);
    if(v){const f=boxes.find(i=>!i.disabled);f&&f.focus({preventScroll:true})}}
  const close=()=>{if(pan.hidden)return;boxes.forEach(i=>{i.checked=cur.includes(i.value)});upd();show(false)};   // unapplied changes are dropped
  const go=()=>{pan.querySelectorAll('button,input').forEach(b=>b.disabled=true);ok.textContent='Loading…';location.reload()};
  btn.onclick=()=>pan.hidden?show(true):close();
  pan.onchange=upd;
  pan.onclick=e=>{const b=e.target.closest('[data-a]');if(!b)return;const a=b.dataset.a;
    if(a==='no'){close();btn.focus()}
    else if(a==='def'){save([]);go()}
    else if(a==='ok'){const s=sel();if(!s.length)return;if(s.join()===cur.join()){close();btn.focus();return}save(s);go()}};
  root.addEventListener('keydown',e=>{if(e.key==='Escape'&&!pan.hidden){e.preventDefault();close();btn.focus()}});
  root.addEventListener('focusout',e=>{if(e.relatedTarget&&!root.contains(e.relatedTarget))close()});
  document.addEventListener('pointerdown',e=>{if(!root.contains(e.target))close()});
  upd();root.hidden=false}
return {KEY,WARN,clean,load,save,url,list,covered,summary,total,picker};
}
