(()=>{
'use strict';
const reduced=()=>document.body.classList.contains('reduce-motion')||window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
const frames=new Set();
function animate(el,keyframes,options={}){
  if(!el||reduced()||typeof el.animate!=='function')return;
  const animation=el.animate(keyframes,{duration:360,easing:'cubic-bezier(.2,.8,.2,1)',...options});
  frames.add(animation);animation.finished.then(()=>frames.delete(animation)).catch(()=>frames.delete(animation));
  return animation;
}
function revealPage(){
  const elements=document.querySelectorAll('.conversation-title,.conversation-description,.overview-header,.home-composer,.task-tile,.connection-card,.schedule-row');
  elements.forEach((el,i)=>animate(el,[{opacity:.15,transform:'translateY(9px)'},{opacity:1,transform:'translateY(0)'}],{duration:420,delay:Math.min(i*32,150)}));
}
function marker(){
  const tabs=document.querySelector('.canvas-tabs');if(!tabs)return;
  const active=tabs.querySelector('[aria-selected="true"]');if(!active)return;
  let line=tabs.querySelector('.tab-marker');if(!line){line=document.createElement('span');line.className='tab-marker';line.setAttribute('aria-hidden','true');tabs.appendChild(line);tabs.classList.add('has-marker')}
  line.style.left=active.offsetLeft+'px';line.style.width=active.offsetWidth+'px';
}
function ripple(button,e){
  if(reduced()||!button.isConnected||!button.animate||button.closest('.canvas-tabs')||button.classList.contains('wordmark'))return;
  const rect=button.getBoundingClientRect(),r=document.createElement('span');
  r.className='motion-ripple';r.style.left=(e.clientX?e.clientX-rect.left:rect.width/2)+'px';r.style.top=(e.clientY?e.clientY-rect.top:rect.height/2)+'px';
  button.classList.add('motion-pressed');button.appendChild(r);
  const a=animate(r,[{opacity:.8,transform:'translate(-50%,-50%) scale(0)'},{opacity:0,transform:'translate(-50%,-50%) scale('+Math.max(rect.width,rect.height)/5+')'}],{duration:450});
  const cleanup=()=>{r.remove();button.classList.remove('motion-pressed')};
  if(a)a.finished.then(cleanup,cleanup);else cleanup();
}
document.addEventListener('click',e=>{
  const target=e.target.closest('button');if(!target)return;
  const route=target.hasAttribute('data-route')||target.hasAttribute('data-task')||['home','create-task'].includes(target.dataset.action);
  const tab=target.hasAttribute('data-canvas-tab')||target.hasAttribute('data-cap-tab');
  const canvasOpen=target.dataset.action==='open-artifact';
  const update=['save-revision','answer','save-plan','approve','deny','pause','resume','simulate-reconnect'].includes(target.dataset.action);
  requestAnimationFrame(()=>{
    ripple(target,e);marker();
    if(route)revealPage();
    if(tab)animate(document.querySelector(target.hasAttribute('data-canvas-tab')?'#canvasContent':'.connection-grid'),[{opacity:.25,transform:'translateY(6px)'},{opacity:1,transform:'translateY(0)'}],{duration:300});
    if(canvasOpen)animate(document.querySelector('.paper'),[{opacity:.6,transform:'translateY(10px) scale(.985)'},{opacity:1,transform:'translateY(0) scale(1)'}],{duration:430});
    if(update)animate(document.querySelector('.status-pill'),[{transform:'scale(.95)',opacity:.5},{transform:'scale(1)',opacity:1}],{duration:270});
  });
},true);
document.addEventListener('submit',e=>{
  if(e.target.id!=='composer')return;
  requestAnimationFrame(()=>{
    const last=document.querySelector('#messages .message:last-child');
    animate(last,[{opacity:0,transform:'translateY(12px)'},{opacity:1,transform:'translateY(0)'}],{duration:400});
    animate(document.querySelector('.composer'),[{boxShadow:'0 0 0 3px #b9cbe630'},{boxShadow:'0 0 0 0 #b9cbe600'}],{duration:500});
    marker();
  });
},true);
const modal=document.getElementById('modal');
if(modal)new MutationObserver(()=>{
  if(modal.open)animate(modal.querySelector('.modal-body'),[{opacity:0,transform:'translateY(5px)'},{opacity:1,transform:'translateY(0)'}],{duration:280,delay:50});
}).observe(modal,{attributes:true,attributeFilter:['open']});
let version='';
const page=document.getElementById('page');
if(page)new MutationObserver(()=>{
  const paper=page.querySelector('.paper');if(!paper)return;
  const next=paper.querySelector('.paper-meta')?.textContent||'';
  const identity=page.querySelector('.conversation-title')?.textContent||'';
  const key=identity+'|'+next;
  if(version&&version.split('|')[0]===identity&&version!==key){
    animate(paper,[{boxShadow:'0 0 0 2px #b1c9e550,0 8px 25px #31547c0a'},{boxShadow:'0 0 0 0 #b1c9e500,0 8px 25px #31547c0a'}],{duration:850});
  }
  version=key;
}).observe(page,{childList:true,subtree:true});
new MutationObserver(()=>{if(reduced()){frames.forEach(a=>a.cancel());frames.clear()}}).observe(document.body,{attributes:true,attributeFilter:['class']});
window.addEventListener('resize',marker);
window.addEventListener('pagehide',()=>frames.forEach(a=>a.cancel()));
marker();
})();
