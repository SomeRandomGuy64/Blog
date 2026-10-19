/* Fazon Nexus · Cinema Planner — feedback box + public comment list, below every tab.
   CINEMA_FEEDBACK(url): url = the API's /v1/feedback. No login. The server allows a fixed number of posts per UTC day
   shared by everyone (and a per-post character cap); both come back with every response and are shown to the viewer.
   Works whether or not the listings loaded. Draft and name are remembered in this browser only.
   Owner mode: open the page with ?owner and enter the server token once (kept in this browser) to reply to posts and
   delete posts or replies. Replies are public, shown under their post, and don't use the daily quota. */
function CINEMA_FEEDBACK(url){
const $=id=>document.getElementById(id),box=$('cfb');if(!box||!url)return;
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const NK='fazon-cinema-fbname',DK='fazon-cinema-fbdraft',OK='fazon-cinema-owner',PAGE=20,base=url.replace(/\/+$/,'');
const store={get(k){try{return localStorage.getItem(k)||''}catch(e){return ''}},
             set(k,v){try{v?localStorage.setItem(k,v):localStorage.removeItem(k)}catch(e){}}};
const len=s=>[...s.trim()].length;   // code points, as the server counts
const form=$('fbform'),name=$('fbname'),text=$('fbtext'),go=$('fbgo'),msg=$('fbmsg');
let S=null,skew=0,shown=PAGE,busy=false,tok=store.get(OK),own=false;
const open=new Set(),drafts={};   // owner: posts with an open reply box, and their unsent text
const now=()=>Date.now()+skew;   // server clock
const hhmm=d=>d.toLocaleTimeString('en-GB',{hour:'2-digit',minute:'2-digit'});
function left(ms){const m=Math.max(1,Math.ceil(ms/6e4));return m<60?m+' min':Math.floor(m/60)+' h'+(m%60?' '+(m%60)+' min':'')}
function ago(t){const d=new Date(t),m=(now()-d)/6e4;
  if(m<1.5)return 'just now';if(m<60)return Math.round(m)+' min ago';if(m<36*60)return Math.round(m/60)+' h ago';
  return d.toLocaleDateString('en-GB',{day:'numeric',month:'short',year:d.getFullYear()===new Date().getFullYear()?undefined:'numeric'})}

function quota(){if(!S)return;
  const r=new Date(S.resets_at),rem=Math.max(0,S.remaining),dots='<span class="fbdots" aria-hidden="true">'+
    Array.from({length:S.limit},(_,i)=>`<i class="${i<S.used?'u':''}"></i>`).join('')+'</span>';
  const reset=`resets at 00:00 UTC (${esc(hhmm(r))} your time, in ${left(r-now())})`;
  $('fbquota').className='fbquota'+(rem?'':' full');
  $('fbquota').innerHTML=dots+(rem?`<span><b>${rem} of ${S.limit}</b> posts left today for everyone · ${reset}</span>`
    :`<span><b>Today’s ${S.limit} posts have all been used.</b> Posting reopens when the count ${reset}.</span>`);
  if(now()>=r)load()}   // the day rolled over: fetch the fresh count
function chars(){const n=len(text.value),max=S?S.max_chars:1000,over=n>max;
  $('fbchars').textContent=`${n} / ${max} characters`;$('fbchars').classList.toggle('over',over);
  const full=S&&S.remaining<=0;
  go.disabled=busy||!S||full||!n||over||len(name.value)>S.max_name;
  text.disabled=name.disabled=!!full;
  return over}
const when=t=>`<time class="meta" datetime="${esc(t)}" title="${esc(new Date(t).toLocaleString('en-GB'))}">${esc(ago(t))}</time>`;
const btn=(act,id,label,cls,rid)=>`<button type="button" class="ib${cls?' '+cls:''}" data-act="${act}" data-id="${esc(id)}"${rid?` data-rid="${esc(rid)}"`:''}>${label}</button>`;
const replies=x=>(x.replies||[]).map(r=>`<div class="fbrep"><div class="fbh"><b>${esc(r.name)}</b><span class="fbbadge">Site owner</span>${when(r.created)}`+
  `${own?btn('rdel',x.id,'Delete reply','fbx',r.id):''}</div><div class="fbt">${esc(r.text)}</div></div>`).join('');
function ownerUI(x){if(!own)return '';
  if(!open.has(x.id))return `<div class="fbown">${btn('reply',x.id,'Reply')}${btn('del',x.id,'Delete post','fbx')}</div>`;
  return `<div class="fbrbox"><textarea class="fbrtext" data-id="${esc(x.id)}" rows="4" placeholder="Your reply (public)" aria-label="Reply">${esc(drafts[x.id]||'')}</textarea>`+
    `<div class="fbfoot"><span class="fbmsg" data-msg="${esc(x.id)}"></span>${btn('cancel',x.id,'Cancel')}<button type="button" class="fbgo" data-act="send" data-id="${esc(x.id)}">Post reply</button></div></div>`}
function list(){if(!S)return;const it=S.items;
  $('fbcount').textContent=it.length?it.length+' post'+(it.length===1?'':'s'):'';
  $('fblist').innerHTML=it.length?it.slice(0,shown).map(x=>`<li class="fbitem"><div class="fbh"><b>${esc(x.name||'Anonymous')}</b>${when(x.created)}</div>`+
      `<div class="fbt">${esc(x.text)}</div>${replies(x)}${ownerUI(x)}</li>`).join('')
    :'<li class="fbempty">No feedback yet. Be the first!</li>';
  $('fbmore').hidden=it.length<=shown}
function apply(st){if(!st||!Array.isArray(st.items))return;S=st;
  const t=Date.parse(st.now);if(!isNaN(t))skew=t-Date.now();
  $('fblimit').textContent=S.limit;$('fbmaxc').textContent=S.max_chars;name.maxLength=S.max_name;
  quota();chars();list()}
function say(s,ok){msg.textContent=s||'';msg.className='fbmsg'+(ok?' ok':'')}

async function load(){
  try{const r=await fetch(url,{headers:{Accept:'application/json'},cache:'no-store'});
    if(!r.ok)throw new Error('HTTP '+r.status);apply(await r.json());box.hidden=false;form.hidden=false}
  catch(e){if(!S){box.hidden=false;form.hidden=true;$('fbquota').className='fbquota full';
    $('fbquota').textContent='Feedback is unavailable right now (couldn’t reach the server). Try again later.'}}}

form.onsubmit=async e=>{e.preventDefault();if(go.disabled)return;
  busy=true;chars();say('Posting…',true);
  try{const r=await fetch(url,{method:'POST',headers:{'Content-Type':'application/json',Accept:'application/json'},
      body:JSON.stringify({name:name.value.trim(),text:text.value,website:$('fbhp').value})});
    let b=null;try{b=await r.json()}catch(err){}
    if(b)apply(b);
    if(r.status===201){text.value='';store.set(DK,'');store.set(NK,name.value.trim());shown=PAGE;list();say('Thanks, your feedback is posted below.',true)}
    else if(r.status===429)say('Sorry, the daily limit was reached before your post went through. Your text is kept here for tomorrow.');
    else say('Couldn’t post: '+((b&&b.error)||'HTTP '+r.status)+'.')}
  catch(err){say('Couldn’t reach the server. Your text is kept, so try again.')}
  busy=false;chars()};
text.oninput=()=>{store.set(DK,text.value);chars();if(msg.classList.contains('ok'))say('')};
name.oninput=chars;
$('fbmore').onclick=()=>{shown+=PAGE;list()};

// ---- owner mode ----
const owner=(method,path,body)=>fetch(base+path,{method,cache:'no-store',body:body?JSON.stringify(body):undefined,
  headers:Object.assign({Authorization:'Bearer '+tok,Accept:'application/json'},body?{'Content-Type':'application/json'}:{})});
const bar=document.createElement('div');bar.className='fbowner';
function ownerBar(note){
  bar.innerHTML=own?`<span><b>Owner mode.</b> You can reply to posts and delete posts or replies.</span><button type="button" class="ib" id="fbout">Sign out</button>`
    :`<input type="password" id="fbtok" placeholder="Owner token" aria-label="Owner token" autocomplete="off"><button type="button" class="ib" id="fbin">Unlock</button>`+
     (note?`<span class="fbmsg">${esc(note)}</span>`:'');
  if(own)$('fbout').onclick=()=>{tok='';store.set(OK,'');own=false;open.clear();ownerBar();list()};
  else{const go=()=>{tok=$('fbtok').value.trim();if(tok)check()};$('fbin').onclick=go;$('fbtok').onkeydown=e=>{if(e.key==='Enter')go()}}}
async function check(){
  try{const r=await owner('GET','/owner');own=r.status===204;
    if(own)store.set(OK,tok);else if(r.status===401){tok='';store.set(OK,'');ownerBar('That token didn’t work.');return}
    ownerBar(own?'':'Couldn’t check the token (HTTP '+r.status+').')}
  catch(e){own=false;ownerBar('Couldn’t reach the server.')}
  list()}
$('fblist').addEventListener('input',e=>{const t=e.target;if(t.classList.contains('fbrtext'))drafts[t.dataset.id]=t.value});
$('fblist').addEventListener('click',async e=>{const b=e.target.closest('button[data-act]');if(!b||!own)return;
  const id=b.dataset.id,act=b.dataset.act,note=s=>{const m=$('fblist').querySelector(`[data-msg="${CSS.escape(id)}"]`);if(m)m.textContent=s};
  if(act==='reply'){open.add(id);list();const t=$('fblist').querySelector(`.fbrtext[data-id="${CSS.escape(id)}"]`);if(t)t.focus();return}
  if(act==='cancel'){open.delete(id);list();return}
  if((act==='del'&&!confirm('Delete this post for everyone?'))||(act==='rdel'&&!confirm('Delete this reply?')))return;
  b.disabled=true;
  try{let r;
    if(act==='send'){const v=(drafts[id]||'').trim();if(!v){note('Write a reply first.');b.disabled=false;return}
      r=await owner('POST','/'+encodeURIComponent(id)+'/replies',{text:v});let j=null;try{j=await r.json()}catch(err){}
      if(r.status===201){open.delete(id);delete drafts[id];if(j)apply(j);return}
      note('Couldn’t post: '+((j&&j.error)||'HTTP '+r.status)+'.')}
    else{r=await owner('DELETE','/'+encodeURIComponent(id)+(act==='rdel'?'/replies/'+encodeURIComponent(b.dataset.rid):''));
      if(r.status===204||r.status===404){await load();return}alert('Couldn’t delete (HTTP '+r.status+').')}
    if(r.status===401){own=false;tok='';store.set(OK,'');ownerBar('The token stopped working.');list()}}
  catch(err){note('Couldn’t reach the server.')}
  b.disabled=false});
if(tok||/[?&]owner\b/.test(location.search)){box.querySelector('.ph').after(bar);ownerBar();if(tok)check()}

name.value=store.get(NK);text.value=store.get(DK);chars();
load();setInterval(()=>{quota();if(S&&!open.size)list()},30000);   // don't redraw over an open reply box
}
