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
  assert.deepEqual(errors,[]);
  await page.close();
 }
} finally {await browser.close();}
