import{a as ae}from"./chunk-JQRUD6KW.mjs";import{a as ne}from"./chunk-DMV4VAQV.mjs";import"./chunk-HNC4WDU7.mjs";import"./chunk-SATU7PGQ.mjs";import"./chunk-224SPVON.mjs";import"./chunk-EQFTRU2I.mjs";import"./chunk-E5QJAATJ.mjs";import"./chunk-66DQ2XMT.mjs";import"./chunk-D6VWDJW2.mjs";import"./chunk-BI6VK774.mjs";import"./chunk-XNMVGMAZ.mjs";import"./chunk-4RFN2BYJ.mjs";import"./chunk-FWYVLQTC.mjs";import"./chunk-RRFMTAIC.mjs";import"./chunk-TZUXEDM2.mjs";import"./chunk-OIYT25JQ.mjs";import"./chunk-NV3KIAZN.mjs";import"./chunk-L5GCOVLC.mjs";import{a as se}from"./chunk-Y3FQM624.mjs";import{n as re,o as oe}from"./chunk-ENMKPL7Y.mjs";import"./chunk-STOV2HOB.mjs";import{O as V,S as U,T as X,U as Z,V as J,W as K,X as Q,Y,_ as ee,j as q}from"./chunk-KEUXMURM.mjs";import{F as O,I as ie,b as S,m as te}from"./chunk-LIEV3EAG.mjs";import{a as o}from"./chunk-AQ6EADP3.mjs";var ce=q.pie,_={sections:new Map,showData:!1,config:ce},A=_.sections,z=_.showData,Pe=structuredClone(ce),xe=o(()=>structuredClone(Pe),"getConfig"),Ce=o(()=>{A=new Map,z=_.showData,U()},"clear"),ve=o(({label:e,value:i})=>{if(i<0)throw new Error(`"${e}" has invalid value: ${i}. Negative values are not allowed in pie charts. All slice values must be >= 0.`);A.has(e)||(A.set(e,i),S.debug(`added new section: ${e}, with value: ${i}`))},"addSection"),we=o(()=>A,"getSections"),Ae=o(e=>{z=e},"setShowData"),Te=o(()=>z,"getShowData"),T={getConfig:xe,clear:Ce,setDiagramTitle:Q,getDiagramTitle:Y,setAccTitle:X,getAccTitle:Z,setAccDescription:J,getAccDescription:K,addSection:ve,getSections:we,setShowData:Ae,getShowData:Te};var $e=o((e,i)=>{ae(e,i),i.setShowData(e.showData),e.sections.map(i.addSection)},"populateDb"),le={parse:o(async e=>{let i=await ne("pie",e);S.debug(i),$e(i,T)},"parse")};var ke=o(e=>`
  .pieCircle{
    stroke: ${e.pieStrokeColor};
    stroke-width : ${e.pieStrokeWidth};
    opacity : ${e.pieOpacity};
  }
  .pieCircle.highlighted{
    scale: 1.05;
    opacity: 1;
  }
  .pieCircle.highlightedOnHover:hover{
    transition-duration: 250ms;
    scale: 1.05;
    opacity: 1;
  }
  .pieOuterCircle{
    stroke: ${e.pieOuterStrokeColor};
    stroke-width: ${e.pieOuterStrokeWidth};
    fill: none;
  }
  .pieTitleText {
    text-anchor: middle;
    font-size: ${e.pieTitleTextSize};
    fill: ${e.pieTitleTextColor};
    font-family: ${e.fontFamily};
  }
  .slice {
    font-family: ${e.fontFamily};
    fill: ${e.pieSectionTextColor};
    font-size:${e.pieSectionTextSize};
    // fill: white;
  }
  .legend text {
    fill: ${e.pieLegendTextColor};
    font-family: ${e.fontFamily};
    font-size: ${e.pieLegendTextSize};
  }
`,"getStyles"),me=ke;var Re=o(e=>{let i=[...e.values()].reduce((s,h)=>s+h,0),B=[...e.entries()].map(([s,h])=>({label:s,value:h})).filter(s=>s.value/i*100>=1);return ie().value(s=>s.value).sort(null)(B)},"createPieArcs"),Ee=o((e,i,B,F)=>{S.debug(`rendering pie chart
`+e);let s=F.db,h=ee(),g=oe(s.getConfig(),h.pie),j=40,n=18,l=4,b=450,y=b,$=se(i),C=$.append("g");C.attr("transform","translate("+y/2+","+b/2+")");let{themeVariables:a}=h,[L]=re(a.pieOuterStrokeWidth);L??=2;let de=g.legendPosition,M=g.textPosition,ge=g.donutHole>0&&g.donutHole<=.9?g.donutHole:0,u=Math.min(y,b)/2-j,ue=O().innerRadius(ge*u).outerRadius(u),fe=O().innerRadius(u*M).outerRadius(u*M),P=C.append("g");P.append("circle").attr("cx",0).attr("cy",0).attr("r",u+L/2).attr("class","pieOuterCircle");let v=s.getSections(),he=Re(v),De=[a.pie1,a.pie2,a.pie3,a.pie4,a.pie5,a.pie6,a.pie7,a.pie8,a.pie9,a.pie10,a.pie11,a.pie12],k=0;v.forEach(t=>{k+=t});let W=he.filter(t=>(t.data.value/k*100).toFixed(0)!=="0"),R=te(De).domain([...v.keys()]);P.selectAll("mySlices").data(W).enter().append("path").attr("d",ue).attr("fill",t=>R(t.data.label)).attr("class",t=>{let r="pieCircle";return g.highlightSlice==="hover"?r+=" highlightedOnHover":g.highlightSlice===t.data.label&&(r+=" highlighted"),r}),P.selectAll("mySlices").data(W).enter().append("text").text(t=>(t.data.value/k*100).toFixed(0)+"%").attr("transform",t=>"translate("+fe.centroid(t)+")").style("text-anchor","middle").attr("class","slice");let Se=C.append("text").text(s.getDiagramTitle()).attr("x",0).attr("y",-(b-50)/2).attr("class","pieTitleText"),x=[...v.entries()].map(([t,r])=>({label:t,value:r})),f=C.selectAll(".legend").data(x).enter().append("g").attr("class","legend");f.append("rect").attr("width",n).attr("height",n).style("fill",t=>R(t.label)).style("stroke",t=>R(t.label)),f.append("text").attr("x",n+l).attr("y",n-l).text(t=>s.getShowData()?`${t.label} [${t.value}]`:t.label);let D=Math.max(...f.selectAll("text").nodes().map(t=>t?.getBoundingClientRect().width??0)),w=b,E=y+j,c=n+l,G=x.length*c;switch(de){case"center":f.attr("transform",(t,r)=>{let m=c*x.length/2,p=-D/2-(n+l),d=r*c-m;return"translate("+p+","+d+")"});break;case"top":w+=G,f.attr("transform",(t,r)=>{let m=u,p=-D/2-(n+l),d=r*c-m;return`translate(${p}, ${d})`}),P.attr("transform",()=>`translate(0, ${G+c})`);break;case"bottom":w+=G,f.attr("transform",(t,r)=>{let m=-u-c,p=-D/2-(n+l),d=r*c-m;return"translate("+p+","+d+")"});break;case"left":E+=n+l+D,f.attr("transform",(t,r)=>{let m=c*x.length/2,p=-u-(n+l),d=r*c-m;return"translate("+p+","+d+")"}),P.attr("transform",()=>`translate(${D+n+l}, 0)`);break;case"right":default:E+=n+l+D,f.attr("transform",(t,r)=>{let m=c*x.length/2,p=12*n,d=r*c-m;return"translate("+p+","+d+")"});break}let H=Se.node()?.getBoundingClientRect().width??0,be=y/2-H/2,ye=y/2+H/2,N=Math.min(0,be),I=Math.max(E,ye)-N;$.attr("viewBox",`${N} 0 ${I} ${w}`),V($,w,I,g.useMaxWidth)},"draw"),pe={draw:Ee};var ot={parser:le,db:T,renderer:pe,styles:me};export{ot as diagram};
