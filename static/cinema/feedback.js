/* Fazon Nexus · Cinema Planner — feedback box + public comment list, below every tab.
   CINEMA_FEEDBACK(url): url = the API's /v1/feedback. No login. The server allows a fixed number of posts per UTC day
   shared by everyone (and a per-post character cap); both come back with every response and are shown to the viewer.
   Works whether or not the listings loaded. Draft and name are remembered in this browser only. */
function CINEMA_FEEDBACK(url){
const $=id=>document.getElementById(id),box=$('cfb');if(!box||!url)return;
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const NK='fazon-cinema-fbname',DK='fazon-cinema-fbdraft',PAGE=20;
const store={get(k){try{return localStorage.getItem(k)||''}catch(e){return ''}},
             set(k,v){try{v?localStorage.setItem(k,v):localStorage.removeItem(k)}catch(e){}}};
const len=s=>[...s.trim()].length;   // code points, as the server counts
const form=$('fbform'),name=$('fbname'),text=$('fbtext'),go=$('fbgo'),msg=$('fbmsg');
let S=null,skew=0,shown=PAGE,busy=false;
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
function list(){if(!S)return;const it=S.items;
  $('fbcount').textContent=it.length?it.length+' post'+(it.length===1?'':'s'):'';
  $('fblist').innerHTML=it.length?it.slice(0,shown).map(x=>`<li class="fbitem"><div class="fbh"><b>${esc(x.name||'Anonymous')}</b>`+
      `<time class="meta" datetime="${esc(x.created)}" title="${esc(new Date(x.created).toLocaleString('en-GB'))}">${esc(ago(x.created))}</time></div>`+
      `<div class="fbt">${esc(x.text)}</div></li>`).join('')
    :'<li class="fbempty">No feedback yet — be the first.</li>';
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
    if(r.status===201){text.value='';store.set(DK,'');store.set(NK,name.value.trim());shown=PAGE;list();say('Thanks — your feedback is posted below.',true)}
    else if(r.status===429)say('Sorry — the daily limit was reached before your post went through. Your text is kept here for tomorrow.');
    else say('Couldn’t post: '+((b&&b.error)||'HTTP '+r.status)+'.')}
  catch(err){say('Couldn’t reach the server — your text is kept; try again.')}
  busy=false;chars()};
text.oninput=()=>{store.set(DK,text.value);chars();if(msg.classList.contains('ok'))say('')};
name.oninput=chars;
$('fbmore').onclick=()=>{shown+=PAGE;list()};

name.value=store.get(NK);text.value=store.get(DK);chars();
load();setInterval(()=>{quota();if(S)list()},30000);
}
