import {HEADER,ROW,RULER,clamp,mapTime,visibleClips,formatTime,waveformLevel,tileSeconds,rulerStep,TileCache} from './core.js';
const $=id=>document.getElementById(id), api=()=>window.go?.main?.App;
const canvas=$('canvas'),ctx=canvas.getContext('2d',{alpha:false}),ruler=$('ruler'),rc=ruler.getContext('2d',{alpha:false});
const state={project:null,left:0,pps:8,width:1,height:1,scroll:0,selected:null,tracks:new Set(),playhead:0,dirty:false,frameTimes:[],drawn:0,lastSeq:0,generation:0};
const colors={supported:['#24454a','#8bbfc5'],validated:['#294635','#98c4a0'],provisional:['#51442c','#ddbe7b'],unresolved:['#453844','#b79aac']};
let headerStamp='',scrollGuard=false,toastTimer,seekTimer,drag=null;
function message(text,error=false){$('status').textContent=text;if(error){$('toast').textContent=text;$('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').hidden=true,12000);}}
function log(kind,text,time=new Date().toLocaleTimeString()){const row=document.createElement('div');row.className=kind==='error'?'error':'';const stamp=document.createElement('span');stamp.className='stamp';stamp.textContent=time;row.append(stamp,document.createTextNode(text));const el=$('log'),bottom=el.scrollTop+el.clientHeight>=el.scrollHeight-30;el.append(row);while(el.children.length>200)el.firstChild.remove();if(bottom)el.scrollTop=el.scrollHeight;}
async function action(fn){try{return await fn();}catch(e){message(String(e.message||e),true);log('error',String(e.message||e));return null;}}
async function fetchBytes(url,signal){const r=await fetch(url,{signal});if(!r.ok)throw Error(await r.text());return r.arrayBuffer();}
const waves=new TileCache(24<<20,4,requestDraw,async(url,signal)=>{const b=await fetchBytes(url,signal);return {value:new Float32Array(b),bytes:b.byteLength};});
const thumbs=new TileCache(24<<20,2,requestDraw,async(url,signal)=>{const r=await fetch(url,{signal});if(!r.ok)throw Error(await r.text());const im=await createImageBitmap(await r.blob());return {value:im,bytes:im.width*im.height*4};});
function url(kind,params){return `/media/${kind}?${new URLSearchParams({g:state.generation,...params})}`;}
function requestDraw(){if(!state.dirty){state.dirty=true;requestAnimationFrame(draw);}}
function resize(){const box=$('timeline').getBoundingClientRect();state.width=Math.max(1,box.width-HEADER-12);state.height=Math.max(1,box.height-RULER);const dpr=window.devicePixelRatio||1;
  for(const[c,h]of [[canvas,state.height],[ruler,RULER]]){c.width=Math.round(state.width*dpr);c.height=Math.round(h*dpr);c.style.width=`${state.width}px`;c.style.height=`${h}px`;c.getContext('2d').setTransform(dpr,0,0,dpr,0,0);}
  pan(state.left);headerStamp='';requestDraw();}
new ResizeObserver(resize).observe($('timeline'));
function limitLeft(){return Math.max(0,(state.project?.duration||0)+2-state.width/state.pps);}
function updateScroll(){scrollGuard=true;const s=$('hscroll');s.scrollLeft=limitLeft()>0?state.left/limitLeft()*(s.scrollWidth-s.clientWidth):0;queueMicrotask(()=>scrollGuard=false);}
function pan(left){state.left=clamp(left,0,limitLeft());updateScroll();requestDraw();}
function zoom(pps,anchor=state.width/2){const t=state.left+anchor/state.pps;state.pps=clamp(pps,Math.max(.001,state.width/Math.max(1,state.project?.duration||100)/4),4096);$('zoom').value=Math.log2(state.pps);pan(t-anchor/state.pps);}
function fit(){if(!state.project)return;state.pps=Math.max(.001,(state.width-24)/Math.max(1,state.project.duration));state.left=0;$('zoom').min=Math.min(-3,Math.log2(state.pps)-1);$('zoom').value=Math.log2(state.pps);updateScroll();requestDraw();}
function setProject(p,fitView=true){if(!p)return;stopPlayback();state.project=p;state.generation=p.generation;state.selected=null;state.tracks=new Set(p.tracks.length?[0]:[]);waves.clear();thumbs.clear();headerStamp='';$('spacer').style.height=`${p.tracks.length*ROW}px`;$('empty').hidden=p.clips.length>0;$('projectName').textContent=p.name;$('projectName').title=p.path||'Live, provisional view';
  const unresolved=p.clips.filter(c=>c.state==='unresolved').length,missing=p.clips.filter(c=>c.missing).length;$('counts').textContent=`${p.clips.length} recordings · ${p.tracks.length} lanes · ${unresolved} unresolved${missing?` · ${missing} missing`:''}`;
  $('inspector').replaceChildren();const hint=document.createElement('p');hint.className='muted';hint.textContent='Select a recording to inspect precise timing and evidence.';$('inspector').append(hint);if(fitView)fit();else pan(state.left);seek(0,false);requestDraw();}
function draw(){state.dirty=false;const begin=performance.now(),p=state.project,w=state.width,h=state.height;
  ctx.fillStyle='#15191e';ctx.fillRect(0,0,w,h);rc.fillStyle='#252b32';rc.fillRect(0,0,w,RULER);rc.font='10px Consolas,monospace';rc.fillStyle='#aebdca';const step=rulerStep(state.pps);
  for(let t=Math.floor(state.left/step)*step;t<state.left+w/state.pps+step;t+=step){const x=(t-state.left)*state.pps;rc.fillText(formatTime(t).slice(step>=1?0:3,step>=1?8:12),x+5,14);rc.fillStyle='#5b6875';rc.fillRect(Math.round(x),19,1,9);rc.fillStyle='#aebdca';ctx.fillStyle='#202730';ctx.fillRect(Math.round(x),0,1,h);}
  waves.begin();thumbs.begin();state.drawn=0;
  if(p){const first=Math.floor(state.scroll/ROW),last=Math.min(p.tracks.length,Math.ceil((state.scroll+h)/ROW));
    for(let i=first;i<last;i++){const y=i*ROW-state.scroll;ctx.fillStyle=i%2?'#171d23':'#192027';ctx.fillRect(0,y,w,ROW-1);ctx.fillStyle='#303944';ctx.fillRect(0,y+ROW-1,w,1);
      for(const c of visibleClips(p,p.tracks[i],state.left,state.left+w/state.pps)){drawClip(c,y);state.drawn++;}}
    drawHeaders(first,last);
  }
  waves.end();thumbs.end();updatePlayhead();$('scale').textContent=`${(1/state.pps).toFixed(3)} s/px`;$('visibleRange').textContent=`${formatTime(state.left).slice(0,8)} — ${formatTime(state.left+w/state.pps).slice(0,8)}`;
  const ms=performance.now()-begin;state.frameTimes.push(ms);if(state.frameTimes.length>240)state.frameTimes.shift();$('performance').textContent=`${state.drawn} visible · draw ${ms.toFixed(1)} ms`;
}
function drawClip(c,row){
  const left=(c.start-state.left)*state.pps,right=(c.end-state.left)*state.pps,x=Math.max(0,left),end=Math.min(state.width,right),width=end-x,y=row+5,height=ROW-10;if(width<=0)return;
  const [fill,ink]=colors[c.state]||colors.provisional;ctx.save();ctx.beginPath();ctx.rect(x,y,width,height);ctx.clip();ctx.fillStyle=c.missing?'#342f35':fill;ctx.fillRect(x,y,width,height);
  ctx.fillStyle='#ffffff08';ctx.fillRect(x,y,width,21);ctx.fillStyle=ink;ctx.font='11px Segoe UI,sans-serif';ctx.fillText(c.name,x+6,y+14);ctx.font='9px Consolas,monospace';
  if(width>170){const label=c.missing?'MISSING SOURCE':c.state.toUpperCase();const tw=ctx.measureText(label).width;ctx.fillStyle=fill;ctx.fillRect(end-tw-12,y+2,tw+10,16);ctx.fillStyle=ink;ctx.fillText(label,end-tw-6,y+13);}
  let waveTop=y+24,waveH=height-28;
  if(c.video&&$('thumbs').checked&&width>45&&!c.missing){const frameH=34;waveTop+=frameH;waveH-=frameH;
    const sourceLo=clamp(mapTime(c.mapping,state.left+x/state.pps,true),0,c.duration),sourceHi=clamp(mapTime(c.mapping,state.left+end/state.pps,true),0,c.duration);
    const interval=2**Math.ceil(Math.log2(Math.max(.125,110/state.pps/c.rate)));
    let t=Math.floor(sourceLo/interval)*interval;let count=0;
    for(;t<sourceHi&&count<24;t+=interval,count++){const tx=(mapTime(c.mapping,t)-state.left)*state.pps;const time=clamp(t+interval/2,0,Math.max(0,c.duration-.05));const key=`${state.generation}:${c.index}:thumb:${time.toFixed(4)}`,im=thumbs.get(key,url('thumb',{clip:c.index,time:time.toFixed(4)}));
      if(im&&!im.error){ctx.globalAlpha=.85;ctx.drawImage(im,tx,y+23,60,34);ctx.globalAlpha=1;}else{ctx.fillStyle='#10182066';ctx.fillRect(tx,y+23,60,34);}}
  }
  const mid=waveTop+waveH/2;ctx.strokeStyle=ink;ctx.lineWidth=1;ctx.globalAlpha=.2;ctx.beginPath();ctx.moveTo(x,mid);ctx.lineTo(end,mid);ctx.stroke();ctx.globalAlpha=1;
  let loaded=false,failed='';
  if(c.audio&&!c.missing&&waveH>0){const sourceLo=clamp(mapTime(c.mapping,state.left+x/state.pps,true),0,c.duration),sourceHi=clamp(mapTime(c.mapping,state.left+end/state.pps,true),0,c.duration);
    const level=waveformLevel((sourceHi-sourceLo)/Math.max(1,width)),span=tileSeconds(level),bin=span/512;
    for(let tile=Math.floor(sourceLo/span);tile<=Math.floor(sourceHi/span)&&tile*span<c.duration;tile++){
      const key=`${state.generation}:${c.index}:wave:${level}:${tile}`,data=waves.get(key,url('wave',{clip:c.index,level,tile}));if(!data)continue;if(data.error){failed=data.error;continue;}loaded=true;ctx.beginPath();
      const first=clamp(Math.floor((sourceLo-tile*span)/bin),0,511),last=clamp(Math.ceil((sourceHi-tile*span)/bin),0,512);
      for(let i=first;i<last;i++){const t=tile*span+i*bin,px=(mapTime(c.mapping,t)-state.left)*state.pps;ctx.moveTo(px,mid-data[i*2+1]*waveH*.46);ctx.lineTo(px,mid-data[i*2]*waveH*.46);}
      ctx.stroke();
    }
  }
  if(c.missing||failed||(!loaded&&c.audio)){ctx.fillStyle='#b6bfc9';ctx.font='10px Consolas,monospace';ctx.fillText(c.missing?'Media offline · Locate media':failed?'Waveform unavailable · inspect activity':'Loading waveform…',x+7,mid+4);}
  if(!c.audio){ctx.fillStyle='#aab7c3';ctx.fillText('No audio stream',x+7,mid+4);}
  ctx.restore();ctx.strokeStyle=state.selected===c.index?'#e3eff6':ink;ctx.globalAlpha=state.selected===c.index?1:.55;ctx.lineWidth=state.selected===c.index?2:1;ctx.strokeRect(Math.max(-2,left)+.5,y+.5,Math.min(state.width+4,right)-Math.max(-2,left)-1,height-1);ctx.globalAlpha=1;
}
function drawHeaders(first,last){const stamp=`${first}:${last}:${state.generation}:${[...state.tracks]}`;const el=$('headers');if(headerStamp!==stamp){headerStamp=stamp;el.replaceChildren();for(let i=first;i<last;i++){const lane=document.createElement('div');lane.className='track';lane.dataset.index=i;const name=document.createElement('div');name.className='name';name.textContent=state.project.tracks[i].name;name.title=name.textContent;const btn=document.createElement('button');btn.textContent=state.tracks.has(i)?'ON':'OFF';btn.className=state.tracks.has(i)?'active':'';btn.title='Include this lane in audio audition (maximum two)';btn.onclick=()=>{if(state.tracks.has(i))state.tracks.delete(i);else if(state.tracks.size<2)state.tracks.add(i);else{message('Select at most two audition lanes. Turn one off first.',true);return;}stopPlayback();headerStamp='';requestDraw();};const count=document.createElement('small');count.textContent=`${state.project.tracks[i].clips.length} recordings`;const hint=document.createElement('div');hint.className='lanehint';hint.textContent='SOURCE AUDIO / VIDEO';lane.append(name,btn,count,hint);el.append(lane);}}
  for(const e of el.children)e.style.top=`${Number(e.dataset.index)*ROW-state.scroll}px`;
}
function updatePlayhead(){const x=(state.playhead-state.left)*state.pps;$('playhead').hidden=x<0||x>state.width||!state.project;$('playhead').style.left=`${HEADER+x}px`;$('time').textContent=formatTime(state.playhead);}
function hit(e){const rect=canvas.getBoundingClientRect(),x=e.clientX-rect.left,y=e.clientY-rect.top;const track=Math.floor((y+state.scroll)/ROW),t=state.left+x/state.pps;if(!state.project||track<0||track>=state.project.tracks.length)return null;return visibleClips(state.project,state.project.tracks[track],t,t+.000001)[0]||null;}
function inspect(c){state.selected=c.index;const el=$('inspector');el.replaceChildren();const title=document.createElement('h2');title.textContent=c.name;const badge=document.createElement('span');badge.className='badge';badge.textContent=c.status;el.append(title,badge);
  const dl=document.createElement('dl');for(const[k,v]of [['Timeline in',`${c.start.toFixed(6)} s`],['Timeline out',`${c.end.toFixed(6)} s`],['Source duration',`${c.duration.toFixed(6)} s`],['Model rate',c.rate.toPrecision(12)],['Clock difference',`${((c.rate-1)*1e6).toFixed(3)} ppm`],['Fit residual',`${(c.residual*1000).toFixed(3)} ms`],['Component',c.component],['Timing knots',c.mapping.length],['Media',c.missing?'OFFLINE':c.video?'Video + audio':'Audio'],['Source frames',c.metadata.video?.avg_frame_rate||'—'],['Audio format',c.metadata.audio?`${c.metadata.audio.sample_rate} Hz / ${c.metadata.audio.channels} ch`:'None']]){const dt=document.createElement('dt'),dd=document.createElement('dd');dt.textContent=k;dd.textContent=v;dl.append(dt,dd);}el.append(dl);
  const note=document.createElement('p');note.className='muted';note.textContent=c.state==='unresolved'?'This component has no established placement relative to the main group. It is preserved at the timeline end.':'Placement follows acoustic arrival time. Physical capture-time alignment is not independently established.';el.append(note);
  const evidence=(state.project.evidence||[]).filter(e=>e.reference===c.index||e.query===c.index);for(const[label,value]of [[`Pair evidence (${evidence.length})`,evidence],['Exact timing mapping',c.mapping],['Source metadata',c.metadata]]){const details=document.createElement('details'),summary=document.createElement('summary'),pre=document.createElement('pre');summary.textContent=label;pre.textContent=JSON.stringify(value,null,2);details.append(summary,pre);el.append(details);}const path=document.createElement('div');path.className='path';path.textContent=c.source;el.append(path);requestDraw();}

// Two-lane audition with one output clock, bounded four-second PCM blocks.
let audioCtx=null,playToken=0,playing=false,buffering=false,baseClock=0,baseTime=0,nextTime=0,scheduledEnd=0,pumping=false,playController=null,sources=new Set();
function stopPlayback(){if(playing&&audioCtx)state.playhead=clamp(baseTime+audioCtx.currentTime-baseClock,0,state.project?.duration||0);playToken++;playing=false;buffering=false;pumping=false;playController?.abort();for(const s of sources){try{s.stop();}catch{}}sources.clear();$('play').textContent='▶';$('playState').textContent='STOPPED';updatePlayhead();}
async function startPlayback(){if(!state.project||!state.tracks.size){message('Enable one or two audition lanes using their ON / OFF buttons.',true);return;}if(state.playhead>=state.project.duration)state.playhead=0;stopPlayback();const token=++playToken;playController=new AbortController();buffering=true;$('playState').textContent='BUFFERING';audioCtx??=new AudioContext({sampleRate:48000});await audioCtx.resume();baseTime=state.playhead;nextTime=baseTime;scheduledEnd=0;
  try{await pumpAudio(token,true);if(token!==playToken)return;playing=true;buffering=false;$('play').textContent='Ⅱ';$('playState').textContent='PLAYING';requestAnimationFrame(playTick);}catch(e){if(token===playToken){stopPlayback();message(e.message||String(e),true);}}
}
async function pumpAudio(token,first=false){if(pumping||token!==playToken)return;pumping=true;try{while(token===playToken&&nextTime<state.project.duration&&(first||scheduledEnd-audioCtx.currentTime<5)){
    const duration=Math.min(4,state.project.duration-nextTime),at=nextTime;const bytes=await fetchBytes(url('audio',{start:at.toFixed(9),duration:duration.toFixed(9),tracks:JSON.stringify([...state.tracks])}),playController.signal);if(token!==playToken)return;
    const data=new Float32Array(bytes),buffer=audioCtx.createBuffer(2,data.length/2,48000);for(let ch=0;ch<2;ch++){const dest=buffer.getChannelData(ch);for(let i=0;i<dest.length;i++)dest[i]=data[i*2+ch];}
    if(first){baseClock=audioCtx.currentTime+.08;first=false;}else if(baseClock+(at-baseTime)<audioCtx.currentTime+.005)throw Error('Audio decode could not keep up. Audition stopped to preserve synchronization; retry closer to the region of interest.');
    const source=audioCtx.createBufferSource();source.buffer=buffer;source.connect(audioCtx.destination);sources.add(source);source.onended=()=>sources.delete(source);const when=baseClock+(at-baseTime);source.start(when);scheduledEnd=when+buffer.duration;nextTime+=duration;
    // Schedule the first block immediately; continue filling asynchronously.
    if(buffering)return;
  }}finally{pumping=false;}}
function playTick(){if(!playing)return;state.playhead=clamp(baseTime+audioCtx.currentTime-baseClock,0,state.project.duration);if(state.playhead>=state.project.duration){stopPlayback();return;}updatePlayhead();if($('follow').checked&&(state.playhead<state.left||state.playhead>state.left+state.width/state.pps*.85))pan(state.playhead-state.width/state.pps*.2);
  pumpAudio(playToken).catch(e=>{if(e.name!=='AbortError'){stopPlayback();message(e.message||String(e),true);}});requestAnimationFrame(playTick);}
function seek(t,restart=true){const resume=restart&&(playing||buffering);stopPlayback();state.playhead=clamp(t,0,state.project?.duration||0);updatePlayhead();clearTimeout(seekTimer);if(resume)seekTimer=setTimeout(()=>action(startPlayback),50);}

canvas.addEventListener('pointerdown',e=>{canvas.focus();if(e.button===1||e.altKey){drag={x:e.clientX,y:e.clientY,left:state.left,scroll:state.scroll};canvas.setPointerCapture(e.pointerId);e.preventDefault();return;}if(e.button!==0)return;const c=hit(e);if(c)inspect(c);const x=e.clientX-canvas.getBoundingClientRect().left;seek(state.left+x/state.pps);});
canvas.addEventListener('pointermove',e=>{if(drag){pan(drag.left-(e.clientX-drag.x)/state.pps);$('scroller').scrollTop=drag.scroll-(e.clientY-drag.y);return;}const c=hit(e),tip=$('tooltip');if(!c){tip.hidden=true;return;}tip.textContent=`${c.name}\n${c.status}\nIn ${formatTime(c.start)}  •  ${c.duration.toFixed(3)} s\nResidual ${(c.residual*1000).toFixed(3)} ms${c.missing?'\nSOURCE MISSING':''}`;tip.hidden=false;tip.style.left=`${Math.min(e.clientX+15,innerWidth-430)}px`;tip.style.top=`${Math.min(e.clientY+17,innerHeight-130)}px`;});
canvas.addEventListener('pointerup',()=>drag=null);canvas.addEventListener('pointercancel',()=>drag=null);canvas.addEventListener('pointerleave',()=>$('tooltip').hidden=true);
canvas.addEventListener('dblclick',e=>{const c=hit(e);if(c){state.pps=clamp((state.width-70)/(c.end-c.start),.001,4096);pan(c.start-30/state.pps);}});
ruler.addEventListener('pointerdown',e=>seek(state.left+(e.clientX-ruler.getBoundingClientRect().left)/state.pps));
$('timeline').addEventListener('wheel',e=>{e.preventDefault();if(e.ctrlKey||e.metaKey){zoom(state.pps*Math.exp(-e.deltaY*.002),clamp(e.clientX-canvas.getBoundingClientRect().left,0,state.width));}else if(e.shiftKey||Math.abs(e.deltaX)>Math.abs(e.deltaY)){pan(state.left+(e.deltaX||e.deltaY)/state.pps);}else{$('scroller').scrollTop+=e.deltaY;}},{passive:false});
$('scroller').addEventListener('scroll',()=>{state.scroll=$('scroller').scrollTop;requestDraw();});$('hscroll').addEventListener('scroll',()=>{if(scrollGuard)return;const s=$('hscroll');state.left=s.scrollLeft/Math.max(1,s.scrollWidth-s.clientWidth)*limitLeft();requestDraw();});
$('open').onclick=()=>action(async()=>setProject(await api().ChooseProject()));$('locate').onclick=()=>action(async()=>setProject(await api().LocateMedia(),false));$('add').onclick=()=>action(()=>api().SelectInputs(false));$('folder').onclick=()=>action(()=>api().SelectInputs(true));$('clear').onclick=()=>action(()=>api().ClearInputs());$('sync').onclick=()=>action(()=>api().StartSync());$('cancel').onclick=()=>action(()=>api().CancelSync());
$('play').onclick=()=>playing||buffering?stopPlayback():action(startPlayback);$('stop').onclick=stopPlayback;$('fit').onclick=fit;$('zoomIn').onclick=()=>zoom(state.pps*1.5);$('zoomOut').onclick=()=>zoom(state.pps/1.5);$('zoom').oninput=e=>zoom(2**Number(e.target.value));$('thumbs').onchange=requestDraw;
document.addEventListener('keydown',e=>{if(e.target.matches('input,select,textarea'))return;const key=e.key.toLowerCase();if((e.ctrlKey||e.metaKey)&&key==='o'){e.preventDefault();$('open').click();return;}if([' ','arrowleft','arrowright','home','end','+','-','=','f','escape'].includes(key))e.preventDefault();
  if(key===' ')$('play').click();else if(key==='escape'){stopPlayback();$('toast').hidden=true;}else if(key==='f')fit();else if(key==='+'||key==='=')zoom(state.pps*1.5);else if(key==='-')zoom(state.pps/1.5);else if(key==='arrowleft'||key==='arrowright')seek(state.playhead+(key==='arrowleft'?-1:1)*(e.shiftKey?1:1/Number($('fps').value)));else if(key==='home')seek(0);else if(key==='end')seek(state.project?.duration||0);
});
let polling=false;
async function poll(){if(polling||!api())return;polling=true;try{const s=await api().Status(state.lastSeq);for(const e of s.events){state.lastSeq=Math.max(state.lastSeq,e.seq);log(e.kind,e.message,e.time);}$('engineStage').textContent=s.stage;$('activityDetail').textContent=s.detail;$('build').textContent=`GO ${s.version}`;$('sync').textContent=s.running?'Synchronizing…':s.inputs?`Synchronize (${s.inputs})`:'Synchronize';$('sync').disabled=s.running||!s.inputs;$('cancel').disabled=!s.running;for(const id of ['open','add','folder','clear','locate'])$(id).disabled=s.running;
  const mem=s.visualization;$('memory').textContent=`Visual RAM ${Math.round((mem.cacheBytes+waves.bytes+thumbs.bytes)/1048576)} MB · ${mem.active} media workers · disk OFF`;
  if(s.generation&&s.generation!==state.generation){const p=await api().GetProject();setProject(p,!state.project||!s.running);}if(waves.lastError){log('error',waves.lastError);waves.lastError=null;}if(thumbs.lastError){log('error',thumbs.lastError);thumbs.lastError=null;}
}catch(e){message(String(e.message||e),true);}finally{polling=false;}}
setInterval(poll,750);poll();resize();
// Explicit test surface, independent of native UI automation. Used only by the
// development harness to inject fixtures and measure actual Canvas rendering.
export const testAPI={setProject,state,pan,zoom,draw,fit,seek,startPlayback,stopPlayback,waves,thumbs,visibleClips};
if(new URLSearchParams(location.search).has('test'))window.opensyncTest=testAPI;
