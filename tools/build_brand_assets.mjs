// Development-only asset generation. Runtime and normal builds use checked-in
// SVG/ICO files and do not require Node, Chromium, or Playwright.
import fs from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
const root=path.resolve(import.meta.dirname,'..'), dir=path.join(root,'desktop/frontend/brand');
await fs.mkdir(dir,{recursive:true});
const source=process.argv[2]||path.join(dir,'logo.svg');
const svg=await fs.readFile(source,'utf8');
if(/<script|<foreignObject|href\s*=\s*["'](?!#)/i.test(svg))throw Error('Logo must be self-contained vector artwork');
await fs.writeFile(path.join(dir,'logo.svg'),svg);
const crop=(view,w,h)=>svg.replace(/<svg\b[^>]*>/,`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="${view}" width="${w}" height="${h}">`);
const mark=crop('36.245 -12.43 168 168',256,256),word=crop('0 148 240.877 49',241,49);
await fs.writeFile(path.join(dir,'mark.svg'),mark);await fs.writeFile(path.join(dir,'wordmark.svg'),word);
const {chromium}=await import(pathToFileURL(process.env.OPENSYNC_PLAYWRIGHT).href);
const browser=await chromium.launch({executablePath:process.env.OPENSYNC_BROWSER,headless:true});
try{
const page=await browser.newPage(),sizes=[16,20,24,32,40,48,64,128,256],images=[];
for(const size of sizes){const data=await page.evaluate(async({svg,size})=>{const im=new Image();im.src='data:image/svg+xml;base64,'+btoa(unescape(encodeURIComponent(svg)));await im.decode();const c=document.createElement('canvas');c.width=c.height=size;c.getContext('2d').drawImage(im,0,0,size,size);return c.toDataURL('image/png').split(',')[1];},{svg:mark,size});images.push(Buffer.from(data,'base64'));}
const header=Buffer.alloc(6+16*sizes.length);header.writeUInt16LE(1,2);header.writeUInt16LE(sizes.length,4);let offset=header.length;
for(let i=0;i<sizes.length;i++){const pos=6+i*16;header[pos]=header[pos+1]=sizes[i]===256?0:sizes[i];header.writeUInt16LE(1,pos+4);header.writeUInt16LE(32,pos+6);header.writeUInt32LE(images[i].length,pos+8);header.writeUInt32LE(offset,pos+12);offset+=images[i].length;}
await fs.writeFile(path.join(root,'desktop/app.ico'),Buffer.concat([header,...images]));
await fs.writeFile(path.join(dir,'icon-256.png'),images.at(-1));
console.log('Generated original artwork viewports and nine-size Windows icon.');
}finally{await browser.close();}
