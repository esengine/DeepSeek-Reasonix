import{a as yt}from"./chunk-JQRUD6KW.mjs";import{a as pt}from"./chunk-DMV4VAQV.mjs";import"./chunk-HNC4WDU7.mjs";import"./chunk-SATU7PGQ.mjs";import"./chunk-224SPVON.mjs";import"./chunk-EQFTRU2I.mjs";import"./chunk-E5QJAATJ.mjs";import"./chunk-66DQ2XMT.mjs";import"./chunk-D6VWDJW2.mjs";import"./chunk-BI6VK774.mjs";import"./chunk-XNMVGMAZ.mjs";import"./chunk-4RFN2BYJ.mjs";import"./chunk-FWYVLQTC.mjs";import"./chunk-RRFMTAIC.mjs";import"./chunk-TZUXEDM2.mjs";import"./chunk-OIYT25JQ.mjs";import"./chunk-NV3KIAZN.mjs";import"./chunk-L5GCOVLC.mjs";import{a as ut}from"./chunk-Y3FQM624.mjs";import{o as H}from"./chunk-ENMKPL7Y.mjs";import"./chunk-STOV2HOB.mjs";import{O as at,S as it,T as st,U as ct,V as mt,W as lt,X as ft,Y as dt,h as _,j as rt,t as E}from"./chunk-KEUXMURM.mjs";import{b as j}from"./chunk-LIEV3EAG.mjs";import{a}from"./chunk-AQ6EADP3.mjs";var gt=a(()=>({domains:new Map,transitions:[]}),"createDefaultData"),Y=gt(),Pt=a(()=>Y.domains,"getDomains"),vt=a(()=>Y.transitions,"getTransitions"),zt=a(t=>{if(t)for(let e of t){let n=e.domain,o=(e.items??[]).map(c=>({label:c.label}));Y.domains.set(n,{name:n,items:o})}},"setDomains"),Lt=a(t=>{t&&(Y.transitions=t.filter(e=>e.from===e.to?(j.warn(`Cynefin: self-loop transition on domain "${e.from}" is not meaningful and will be skipped.`),!1):!0).map(e=>({from:e.from,to:e.to,label:e.label||void 0})))},"setTransitions"),Rt=a(()=>H({...rt.cynefin,...E().cynefin}),"getConfig"),jt=a(()=>{it(),Y=gt()},"clear"),O={getDomains:Pt,getTransitions:vt,setDomains:zt,setTransitions:Lt,getConfig:Rt,clear:jt,setAccTitle:st,getAccTitle:ct,setDiagramTitle:ft,getDiagramTitle:dt,getAccDescription:lt,setAccDescription:mt};var It=a(t=>{yt(t,O),O.setDomains(t.domains),O.setTransitions(t.transitions)},"populate"),xt={parse:a(async t=>{let e=await pt("cynefin",t);j.debug(e),It(e)},"parse")};function q(t){let e=t+1831565813|0;return e=Math.imul(e^e>>>15,e|1),e^=e+Math.imul(e^e>>>7,e|61),((e^e>>>14)>>>0)/4294967296}a(q,"seededRandom");function Wt(t){let e=0;for(let n=0;n<t.length;n++){let o=t.charCodeAt(n);e=(e<<5)-e+o,e|=0}return e}a(Wt,"hashString");function bt(t,e){return typeof t=="number"&&Number.isFinite(t)&&t!==0?t:Wt(e)}a(bt,"resolveSeed");function ht(t,e,n,o){let c=t/2,d=o??t*.015,w=7,I=e/w,l=[];for(let r=0;r<=w;r++){let p=q(n+r*17)*d*2-d;l.push({x:c+p,y:r*I})}let B=`M${l[0].x},${l[0].y}`;for(let r=0;r<l.length-1;r++){let p=l[r],s=l[r+1],f=(p.y+s.y)/2,C=r%2===0?1:-1,g=d*1.5*C*q(n+r*31+7),W=p.x+g,F=f,V=s.x-g;B+=` C${W},${F} ${V},${f} ${s.x},${s.y}`}return B}a(ht,"generateFoldPath");function Ct(t,e,n,o){let c=e/2,d=o??e*.015,w=7,I=t/w,l=[];for(let r=0;r<=w;r++){let p=q(n+r*23)*d*2-d;l.push({x:r*I,y:c+p})}let B=`M${l[0].x},${l[0].y}`;for(let r=0;r<l.length-1;r++){let p=l[r],s=l[r+1],f=(p.x+s.x)/2,C=r%2===0?1:-1,g=d*1.5*C*q(n+r*37+11),W=f,F=p.y+g,V=f,P=s.y-g;B+=` C${W},${F} ${V},${P} ${s.x},${s.y}`}return B}a(Ct,"generateHorizontalBoundary");function Dt(t,e){let n=t/2,o=e*.5,c=e,d=t*.03;return[`M${n},${o}`,`C${n+d},${o+(c-o)*.2}`,`${n-d*1.5},${o+(c-o)*.55}`,`${n+d*.5},${o+(c-o)*.75}`,`C${n-d},${o+(c-o)*.85}`,`${n+d*.3},${o+(c-o)*.95}`,`${n},${c}`].join(" ")}a(Dt,"generateCliffPath");function $t(t,e,n,o){return[`M${t-n},${e}`,`A${n},${o} 0 1,1 ${t+n},${e}`,`A${n},${o} 0 1,1 ${t-n},${e}`,"Z"].join(" ")}a($t,"generateConfusionPath");var wt={complex:{model:"Probe \u2192 Sense \u2192 Respond",practice:"Emergent Practices"},complicated:{model:"Sense \u2192 Analyse \u2192 Respond",practice:"Good Practices"},clear:{model:"Sense \u2192 Categorise \u2192 Respond",practice:"Best Practices"},chaotic:{model:"Act \u2192 Sense \u2192 Respond",practice:"Novel Practices"},confusion:{model:"",practice:"Disorder"}},Ft=a((t,e)=>{let n=t/2,o=e/2;return{complex:{cx:n/2,cy:o/2,x:0,y:0,w:n,h:o},complicated:{cx:n+n/2,cy:o/2,x:n,y:0,w:n,h:o},chaotic:{cx:n/2,cy:o+o/2,x:0,y:o,w:n,h:o},clear:{cx:n+n/2,cy:o+o/2,x:n,y:o,w:n,h:o},confusion:{cx:n,cy:o,x:n*.7,y:o*.7,w:n*.6,h:o*.6}}},"getDomainLayouts"),Vt=a(()=>{let t=_(),e=E();return H(t,e.themeVariables).cynefin},"getCynefinDomainColors"),Z=3,Gt=a((t,e,n,o)=>{let c=o.db,d=c.getDomains(),w=c.getTransitions(),I=c.getDiagramTitle(),l=c.getAccTitle(),B=c.getAccDescription(),r=c.getConfig(),p=Vt();j.debug("Rendering Cynefin diagram");let s=r.width,f=r.height,C=r.padding,g=r.showDomainDescriptions,W=r.boundaryAmplitude,F=s+C*2,V=f+C*2,P={complex:p.complexBg,complicated:p.complicatedBg,clear:p.clearBg,chaotic:p.chaoticBg,confusion:p.confusionBg},T=ut(e);at(T,V,F,r.useMaxWidth??!0),T.attr("viewBox",`0 0 ${F} ${V}`),l&&T.append("title").text(l),B&&T.append("desc").text(B);let A=T.append("g").attr("transform",`translate(${C}, ${C})`),G=Ft(s,f),J=bt(r.seed,e),At=A.append("g").attr("class","cynefin-backgrounds"),U=["complex","complicated","chaotic","clear"];for(let m of U){let i=G[m];At.append("rect").attr("class","cynefinDomain").attr("x",i.x).attr("y",i.y).attr("width",i.w).attr("height",i.h).attr("fill",P[m]).attr("fill-opacity",.4).attr("stroke","none")}let Q=A.append("g").attr("class","cynefin-boundaries");Q.append("path").attr("class","cynefinBoundary").attr("d",ht(s,f,J,W)).attr("fill","none"),Q.append("path").attr("class","cynefinBoundary").attr("d",Ct(s,f,J+100,W)).attr("fill","none"),Q.append("path").attr("class","cynefinCliff").attr("d",Dt(s,f)).attr("fill","none");let kt=s*.15,Nt=f*.15;A.append("path").attr("class","cynefinConfusion").attr("d",$t(s/2,f/2,kt,Nt)).attr("fill",P.confusion).attr("fill-opacity",.5);let K=A.append("g").attr("class","cynefin-labels");for(let m of U){let i=G[m];K.append("text").attr("class","cynefinDomainLabel").attr("x",i.cx).attr("y",g?i.cy-30:i.cy).attr("text-anchor","middle").attr("dominant-baseline","middle").text(m.charAt(0).toUpperCase()+m.slice(1))}if(K.append("text").attr("class","cynefinDomainLabel").attr("x",s/2).attr("y",g?f/2-10:f/2).attr("text-anchor","middle").attr("dominant-baseline","middle").text("Confusion"),g){let m=A.append("g").attr("class","cynefin-subtitles");for(let i of U){let u=G[i],y=wt[i];m.append("text").attr("class","cynefinSubtitle").attr("x",u.cx).attr("y",u.cy-10).attr("text-anchor","middle").attr("dominant-baseline","middle").text(y.model),m.append("text").attr("class","cynefinSubtitle").attr("x",u.cx).attr("y",u.cy+5).attr("text-anchor","middle").attr("dominant-baseline","middle").text(y.practice)}m.append("text").attr("class","cynefinSubtitle").attr("x",s/2).attr("y",f/2+8).attr("text-anchor","middle").attr("dominant-baseline","middle").text(wt.confusion.practice)}let tt=A.append("g").attr("class","cynefin-items"),k=26,et=10,St=["complex","complicated","chaotic","clear","confusion"];for(let m of St){let i=d.get(m);if(!i||i.items.length===0)continue;let u=G[m],y=m==="confusion",v=i.items,z=0;y&&i.items.length>Z&&(z=i.items.length-Z,v=i.items.slice(0,Z));let N;if(y){let b=g?22:14;N=u.cy+b}else N=u.cy+(g?25:15);if([...v].forEach((b,S)=>{let D=N+S*(k+4),M=tt.append("g"),L=M.append("text").attr("class","cynefinItemText").attr("x",0).attr("y",k/2).attr("text-anchor","middle").attr("dominant-baseline","central").text(b.label),h=b.label.length*7,x=L.node();if(x&&typeof x.getBBox=="function"){let X=x.getBBox();X.width>0&&(h=X.width)}let $=h+et*2,R=u.cx-$/2;M.attr("transform",`translate(${R}, ${D})`),M.insert("rect","text").attr("class","cynefinItem").attr("x",0).attr("y",0).attr("width",$).attr("height",k).attr("rx",4).attr("ry",4).attr("fill",P[m]).attr("fill-opacity",.95),L.attr("x",$/2).attr("y",k/2)}),z>0){let b=N+v.length*(k+4),S=`+${z} more`,D=tt.append("g"),M=D.append("text").attr("class","cynefinItemText").attr("x",0).attr("y",k/2).attr("text-anchor","middle").attr("dominant-baseline","central").text(S),L=S.length*7,h=M.node();if(h&&typeof h.getBBox=="function"){let R=h.getBBox();R.width>0&&(L=R.width)}let x=L+et*2,$=u.cx-x/2;D.attr("transform",`translate(${$}, ${b})`),D.insert("rect","text").attr("class","cynefinItemOverflow").attr("x",0).attr("y",0).attr("width",x).attr("height",k).attr("rx",4).attr("ry",4).attr("fill",P[m]).attr("fill-opacity",.6),M.attr("x",x/2).attr("y",k/2)}}if(w.length>0){let m=T.select("defs").empty()?T.append("defs"):T.select("defs"),i=`cynefin-arrow-${e}`;m.append("marker").attr("id",i).attr("viewBox","0 0 10 10").attr("refX",9).attr("refY",5).attr("markerWidth",6).attr("markerHeight",6).attr("orient","auto-start-reverse").append("path").attr("d","M 0 0 L 10 5 L 0 10 z").attr("class","cynefinArrowHead");let u=A.append("g").attr("class","cynefin-arrows");w.forEach(y=>{let v=G[y.from],z=G[y.to];if(!v||!z)return;if(y.from===y.to){j.warn(`Cynefin renderer: skipping self-loop on domain "${y.from}"`);return}let N=v.cx,b=v.cy,S=z.cx,D=z.cy,M=(N+S)/2,L=(b+D)/2,h=S-N,x=D-b,$=Math.sqrt(h*h+x*x),R=$*.15,X=-x/$,Mt=h/$,nt=M+X*R,ot=L+Mt*R;u.append("path").attr("class","cynefinArrowLine").attr("d",`M${N},${b} Q${nt},${ot} ${S},${D}`).attr("fill","none").attr("marker-end",`url(#${i})`),y.label&&u.append("text").attr("class","cynefinArrowLabel").attr("x",nt).attr("y",ot-6).attr("text-anchor","middle").attr("dominant-baseline","auto").text(y.label)})}I&&A.append("text").attr("class","cynefinTitle").attr("x",s/2).attr("y",-C/2).attr("text-anchor","middle").attr("dominant-baseline","middle").text(I)},"draw"),Bt={draw:Gt};var Et=a(()=>{let t=_(),e=E();return H(t,e.themeVariables).cynefin},"getCynefinTheme"),Ht=a(()=>{let t=Et();return`
	.cynefinDomain {
		stroke: none;
	}
	.cynefinDomainLabel {
		font-size: ${t.domainFontSize}px;
		font-weight: bold;
		fill: ${t.labelColor};
	}
	.cynefinSubtitle {
		font-size: ${t.itemFontSize-1}px;
		fill: ${t.textColor};
		font-style: italic;
	}
	.cynefinItem {
		fill-opacity: 0.95;
		stroke: ${t.boundaryColor};
		stroke-width: 1;
	}
	.cynefinItemText {
		font-size: ${t.itemFontSize}px;
		fill: ${t.textColor};
	}
	.cynefinItemOverflow {
		fill-opacity: 0.6;
		stroke: ${t.boundaryColor};
		stroke-width: 1;
		stroke-dasharray: 3 2;
	}
	.cynefinBoundary {
		stroke: ${t.boundaryColor};
		stroke-width: ${t.boundaryWidth};
		stroke-dasharray: 6 3;
	}
	.cynefinCliff {
		stroke: ${t.cliffColor};
		stroke-width: ${t.cliffWidth};
	}
	.cynefinConfusion {
		stroke: ${t.boundaryColor};
		stroke-width: 1.5;
		stroke-dasharray: 4 2;
	}
	.cynefinArrowLine {
		stroke: ${t.arrowColor};
		stroke-width: ${t.arrowWidth};
		fill: none;
	}
	.cynefinArrowHead {
		fill: ${t.arrowColor};
		stroke: none;
	}
	.cynefinArrowLabel {
		font-size: ${t.itemFontSize-1}px;
		fill: ${t.textColor};
	}
	.cynefinTitle {
		font-size: ${t.domainFontSize+2}px;
		font-weight: bold;
		fill: ${t.labelColor};
	}
	`},"styles"),Tt=Ht;var we={parser:xt,db:O,renderer:Bt,styles:Tt};export{we as diagram};
