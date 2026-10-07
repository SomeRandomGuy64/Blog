/* Fazon Nexus · Cinema Planner — screen-type classification shared by the listings and the planner. */
// One "screen type" per showing = its premium/screen tags combined in a canonical order ("4DX 3D", "IMAX 1.43:1 70mm");
// no screen tags = "Standard". Other tags (Subtitled, Silver Screen, languages…) stay separate.
function SCREENTYPES(P){
  const ORD=['IMAX 1.43:1','IMAX','4DX','ScreenX','Dolby Cinema','iSense','XPlus','EPIC','Superscreen','Infinity Vision',
    'D-BOX','Luxe','Ultra Lux','70mm','35mm','4K','Dolby Atmos'];
  const rk=t=>t==='3D'?1e3:ORD.includes(t)?ORD.indexOf(t):500;   // unknown screen tags: before 3D, alphabetical
  const isScreen=t=>t==='3D'||P.has(t)||ORD.includes(t);
  const tcmp=(a,b)=>rk(a)-rk(b)||a.localeCompare(b);
  const memo=new Map(),RK=new Map([['Standard',[-1]]]);
  function screenType(x){const k=x.join('\u0001');let v=memo.get(k);if(v!==undefined)return v;
    let s=[...new Set(x.filter(isScreen))].sort(tcmp);
    if(s.includes('IMAX 1.43:1'))s=s.filter(t=>t!=='IMAX');      // "IMAX 1.43:1" already says IMAX
    v=s.length?s.join(' '):'Standard';memo.set(k,v);if(!RK.has(v))RK.set(v,s.map(rk));return v}
  const otherTags=x=>x.filter(t=>!isScreen(t)).sort((a,b)=>a.localeCompare(b));
  function stCmp(a,b){const A=RK.get(a),B=RK.get(b);if(!A||!B)return (!A)-(!B)||a.localeCompare(b);
    for(let i=0;i<Math.min(A.length,B.length);i++)if(A[i]!==B[i])return A[i]-B[i];return A.length-B.length||a.localeCompare(b)}
  // does a planner pick allow this showing? p:{st,req,exc,dd}; cs: Set of allowed cinema indices or null; day: "YYYY-MM-DD"
  // (p.dd = dates the pick is excluded on, null = any day)
  function pickAllows(p,x,c,day,cs){if(cs&&!cs.has(c))return false;
    if(p.st&&p.st.length&&!p.st.includes(screenType(x)))return false;
    if(p.req&&p.req.length&&!x.some(v=>p.req.includes(v)))return false;
    if(p.exc&&p.exc.length&&x.some(v=>p.exc.includes(v)))return false;
    return !(p.dd&&p.dd.includes(day))}
  return {screenType,otherTags,isScreen,stCmp,pickAllows}}
