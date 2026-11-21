import assert from "node:assert/strict";
import {pathToFileURL} from "node:url";
const {chromium}=await import(pathToFileURL(process.env.OPENSYNC_PLAYWRIGHT).href);
const base=process.env.OPENSYNC_PREVIEW_URL;
const project=await (await fetch(base+"/test/project")).json();
const browser=await chromium.launch({executablePath:process.env.OPENSYNC_BROWSER});
try {
 for(const width of [1000, 1500, 1920]){
  const page=await browser.newPage({viewport:{width,height:850},deviceScaleFactor:1.5});
  const errors=[];
  page.on("pageerror",e=>errors.push(e.message));
  await page.goto(base+"/?test=1");
  await page.waitForFunction(()=>window.opensyncTest);
  await page.evaluate(async p=>{
   const t=window.opensyncTest;
   t.workstation.monitor.enabled=false;
   t.setProject({...p,path:"",generation:p.generation-1});
   await new Promise(requestAnimationFrame);
   t.setProject(p);
   t.fit();
  },project);
  await page.waitForFunction(()=>window.opensyncTest.state.drawn>0);
  const result=await page.evaluate(()=>{
   const t=window.opensyncTest;
   const canvas=document.querySelector("#canvas");
   const visible=id=>document.querySelector(id).getBoundingClientRect().right<=innerWidth;
   return {width:innerWidth,body:document.body.scrollWidth,canvas:canvas.width,drawn:t.state.drawn,
    settings:visible("#settingsButton"),export:visible("#exportXML"),inspector:visible("#inspectorPanel"),
    pixel:[...canvas.getContext("2d").getImageData(2,2,1,1).data]};
  });
  console.log(result);
  assert.ok(result.body<=width && result.settings && result.export && result.inspector);
  assert.ok(result.canvas<=Math.ceil(width*1.5) && result.pixel[3]>0);
  const progress = await page.evaluate(() => {
   const w = window.opensyncTest.workstation;
   const state = { running: true, stage: "Streaming audio and extracting fingerprints" };
   const timeline = () => document.querySelector("#timeline").getBoundingClientRect().toJSON();
   const before = timeline();
   w.status({ ...state, ioProgress: { remaining: 5, total: 6, eta: 43, files: [
     { name: "Office_ch5_20260820151043_20260820172806.mp4", fraction: .37, eta: 18 },
     { name: "camera.mp4", fraction: .09, eta: 11 },
   ] } });
   const text = document.querySelector("#ioFiles").textContent;
   const whileSyncing = w.monitor.enabled;
   const after = timeline();
   w.status({ running: false });
   return { before, after, text, whileSyncing, idleText: document.querySelector("#ioFiles").textContent, enabled: w.monitor.enabled };
  });
  assert.deepEqual(progress.before, progress.after);
  assert.match(progress.text, /Files left 5\/6.*~43s.*Office_ch5.*37%.*~18s.*camera.mp4.*~11s/);
  assert.equal(progress.whileSyncing,false);
  assert.equal(progress.enabled,true);
  assert.equal(progress.idleText,"");
  const match = project.clips.find(c => c.name.startsWith("Office_ch5"));
  assert.ok(match);
  await page.keyboard.press("Control+f");
  await page.locator("#findClipInput").fill("OFFICE_ch5");
  assert.equal(await page.locator("#findClipResults option").count(), 1);
  await page.locator("#findClipInput").press("Enter");
  const found = await page.evaluate(() => {
    const { state } = window.opensyncTest;
    return { selected: state.selected, playhead: state.playhead };
  });
  assert.equal(found.selected, match.index);
  assert.equal(found.playhead, match.start);
  assert.deepEqual(errors,[]);
  await page.close();
 }
} finally {await browser.close();}
