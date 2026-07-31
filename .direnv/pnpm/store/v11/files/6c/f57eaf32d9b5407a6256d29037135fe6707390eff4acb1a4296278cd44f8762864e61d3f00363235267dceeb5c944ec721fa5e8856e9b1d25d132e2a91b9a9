import{a as P}from"./chunk-JQRUD6KW.mjs";import{a as j}from"./chunk-DMV4VAQV.mjs";import"./chunk-HNC4WDU7.mjs";import"./chunk-SATU7PGQ.mjs";import"./chunk-224SPVON.mjs";import"./chunk-EQFTRU2I.mjs";import"./chunk-E5QJAATJ.mjs";import"./chunk-66DQ2XMT.mjs";import"./chunk-D6VWDJW2.mjs";import"./chunk-BI6VK774.mjs";import"./chunk-XNMVGMAZ.mjs";import"./chunk-4RFN2BYJ.mjs";import"./chunk-FWYVLQTC.mjs";import"./chunk-RRFMTAIC.mjs";import"./chunk-TZUXEDM2.mjs";import"./chunk-OIYT25JQ.mjs";import"./chunk-NV3KIAZN.mjs";import"./chunk-L5GCOVLC.mjs";import{a as V}from"./chunk-Y3FQM624.mjs";import{o as R}from"./chunk-ENMKPL7Y.mjs";import"./chunk-STOV2HOB.mjs";import{O as A,S,T as w,U as M,V as O,W as G,X as L,Y as T,h as D,j as v,t as C}from"./chunk-KEUXMURM.mjs";import{b as $}from"./chunk-LIEV3EAG.mjs";import{a as n}from"./chunk-AQ6EADP3.mjs";var f={showLegend:!0,ticks:5,max:null,min:0,graticule:"circle"},k={axes:[],curves:[],options:f},g=structuredClone(k),q=v.radar,W=n(()=>R({...q,...C().radar}),"getConfig"),E=n(()=>g.axes,"getAxes"),H=n(()=>g.curves,"getCurves"),N=n(()=>g.options,"getOptions"),U=n(e=>{g.axes=e.map(t=>({name:t.name,label:t.label??t.name}))},"setAxes"),X=n(e=>{g.curves=e.map(t=>({name:t.name,label:t.label??t.name,entries:Y(t.entries)}))},"setCurves"),Y=n(e=>{if(e[0].axis==null)return e.map(r=>r.value);let t=E();if(t.length===0)throw new Error("Axes must be populated before curves for reference entries");return t.map(r=>{let a=e.find(o=>o.axis?.$refText===r.name);if(a===void 0)throw new Error("Missing entry for axis "+r.label);return a.value})},"computeCurveEntries"),Z=n(e=>{let t=e.reduce((r,a)=>(r[a.name]=a,r),{});g.options={showLegend:t.showLegend?.value??f.showLegend,ticks:t.ticks?.value??f.ticks,max:t.max?.value??f.max,min:t.min?.value??f.min,graticule:t.graticule?.value??f.graticule}},"setOptions"),J=n(()=>{S(),g=structuredClone(k)},"clear"),x={getAxes:E,getCurves:H,getOptions:N,setAxes:U,setCurves:X,setOptions:Z,getConfig:W,clear:J,setAccTitle:w,getAccTitle:M,setDiagramTitle:L,getDiagramTitle:T,getAccDescription:G,setAccDescription:O};var K=n(e=>{P(e,x);let{axes:t,curves:r,options:a}=e;x.setAxes(t),x.setCurves(r),x.setOptions(a)},"populate"),I={parse:n(async e=>{let t=await j("radar",e);$.debug(t),K(t)},"parse")};var Q=n((e,t,r,a)=>{let o=a.db,c=o.getAxes(),m=o.getCurves(),i=o.getOptions(),s=o.getConfig(),l=o.getDiagramTitle(),p=V(t),d=tt(p,s),u=i.max??Math.max(...m.map(b=>Math.max(...b.entries))),h=i.min,y=Math.min(s.width,s.height)/2;rt(d,c,y,i.ticks,i.graticule),et(d,c,y,s),at(d,c,m,h,u,i.graticule,s),it(d,m,i.showLegend,s),d.append("text").attr("class","radarTitle").text(l).attr("x",0).attr("y",-s.height/2-s.marginTop)},"draw"),tt=n((e,t)=>{let r=t.width+t.marginLeft+t.marginRight,a=t.height+t.marginTop+t.marginBottom,o={x:t.marginLeft+t.width/2,y:t.marginTop+t.height/2};return A(e,a,r,t.useMaxWidth??!0),e.attr("viewBox",`0 0 ${r} ${a}`).attr("overflow","visible"),e.append("g").attr("transform",`translate(${o.x}, ${o.y})`)},"drawFrame"),rt=n((e,t,r,a,o)=>{if(o==="circle")for(let c=0;c<a;c++){let m=r*(c+1)/a;e.append("circle").attr("r",m).attr("class","radarGraticule")}else if(o==="polygon"){let c=t.length;for(let m=0;m<a;m++){let i=r*(m+1)/a,s=t.map((l,p)=>{let d=2*p*Math.PI/c-Math.PI/2,u=i*Math.cos(d),h=i*Math.sin(d);return`${u},${h}`}).join(" ");e.append("polygon").attr("points",s).attr("class","radarGraticule")}}},"drawGraticule"),et=n((e,t,r,a)=>{let o=t.length;for(let c=0;c<o;c++){let m=t[c].label,i=2*c*Math.PI/o-Math.PI/2,s=Math.cos(i),l=Math.sin(i);e.append("line").attr("x1",0).attr("y1",0).attr("x2",r*a.axisScaleFactor*s).attr("y2",r*a.axisScaleFactor*l).attr("class","radarAxisLine");let p=s>.01?"start":s<-.01?"end":"middle",d=l>.01?"hanging":l<-.01?"auto":"central",u=4;e.append("text").text(m).attr("x",r*a.axisLabelFactor*s+u*s).attr("y",r*a.axisLabelFactor*l+u*l).attr("text-anchor",p).attr("dominant-baseline",d).attr("class","radarAxisLabel")}},"drawAxes");function at(e,t,r,a,o,c,m){let i=t.length,s=Math.min(m.width,m.height)/2;r.forEach((l,p)=>{if(l.entries.length!==i)return;let d=l.entries.map((u,h)=>{let y=2*Math.PI*h/i-Math.PI/2,b=ot(u,a,o,s),_=b*Math.cos(y),z=b*Math.sin(y);return{x:_,y:z}});c==="circle"?e.append("path").attr("d",nt(d,m.curveTension)).attr("class",`radarCurve-${p}`):c==="polygon"&&e.append("polygon").attr("points",d.map(u=>`${u.x},${u.y}`).join(" ")).attr("class",`radarCurve-${p}`)})}n(at,"drawCurves");function ot(e,t,r,a){let o=Math.min(Math.max(e,t),r);return a*(o-t)/(r-t)}n(ot,"relativeRadius");function nt(e,t){let r=e.length,a=`M${e[0].x},${e[0].y}`;for(let o=0;o<r;o++){let c=e[(o-1+r)%r],m=e[o],i=e[(o+1)%r],s=e[(o+2)%r],l={x:m.x+(i.x-c.x)*t,y:m.y+(i.y-c.y)*t},p={x:i.x-(s.x-m.x)*t,y:i.y-(s.y-m.y)*t};a+=` C${l.x},${l.y} ${p.x},${p.y} ${i.x},${i.y}`}return`${a} Z`}n(nt,"closedRoundCurve");function it(e,t,r,a){if(!r)return;let o=(a.width/2+a.marginRight)*3/4,c=-(a.height/2+a.marginTop)*3/4,m=20;t.forEach((i,s)=>{let l=e.append("g").attr("transform",`translate(${o}, ${c+s*m})`);l.append("rect").attr("width",12).attr("height",12).attr("class",`radarLegendBox-${s}`),l.append("text").attr("x",16).attr("y",0).attr("class","radarLegendText").text(i.label)})}n(it,"drawLegend");var F={draw:Q};var st=n((e,t)=>{let r="";for(let a=0;a<e.THEME_COLOR_LIMIT;a++){let o=e[`cScale${a}`];r+=`
		.radarCurve-${a} {
			color: ${o};
			fill: ${o};
			fill-opacity: ${t.curveOpacity};
			stroke: ${o};
			stroke-width: ${t.curveStrokeWidth};
		}
		.radarLegendBox-${a} {
			fill: ${o};
			fill-opacity: ${t.curveOpacity};
			stroke: ${o};
		}
		`}return r},"genIndexStyles"),ct=n(e=>{let t=D(),r=C(),a=R(t,r.themeVariables),o=R(a.radar,e);return{themeVariables:a,radarOptions:o}},"buildRadarStyleOptions"),B=n(({radar:e}={})=>{let{themeVariables:t,radarOptions:r}=ct(e);return`
	.radarTitle {
		font-size: ${t.fontSize};
		color: ${t.titleColor};
		dominant-baseline: hanging;
		text-anchor: middle;
	}
	.radarAxisLine {
		stroke: ${r.axisColor};
		stroke-width: ${r.axisStrokeWidth};
	}
	.radarAxisLabel {
		font-size: ${r.axisLabelFontSize}px;
		color: ${r.axisColor};
	}
	.radarGraticule {
		fill: ${r.graticuleColor};
		fill-opacity: ${r.graticuleOpacity};
		stroke: ${r.graticuleColor};
		stroke-width: ${r.graticuleStrokeWidth};
	}
	.radarLegendText {
		text-anchor: start;
		font-size: ${r.legendFontSize}px;
		dominant-baseline: hanging;
	}
	${st(t,r)}
	`},"styles");var Pt={parser:I,db:x,renderer:F,styles:B};export{Pt as diagram};
