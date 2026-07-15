package main

import _ "embed"

//go:embed assets/default_spritesheet.webp
var defaultSpritesheet []byte

func petPageHTML() string {
	return `<!DOCTYPE html>
<html style="width:100%;height:100%;margin:0;overflow:hidden;background:transparent">
<head>
<meta charset="utf-8">
<style>
html,body{margin:0;padding:0;background:transparent;overflow:hidden;width:100%;height:100%;
  font-family:-apple-system,system-ui,sans-serif}
body input,body textarea,body button{cursor:text!important;user-select:text!important;-webkit-user-select:text!important}
.stage{position:fixed;left:0;top:0;width:100%;height:100%;display:flex;
  align-items:center;justify-content:center;pointer-events:none}
#pet{aspect-ratio:192/208;width:7rem;image-rendering:pixelated;
  background-image:url('/sprite');background-repeat:no-repeat;
  background-size:800% 900%;background-position:0% 0%;pointer-events:auto;cursor:grab;
  transform:scale(1);transform-origin:center center;transition:transform 120ms ease-out}
#pet.dragging{cursor:grabbing;transition:none}
</style>
</head>
<body>
<div class="stage"><div class="pet" id="pet" data-state="idle"></div></div>
<script>
var COLS=8, ROWS=9;
var STATES={
  idle:{row:0,frames:[{c:0,d:280},{c:1,d:110},{c:2,d:110},{c:3,d:140},{c:4,d:140},{c:5,d:320}],slow:6},
  "running-right":{row:1,count:8,dur:120,last:220},
  "running-left":{row:2,count:8,dur:120,last:220},
  waving:{row:3,count:4,dur:140,last:280},
  jumping:{row:4,count:5,dur:140,last:280},
  failed:{row:5,count:8,dur:140,last:240},
  waiting:{row:6,count:6,dur:150,last:260},
  running:{row:7,count:6,dur:120,last:220},
  review:{row:8,count:6,dur:150,last:280},
};
var SLUG="default";
function escapeHtml(s){return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');}
var curState="idle", animTimer=null;
var idleSince=null, lastIdle=null, tick=0;
var pet=document.getElementById("pet");

function pos(c,r){return(c/(COLS-1)*100)+'% '+(r/(ROWS-1)*100)+'%';}
function buildFrames(s){
  if(s.frames){var sl=s.slow||1;return s.frames.map(function(f){return{c:f.c,r:s.row,d:f.d*sl};});}
  return Array.from({length:s.count},function(_,i){return{c:i,r:s.row,d:i===s.count-1?s.last:s.dur};});
}
function play(state){
  if(state===curState&&state!=='running-right'&&state!=='running-left')return;
  curState=state;
  pet.dataset.state=state;
  if(state==='idle'||state==='stop'){idleSince=Date.now();lastIdle=null;}
  else{idleSince=null;}
  var def=STATES[state]||STATES.idle;
  var frames=buildFrames(def);
  if(animTimer){clearTimeout(animTimer);animTimer=null;}
  var i=0;
  pet.style.backgroundPosition=pos(frames[0].c,frames[0].r);
  if(frames.length===1)return;
  (function tick(){
    animTimer=setTimeout(function(){
      i=(i+1)%frames.length;
      pet.style.backgroundPosition=pos(frames[i].c,frames[i].r);
      tick();
    },frames[i].d);
  })();
}
play('idle');

// ── Public API (called from Go) ──
window.__realState='idle';
window.__stateLabel='idle';
window.__sessions=0;
var I=window.__petI18n||{};var STATE_LABELS=I.labels||{idle:'idle','running-right':'running','running-left':'running',running:'running',waving:'waving',waiting:'waiting',failed:'failed',review:'review',jumping:'jumping'};

// setHookState: sets the real underlying state (from agent events).
// This is the persistent state that temporary interactions restore to.
window.setHookState=function(state){
  var m={stop:'idle','session_end':'idle',tool_call:'running-right',
    need_confirm:'waiting',tool_error:'review',error_final:'failed'};
  var s=m[state]||state;
  window.__realState=s;
  window.__stateLabel=STATE_LABELS[s]||s;
  play(s);
};

// setState: sets a temporary animation state, then restores to realState.
// Used by click interactions. If durationMs is 0, stays until next setHookState.
window.setState=function(state,durationMs){
  play(state);
  if(window.__stateTimer)clearTimeout(window.__stateTimer);
  if(durationMs){window.__stateTimer=setTimeout(function(){play(window.__realState||'idle');window.__stateTimer=null;},durationMs);}
};

function fmt(s,n){return s&&s.replace?s.replace('%d',n):'';}
function fmt2(s,n,st){return s&&s.replace?s.replace('%d',n).replace('%s',st):'';}

// ── Running direction switch (4s) ──
setInterval(function(){
  if(curState!=='running-right'&&curState!=='running-left')return;
  play(curState==='running-right'?'running-left':'running-right');
},4000);

// ── Sprite ──
window.__petSpriteUrl='';
function setDefaultSprite(url){pet.style.backgroundImage="url('"+url+"')";window.__petSpriteUrl=url;}
function resetPetSprite(){if(window.__petSpriteUrl)pet.style.backgroundImage="url('"+window.__petSpriteUrl+"')";}
function setPetSprite(url){pet.style.backgroundImage="url('"+url+"')";}
function setScale(s){
  if(s<0.5||s>1.5)return;
  window.__petScale=s;
  document.body.style.zoom=s;
  var sizeLabel=document.getElementById('size-label');
  if(sizeLabel)sizeLabel.textContent='🔍 大小 x'+s.toFixed(1);
}

// ── Bubble (JS dynamic positioning) ──
var bubbleEl=null, bubbleTextEl=null;
function ensureBubble(){
  if(bubbleEl)return bubbleEl;
  bubbleEl=document.createElement('div');
  bubbleEl.style.cssText='position:fixed;padding:4px 8px;border-radius:10px;background:#fff;color:#111;font:600 11px system-ui;line-height:1.2;box-shadow:0 2px 6px rgba(0,0,0,.3);text-align:left;white-space:normal;max-width:130px;word-break:break-all;overflow-wrap:break-word;min-width:40px;display:flex;align-items:center;gap:6px;opacity:0;transition:opacity 180ms ease;pointer-events:none;z-index:5';
  bubbleTextEl=document.createElement('span');
  bubbleTextEl.style.cssText='display:block;min-width:0';
  bubbleEl.appendChild(bubbleTextEl);
  document.body.appendChild(bubbleEl);
  return bubbleEl;
}
function positionBubble(){
  if(!bubbleEl||!bubbleTextEl.textContent)return;
  bubbleEl.style.left='50%';bubbleEl.style.transform='translateX(-50%)';
  bubbleEl.style.top='4px';bubbleEl.style.bottom='auto';
}
window.__persistentBubble='';
function showBubble(text,ms){
  var el=ensureBubble();
  bubbleTextEl.textContent=text||'';
  clearTimeout(window.__bubbleTimer);
  if(text){el.style.opacity='1';positionBubble();if(ms)window.__bubbleTimer=setTimeout(function(){el.style.opacity='0';},ms);}
  else el.style.opacity='0';
}
window.showTransientBubble=function(text,ms){
  if(!text)return;
  var saved=window.__persistentBubble||'';
  var el=ensureBubble();bubbleTextEl.textContent=text;
  clearTimeout(window.__bubbleTimer);el.style.opacity='1';positionBubble();
  window.__bubbleTimer=setTimeout(function(){if(saved)bubbleTextEl.textContent=saved;else el.style.opacity='0';},ms);
};

// ── Drag: mousedown anywhere except inputs ──
var dragging=false, wasDrag=false, startX=0, startY=0, lastMove=0;
document.body.addEventListener('mousedown',function(e){
  if(e.button!==0)return;
  var t=e.target;
  if(t.tagName==='INPUT'||t.tagName==='TEXTAREA'||t.tagName==='SELECT'||t.tagName==='BUTTON'||t.closest('input')||t.closest('textarea')||t.closest('select')||t.closest('button'))return;
  wasDrag=false; dragging=true; startX=e.screenX; startY=e.screenY; lastMove=0;
});
window.addEventListener('mousemove',function(e){
  if(!dragging)return;var now=Date.now();if(now-lastMove<16)return;lastMove=now;
  var dx=e.screenX-startX,dy=e.screenY-startY;
  if(Math.abs(dx)>2||Math.abs(dy)>2)wasDrag=true;
  window.__petMoveBy(dx,dy);startX=e.screenX;startY=e.screenY;
});
window.addEventListener('mouseup',function(){
  if(!dragging)return;dragging=false;window.__petSavePos();
  if(wasDrag){showBubble(window.__realState==='idle'?(I.dragIdle||''): (I.dragBusy||''),2000);if(window.__realState==='idle')window.setState('waving',2000);}
  wasDrag=false;
});

// ── Click (head vs body) ──
var headTexts=I.headTexts||['Hehe~'];
var bodyTexts=I.bodyTexts||['Meow!'];
var clickActions=['waving','jumping','waiting','review'];var clickBusy=false;
pet.addEventListener('click',function(e){
  if(wasDrag||(window.__realState&&window.__realState!=='idle')||clickBusy)return;
  clickBusy=true;
  var rect=pet.getBoundingClientRect();var y=(e.clientY-rect.top)/rect.height;
  var t,a;var texts=(y<0.3)?headTexts:bodyTexts;var acts=(y<0.3)?['waving']:clickActions;
  t=texts[Math.floor(Math.random()*texts.length)];a=acts[Math.floor(Math.random()*acts.length)];
  window.showTransientBubble(t,3000);window.setState(a,2000);
  setTimeout(function(){clickBusy=false;},2000);
});
pet.addEventListener('dblclick',function(){
  var st=window.__realState||'idle',cnt=window.__sessions||0,msg;
  if(st==='idle')msg=cnt===0?(I.dblClickIdle0||'Nobody here... 😴'):cnt===1?(I.dblClickIdle1||'1 session idle ✨'):fmt(I.dblClickIdleMany||'%d sessions idle ✨',cnt);
  else if(st.indexOf('running')!==-1)msg=fmt(I.dblClickRunning||'Working · %d sessions 🏃',cnt);
  else if(st==='waiting')msg=fmt(I.dblClickWaiting||'Waiting · %d sessions ⏳',cnt);
  else if(st==='review')msg=fmt(I.dblClickReview||'Issues · %d sessions 😅',cnt);
  else if(st==='failed')msg=fmt(I.dblClickFailed||'Error · %d sessions 😱',cnt);
  else msg=fmt2(I.dblClickFallback,cnt,st);
  window.showTransientBubble(msg,3000);
});

// ── Context menu (manual close only) ──
pet.addEventListener('contextmenu',function(e){
  e.preventDefault();
  var menu=document.getElementById('pet-menu');
  if(!menu){
    menu=document.createElement('div');
    menu.id='pet-menu';menu.style.cssText='position:fixed;background:rgba(20,20,22,0.96);border:1px solid rgba(255,255,255,0.08);border-radius:10px;padding:12px;z-index:999;left:4px;right:4px;top:4px;bottom:4px;overflow-y:auto;overflow-x:hidden;pointer-events:auto;display:none';
    document.body.appendChild(menu);
    document.addEventListener('click',function(ev){if(menu&&menu.style.display==='block'&&!menu.contains(ev.target))menu.style.display='none';});
  }
  var pets=window.__PET_PETS||[{slug:'default',name:'🐱 Default'}];
  var sc=window.__petScale||1.0;
  // Build full innerHTML string first, then attach listeners.
  var html='<div style="padding:6px 8px 10px;color:rgba(255,255,255,0.7);font-size:12px;text-align:center;font-weight:600">'+(I.menuTitle||'🐾')+'</div><div id="menu-close" style="position:absolute;top:8px;right:12px;color:rgba(255,255,255,0.5);cursor:pointer;font-size:16px;line-height:1">×</div>';
  pets.forEach(function(p){
    var sel=(window.__petSlug===p.slug)||(!window.__petSlug&&p.slug==='default');
    var del=p.slug!=='default'?'<span data-del="'+p.slug+'" style="padding:2px 6px;color:rgba(255,80,80,0.7);cursor:pointer;font-size:11px">✕</span>':'';
    html+='<div style="display:flex;align-items:center;padding:2px 0">'+del+'<span data-slug="'+p.slug+'" title="'+p.slug+'" style="flex:1;padding:4px 8px;border-radius:6px;color:'+(sel?'#00e676':'#ddd')+';cursor:pointer;font-size:12px">'+(p.name||p.slug)+'</span></div>';
  });
  html+='<hr style="margin:8px 0;border:none;border-top:1px solid rgba(255,255,255,0.08)"><div id="size-label" style="padding:4px 8px;color:rgba(255,255,255,0.6);font-size:11px;text-align:center">'+(I.menuSize||'🔍 Size x')+sc.toFixed(1)+'</div>';
  html+='<div style="display:flex;gap:6px;justify-content:center;padding:4px 0"><div id="scale-minus" style="width:28px;text-align:center;color:#ddd;cursor:pointer;font-size:14px;border-radius:6px;padding:4px 0">−</div><div id="scale-plus" style="width:28px;text-align:center;color:#ddd;cursor:pointer;font-size:14px;border-radius:6px;padding:4px 0">+</div></div>';
  html+='<hr style="margin:8px 0;border:none;border-top:1px solid rgba(255,255,255,0.08)"><input id="pet-install-input" placeholder="'+(I.menuInstallPH||'Enter pet slug')+'" onmousedown="event.stopPropagation()" style="display:block;width:calc(100% - 8px);margin:4px auto;padding:5px 6px;border:1px solid rgba(255,255,255,0.2);border-radius:5px;background:rgba(255,255,255,0.05);color:#eee;font-size:12px;outline:none;box-sizing:border-box">';
  html+='<div style="display:flex;gap:6px;padding:2px 0"><div id="btn-install" style="flex:1;padding:6px 0;border-radius:6px;color:#4caf50;cursor:pointer;font-size:12px;text-align:center;font-weight:500">'+(I.menuInstallBtn||'📥 Install')+'</div><div id="btn-random" style="flex:1;padding:6px 0;border-radius:6px;color:#ff9800;cursor:pointer;font-size:12px;text-align:center;font-weight:500">'+(I.menuRandomBtn||'🎲 Random')+'</div></div>';
  html+='<div id="btn-market" style="padding:6px 8px;color:rgba(255,255,255,0.5);font-size:11px;text-align:center;cursor:pointer;border-radius:6px">'+(I.menuMarketBtn||'🌐 Market')+'</div>';
  menu.innerHTML=html;
  menu.style.display='block';
  // Attach listeners after innerHTML is set.
  document.getElementById('menu-close').onclick=function(){menu.style.display='none';};
  menu.querySelectorAll('[data-slug]').forEach(function(el){
    var slug=el.dataset.slug;
    var sel=(window.__petSlug===slug)||(!window.__petSlug&&slug==='default');
    el.addEventListener('mouseenter',function(){el.style.background='rgba(255,255,255,0.1)';el.style.color='#fff';});
    el.addEventListener('mouseleave',function(){el.style.background='';el.style.color=sel?'#00e676':'#ddd';});
    el.addEventListener('click',function(){menu.style.display='none';window.__petCommand('setSlug',{slug:slug});});
  });
  menu.querySelectorAll('[data-del]').forEach(function(el){
    var slug=el.dataset.del;
    el.addEventListener('mouseenter',function(){el.style.color='#f55';});
    el.addEventListener('mouseleave',function(){el.style.color='rgba(255,80,80,0.7)';});
    el.addEventListener('click',function(e){e.stopPropagation();window.__petCommand('deletePet',{slug:slug});menu.style.display='none';});
  });
  var sizeLabel=document.getElementById('size-label');
  function updateScale(delta){
    var cur=parseFloat(sizeLabel.textContent.match(/[\d.]+/)?.[0]||1);
    var ns=Math.round((cur+delta)*10)/10;
    if(ns<0.5||ns>1.5)return;
    sizeLabel.textContent=(I.menuSize||'🔍 Size x')+ns.toFixed(1);
    window.__petCommand('setScale',{scale:ns});
  }
  document.getElementById('scale-minus').addEventListener('click',function(e){e.stopPropagation();updateScale(-0.1);});
  document.getElementById('scale-plus').addEventListener('click',function(e){e.stopPropagation();updateScale(0.1);});
  document.getElementById('btn-install').addEventListener('click',function(){
    var inp=document.getElementById('pet-install-input');
    var nm=inp.value.trim().toLowerCase().replace(/\s+/g,'-');
    if(!nm){inp.focus();return;}
    window.__petCommand('installPet',{name:nm});
    menu.style.display='none';
  });
  document.getElementById('btn-random').addEventListener('click',function(){
    window.__petCommand('installPet',{name:'__random__'});
    menu.style.display='none';
  });
  document.getElementById('btn-market').addEventListener('click',function(){
    window.__petCommand('openUrl',{url:'https://petdex.dev/collections'});
  });
});

// ── Idle animation ──
setInterval(function(){
  tick++;if(curState!=='idle')return;var now=Date.now();
  if(idleSince===null)idleSince=now;var elapsed=now-idleSince;
  if(elapsed<20000||elapsed>7200000)return;
  if(lastIdle!==null&&now-lastIdle<20000)return;
  var prob=elapsed<300000?40:elapsed<1800000?15:5;
  if(Math.random()*100>=prob)return;
  var bubbles=I.idleBubbles||['So bored...'];
  var acts=['waving','jumping','review'];
  showBubble(bubbles[Math.floor(Math.random()*bubbles.length)],4000);
  window.setState(acts[Math.floor(Math.random()*acts.length)],3000);
  lastIdle=now;
},1000);
</script>
</body>
</html>`
}
