/* Fazon Nexus · Cinema Planner — date helpers shared by the Listings and Schedule tabs (pure, so they can be unit tested).
   Dates are "YYYY-MM-DD" strings, which sort as dates. */
function CINEMA_DATES(){
const add=(day,n)=>new Date(Date.parse(day+'T00:00:00Z')+n*864e5).toISOString().slice(0,10);
// dates with at least one showing, from..to; the plan's start-date choices (never empty while the window is open: an empty listing still offers its first day)
function startDays(shows,from,to){const s=new Set();for(const x of shows){const d=x.t.slice(0,10);if(d>=from&&d<=to)s.add(d)}
  return s.size?[...s].sort():from<=to?[from]:[]}
// a saved start stays when it's still offered, else moves to the next date offered, else the first
const nearest=(opts,day)=>opts.includes(day)?day:opts.find(d=>d>day)||opts[0];
// [["November 2026",[dates…]],…] for optgroups; months in date order
function months(days){const g=new Map();for(const d of days){const m=new Date(d+'T12:00:00Z').toLocaleDateString('en-GB',{month:'long',year:'numeric',timeZone:'UTC'});
  (g.get(m)||g.set(m,[]).get(m)).push(d)}return [...g.entries()]}
// chains (in cinema order) whose listings stop before day: the plan can't use them on that date
function stopsBefore(D,day){const last=new Map();for(const c of D.cinemas)last.set(c.chain,last.get(c.chain)||'');
  for(const s of D.shows){const c=D.cinemas[s.c].chain,d=s.t.slice(0,10);if(d>last.get(c))last.set(c,d)}
  return [...last].filter(([,d])=>d<day).map(([c])=>c)}
// the note under the plan dates; last = the plan's last day, today = the visitor's date
function farNote(D,last,today){if(last<=add(today,6))return '';
  // a plan can run past every listing: only name the chains that stop before the others do
  const top=D.shows.reduce((m,x)=>x.t>m?x.t:m,'').slice(0,10),gone=stopsBefore(D,last<top?last:top),t=[gone.length?`Nothing is listed this far ahead for ${gone.join(', ')}, so the plan can’t use them on the later days.`:'Cinemas publish less far ahead, so later days may have fewer listings yet.'];
  if(last>add(today,27))t.push('Past about four weeks, cinemas have usually listed only the biggest releases, so more may appear closer to the date.');
  return t.join(' ')}
return {add,startDays,nearest,months,stopsBefore,farNote};
}
