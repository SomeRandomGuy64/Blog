// Reference copy of the solver from before rankings (no preference penalties), kept only so test_rank.js can check that the
// current solver gives byte-identical results with no ranking, timed-out searches included. Not served by the site.
/* Fazon Nexus · Cinema Planner — schedule optimiser. SOLVER is stringified (SOLVER.toString()) into a Blob Web Worker by planner.js, so it must stay a self-contained function declaration with no outer references. */
// Self-contained schedule optimiser (also runs as an inline Blob worker; keep it free of outer references).
// Input P: {films:[{days:[[{i,c,st,en}]...per day]}] in priority order (0 = most wanted),
//   win:[{e,l}|null per day] (minutes from that day's midnight; l may exceed 1440), T:Int32Array (nc+1)^2 minutes
//   (-1 = unreachable; index nc = home; should be shortest-path closed so feasibility is monotone -> exact search), nc, buf, late, maxDay (0 = none), mode:'most'|'strict', budget ms,
//   ds?, de?: [Int32Array(nc)|null per day], minutes from that day's own start / to its own end at each cinema (null or absent = home's row / column of T).
//   They need the same closure: ds[d][c] <= ds[d][k] + T[k][c] and de[d][c] <= T[c][k] + de[d][k] for the cinemas k, c with showings on day d}
//   Film sets are plain JS numbers used as bit sets (film f = 2**f), exact up to 2**53, so up to 47 films are supported
//   (the planner sends at most 40); JS bitwise operators are 32-bit, so film sets only go through the helpers below.
// Output: {status:'optimal'|'films' (film choice proven, travel not)|'timeout', days:[{d,seq:[showIdx],cost}], chosen:[f], unfit:{f:{higher}}, nodes, ms, memo}
function SOLVER(){
"use strict";
const pc32=x=>{x-=(x>>>1)&0x55555555;x=(x&0x33333333)+((x>>>2)&0x33333333);return (((x+(x>>>4))&0x0F0F0F0F)*0x01010101)>>>24};
const low32=b=>31-Math.clz32(b&-b);
// film-set helpers: a set S is split into 31-bit words lo = S % 2**31 and hi = floor(S / 2**31) (fast path: S < 2**31)
const W31=2147483648,BIT=Array.from({length:53},(_,f)=>2**f);
const pc=S=>{if(S<W31)return pc32(S);const h=Math.floor(S/W31);return pc32(S-h*W31)+pc32(h)};
const low=S=>{if(S<W31)return low32(S);const l=S%W31;return l?low32(l):31+low32(Math.floor(S/W31))};   // lowest film in S (S > 0)
const has=(S,f)=>f<31?((S<W31?S:S%W31)>>f&1)===1:(Math.floor(S/W31)>>(f-31)&1)===1;
const add=(S,f)=>has(S,f)?S:S+BIT[f], del=(S,f)=>has(S,f)?S-BIT[f]:S;
const or=(a,b)=>{const ah=Math.floor(a/W31),bh=Math.floor(b/W31);return ((ah|bh)>>>0)*W31+(((a-ah*W31)|(b-bh*W31))>>>0)};
const EMPTY={groups:[],home:Infinity,hs:null,n:0};
function solve(P){
  if(P.films.length>47)throw new Error('The solver handles at most 47 films');
  const t0=Date.now(),budget=P.budget||2500,n=P.films.length,ND=P.win.length,NC1=P.nc+1,H=P.nc,T=P.T;
  const buf=P.buf||0,late=P.late||0,strict=P.mode==='strict',W=P.win,maxDay=P.maxDay>0?P.maxDay:99;
  const tr=(a,b)=>a===b?0:T[a*NC1+b];
  const hTo=Array.from({length:P.nc},(_,c)=>T[H*NC1+c]),hFr=Array.from({length:P.nc},(_,c)=>T[c*NC1+H]);
  const DS=Array.from({length:ND},(_,d)=>(P.ds&&P.ds[d])||hTo),DE=Array.from({length:ND},(_,d)=>(P.de&&P.de[d])||hFr);   // each day's first and last legs by cinema
  const cand=P.films.map(f=>Array.from({length:ND},(_,d)=>((f.days&&f.days[d])||[]).slice().sort((a,b)=>a.st-b.st)));
  // ---- per-day capacity: longest chain of any candidate showings (upper bound on films/day) ----
  const cap=new Int32Array(ND);
  for(let d=0;d<ND;d++){const w=W[d];if(!w)continue;
    const all=[];for(let f=0;f<n;f++)for(const s of cand[f][d])all.push(s);
    all.sort((a,b)=>a.st-b.st);
    const K=Math.min(maxDay,n,16),minEnd=new Map();let best=0;
    for(const s of all){const t=DS[d][s.c];let L=t<0||w.e+t>s.st+late?0:1;
      for(const [c,arr] of minEnd){const tt=tr(c,s.c);if(tt<0)continue;const lim=s.st+late-buf-tt;
        for(let k=K-1;k>=L;k--)if(arr[k]<=lim){L=k+1;break}}
      if(!L)continue;if(L>K)L=K;
      let arr=minEnd.get(s.c);if(!arr){arr=new Float64Array(K+1).fill(Infinity);minEnd.set(s.c,arr)}
      for(let k=1;k<=L;k++)if(s.en<arr[k])arr[k]=s.en;
      const th=DE[d][s.c];if(th>=0&&s.en+buf+th<=w.l&&L>best)best=L}
    cap[d]=Math.min(best,maxDay)}
  // ---- per-day DP over film subsets: states = showing that can END a feasible sequence covering exactly S ----
  const memo=Array.from({length:ND},()=>new Map());let memoN=0;
  function last(d,S){let r=memo[d].get(S);if(r===undefined){r=build(d,S);memo[d].set(S,r);memoN++}return r}
  function build(d,S){
    const w=W[d],cnt=pc(S);if(!w||cnt>cap[d])return EMPTY;
    const states=[];   // {s, cost: min travel+wait to finish s, mt: min travel alone (monotone lower bound), prev}
    if(cnt===1){const f=low(S);
      for(const s of cand[f][d]){const t=DS[d][s.c];if(t<0||w.e+t>s.st+late)continue;states.push({s,cost:t,mt:t,prev:null})}}
    else for(let b=S;b;){const f=low(b);b-=BIT[f];const Pr=last(d,S-BIT[f]);if(!Pr.n)continue;const G=Pr.groups;
      for(const s of cand[f][d]){let best=Infinity,bp=null,bmt=Infinity;
        for(const g of G){const t=tr(g.c,s.c);if(t<0)continue;
          const T0=s.st-buf-t,T1=T0+late,E=g.end;   // end<=T0: on time (cost += wait); T0<end<=T1: within the trailer allowance
          let lo=0,hi=E.length;while(lo<hi){const m=(lo+hi)>>1;if(E[m]<=T0)lo=m+1;else hi=m}
          if(lo>0){const x=g.st[g.pmi[lo-1]],v=x.cost-x.s.en+s.st-buf;if(v<best){best=v;bp=x}if(g.pmt[lo-1]+t<bmt)bmt=g.pmt[lo-1]+t}
          for(let k=lo;k<E.length&&E[k]<=T1;k++){const x=g.st[k],v=x.cost+t;if(v<best){best=v;bp=x}if(x.mt+t<bmt)bmt=x.mt+t}}
        if(bp)states.push({s,cost:best,mt:bmt,prev:bp})}}
    if(!states.length)return EMPTY;
    const by=new Map();for(const x of states){const a=by.get(x.s.c);if(a)a.push(x);else by.set(x.s.c,[x])}
    const groups=[];let home=Infinity,hs=null,mth=Infinity;
    for(const [c,arr] of by){arr.sort((a,b)=>a.s.en-b.s.en);
      const L=arr.length,end=new Float64Array(L),pmi=new Int32Array(L),pmt=new Float64Array(L);let bi=0,bv=Infinity,mv=Infinity;
      for(let k=0;k<L;k++){const x=arr[k];end[k]=x.s.en;const v=x.cost-x.s.en;if(v<bv){bv=v;bi=k}pmi[k]=bi;if(x.mt<mv)mv=x.mt;pmt[k]=mv}
      groups.push({c,end,pmi,pmt,st:arr});
      const th=DE[d][c];if(th>=0)for(const x of arr)if(x.s.en+buf+th<=w.l){if(x.cost+th<home){home=x.cost+th;hs=x}if(x.mt+th<mth)mth=x.mt+th}}
    return {groups,home,hs,mth,n:states.length}}
  const fits=(d,S)=>last(d,S).home<Infinity;
  const daysOf=cand.map(c=>{const r=[];for(let d=0;d<ND;d++)if(W[d]&&c[d].length)r.push(d);return r});
  const Sd=new Float64Array(ND),dc=new Float64Array(ND),dm=new Float64Array(ND);
  const MB=f=>2**(n-1-f), TOP=2**n;   // priority value of film f; mv < 2**n, so cnt*TOP+mv <= (n+1)*2**n < 2**53
  const keyOf=(cnt,mv)=>strict?mv:cnt*TOP+mv;
  let best={key:-1,cost:Infinity,Sd:new Float64Array(ND)},nodes=0,timedOut=false,deadline=t0+budget;
  const tick=()=>{if((++nodes&31)===0&&Date.now()>deadline)timedOut=true;return timedOut};
  const cntD=new Int32Array(ND);
  function options(i){const o=[],bit=BIT[i];   // film i is undecided, so it is in no Sd[d]: S+bit adds it
    for(const d of daysOf[i]){if(pc(Sd[d])>=cap[d])continue;const R=last(d,Sd[d]+bit);if(R.home<Infinity)o.push([d,R.home-dc[d],R])}
    return o.sort((a,b)=>a[1]-b[1])}
  function upper(i,cnt,mv){let fit=0,fb=0;cntD.fill(0);
    for(let j=i;j<n;j++){let any=false;const bit=BIT[j];
      for(const d of daysOf[j]){if(pc(Sd[d])>=cap[d])continue;if(fits(d,Sd[d]+bit)){any=true;cntD[d]++}}
      if(any){fit++;fb+=MB(j)}}
    let room=0;for(let d=0;d<ND;d++)room+=Math.min(Math.max(0,cap[d]-pc(Sd[d])),cntD[d]);
    return keyOf(cnt+Math.min(fit,room),mv+fb)}   // fb has no overlap with mv (films >= i are undecided)
  function leaf(cnt,mv,cost){const k=keyOf(cnt,mv);
    if(k>best.key||(k===best.key&&cost<best.cost-1e-9))best={key:k,cost,Sd:Float64Array.from(Sd)}}
  // ---- phase 1: branch & bound on the film score (count/priority), films in priority order: a day for film i, or skip ----
  function dfs(i,cnt,mv,cost){
    if(tick())return;
    if(i===n){leaf(cnt,mv,cost);return}
    if(upper(i,cnt,mv)<=best.key)return;
    const o=options(i),bit=BIT[i];
    for(const [d,delta,R] of o){const s0=Sd[d],c0=dc[d];Sd[d]=s0+bit;dc[d]=R.home;
      dfs(i+1,cnt+1,mv+MB(i),cost+delta);Sd[d]=s0;dc[d]=c0;if(timedOut)return}
    if(strict&&o.length)return;   // strict: including a fittable higher pick always dominates skipping it
    dfs(i+1,cnt,mv,cost)}
  // Same search with dynamic ordering: branch on the undecided film that fits on the fewest days (most constrained
  // first); a film that fits nowhere is dropped. Finds full schedules far sooner when a low pick has few showings.
  function scan(U,cnt,mv){   // -> upper bound on the score, plus the most constrained undecided film and its options
    let fit=0,fb=0,bf=-1,bo=null;cntD.fill(0);
    for(let b=U;b;){const j=low(b),bit=BIT[j],o=[];b-=bit;
      for(const d of daysOf[j]){if(pc(Sd[d])>=cap[d])continue;const R=last(d,Sd[d]+bit);if(R.home<Infinity){o.push([d,R.home-dc[d],R]);cntD[d]++}}
      if(!o.length){U-=bit;continue}
      fit++;fb+=MB(j);if(!bo||o.length<bo.length)bf=j,bo=o}
    let room=0;for(let d=0;d<ND;d++)room+=Math.min(Math.max(0,cap[d]-pc(Sd[d])),cntD[d]);
    return {ub:keyOf(cnt+Math.min(fit,room),mv+fb),U,bf,bo}}
  function dfsDyn(U0,cnt,mv,cost){
    if(tick())return;
    const {ub,U,bf,bo}=scan(U0,cnt,mv);
    if(bf<0){leaf(cnt,mv,cost);return}
    if(ub<=best.key)return;
    const bit=BIT[bf],U2=U-bit;bo.sort((a,b)=>a[1]-b[1]);
    for(const [d,delta,R] of bo){const s0=Sd[d],c0=dc[d];Sd[d]=s0+bit;dc[d]=R.home;
      dfsDyn(U2,cnt+1,mv+MB(bf),cost+delta);Sd[d]=s0;dc[d]=c0;if(timedOut)return}
    dfsDyn(U2,cnt,mv,cost)}
  // greedy incumbent first (guarantees an answer even if the budget is tiny)
  (function greedy(){let cnt=0,mv=0,cost=0;
    for(let i=0;i<n;i++){const o=options(i);if(o.length){const [d,delta,R]=o[0];Sd[d]+=BIT[i];dc[d]=R.home;cnt++;mv+=MB(i);cost+=delta}}
    leaf(cnt,mv,cost);Sd.fill(0);dc.fill(0)})();
  // ---- local search on the incumbent: (1) insert each missing film, bumping films to other days along an ejection
  //      chain (depth 3); else replace a lower-ranked pick; (2) move/swap films between days while travel+wait drops ----
  function improve(until){const B=Float64Array.from(best.Sd),C=new Float64Array(ND);
    const home=(d,S)=>S?last(d,S).home:0;for(let d=0;d<ND;d++)C[d]=home(d,B[d]);
    const dayOf=f=>{for(let d=0;d<ND;d++)if(has(B[d],f))return d;return -1};
    const set=(d,S)=>{B[d]=S;C[d]=home(d,S)};
    const ok=(d,S)=>!S||(pc(S)<=cap[d]&&fits(d,S));
    let steps=0;
    function place(f,depth,used){   // used: bit set over days (<= 10), so 32-bit ops are fine there
      for(const d of daysOf[f])if(!(used>>d&1)&&ok(d,add(B[d],f))){set(d,add(B[d],f));return true}
      if(!depth||(++steps&63)===0&&Date.now()>until)return false;
      for(const d of daysOf[f]){if(used>>d&1)continue;const S0=B[d];
        for(let b=S0;b;){const g=low(b);b-=BIT[g];const S2=add(del(S0,g),f);if(!ok(d,S2))continue;
          set(d,S2);if(place(g,depth-1,used|1<<d))return true;set(d,S0)}}
      return false}
    for(let again=true,guard=0;again&&guard++<40&&Date.now()<until;){again=false;
      for(let f=0;f<n&&Date.now()<until;f++){if(dayOf(f)>=0)continue;
        if(place(f,3,0)){again=true;continue}
        let rep=false;   // replace the lowest-ranked pick that makes room (same count, better priority)
        for(let g=n-1;g>f&&!rep;g--){const d=dayOf(g);if(d<0||!daysOf[f].includes(d))continue;const S2=add(del(B[d],g),f);
          if(ok(d,S2)){set(d,S2);rep=true}}
        if(rep)again=true}
      for(let d=0;d<ND;d++)for(let b=B[d];b;){const g=low(b);b-=BIT[g];
        for(const d2 of daysOf[g]){if(d2===d||!has(B[d],g))continue;
          const a1=del(B[d],g),a2=add(B[d2],g);if(!ok(d2,a2))continue;
          if(home(d,a1)+home(d2,a2)<C[d]+C[d2]-1e-9){set(d,a1);set(d2,a2);again=true;continue}
          for(let b2=B[d2];b2;){const h=low(b2);b2-=BIT[h];if(!daysOf[h].includes(d))continue;
            const x1=add(del(B[d],g),h),x2=add(del(B[d2],h),g);if(!ok(d,x1)||!ok(d2,x2))continue;
            if(home(d,x1)+home(d2,x2)<C[d]+C[d2]-1e-9){set(d,x1);set(d2,x2);again=true;break}}}}}
    let cnt=0,mv=0,cost=0;for(let d=0;d<ND;d++){cost+=C[d];for(let b=B[d];b;){const g=low(b);b-=BIT[g];cnt++;mv+=MB(g)}}
    const k=keyOf(cnt,mv);if(k>best.key||(k===best.key&&cost<best.cost-1e-9))best={key:k,cost,Sd:B}}
  const ALL=2**n-1;Sd.fill(0);dc.fill(0);const rootUB=scan(ALL,0,0).ub;
  improve(t0+budget*0.2);
  deadline=t0+budget*0.7;
  if(P.order==='priority')dfs(0,0,0,0);else if(best.key<rootUB)dfsDyn(ALL,0,0,0);
  let p1=!timedOut,p2=false;timedOut=false;
  improve(t0+budget*0.85);
  if(best.key>=rootUB)p1=true;   // incumbent reaches the root bound (e.g. every individually-fittable film is in): proven
  // ---- phase 2: same films, minimise travel+waiting; exact prune via sum of per-day minimum travel (monotone) ----
  if(best.key>0){
    let target=0;for(const S of best.Sd)target=or(target,S);
    deadline=p1?Math.max(t0+budget,Date.now()+300):t0+budget;
    // lb = sum of per-day minimum travel (monotone as films are added; travel+wait >= travel), plus the largest
    // minimum increase some still-unplaced film must cause. Branch on the film with the fewest feasible days.
    const dfs2=(U,lb,cost)=>{
      if(tick())return;
      if(!U){if(cost<best.cost-1e-9)best={key:best.key,cost,Sd:Float64Array.from(Sd)};return}
      let bf=-1,bo=null,maxInc=0;
      for(let b=U;b;){const j=low(b),bit=BIT[j],o=[];let mi=Infinity;b-=bit;
        for(const d of daysOf[j]){if(pc(Sd[d])>=cap[d])continue;const R=last(d,Sd[d]+bit);if(R.home===Infinity)continue;
          const inc=R.mth-dm[d];if(inc<mi)mi=inc;o.push([d,R.home-dc[d],R,lb+inc])}
        if(!o.length)return;
        if(mi>maxInc)maxInc=mi;if(!bo||o.length<bo.length){bf=j;bo=o}}
      if(lb+maxInc>=best.cost-1e-9)return;
      const bit=BIT[bf];bo.sort((a,b)=>a[1]-b[1]);
      for(const [d,delta,R,nlb] of bo){if(nlb>=best.cost-1e-9)continue;const s0=Sd[d],c0=dc[d],m0=dm[d];Sd[d]=s0+bit;dc[d]=R.home;dm[d]=R.mth;
        dfs2(U-bit,nlb,cost+delta);Sd[d]=s0;dc[d]=c0;dm[d]=m0;if(timedOut)return}};
    Sd.fill(0);dc.fill(0);dm.fill(0);dfs2(target,0,0);p2=!timedOut}
  // ---- reconstruct ----
  const out={status:!p1?'timeout':p2||best.key<=0?'optimal':'films',days:[],chosen:[],unfit:{},nodes,ms:Date.now()-t0,memo:memoN,cap:Array.from(cap)};
  const B=best.Sd;let chosenMask=0;
  for(let d=0;d<ND;d++){const S=B[d];if(!S)continue;chosenMask=or(chosenMask,S);const R=last(d,S),seq=[];
    for(let x=R.hs;x;x=x.prev)seq.push(x.s.i);
    out.days.push({d,seq:seq.reverse(),cost:R.home})}
  for(let f=0;f<n;f++){if(has(chosenMask,f)){out.chosen.push(f);continue}
    let higher=false;   // B[d] % 2**f = the picks on day d ranked above f
    for(const d of daysOf[f])if(fits(d,B[d]%BIT[f]+BIT[f])){higher=true;break}
    out.unfit[f]={higher}}   // higher=true: fits next to the higher-ranked picks (so it lost to lower-ranked ones on count)
  out.ms=Date.now()-t0;
  return out}
return {solve}}
