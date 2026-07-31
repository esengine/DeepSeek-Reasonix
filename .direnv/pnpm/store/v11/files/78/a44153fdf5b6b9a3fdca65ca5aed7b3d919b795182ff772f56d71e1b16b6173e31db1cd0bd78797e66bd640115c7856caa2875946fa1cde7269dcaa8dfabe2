import{a as Y}from"./chunk-Y3FQM624.mjs";import{A as B,O as W,S as P,_,h as I,t as v}from"./chunk-KEUXMURM.mjs";import{b as R}from"./chunk-LIEV3EAG.mjs";import{a as c}from"./chunk-AQ6EADP3.mjs";var z="",G="",D="",O=[],$=new Map,w=c(e=>B(e,_()),"sanitizeText"),b=c(e=>{switch(e.type){case"terminal":return{...e,value:w(e.value)};case"nonterminal":return{...e,name:w(e.name)};case"sequence":return{...e,elements:e.elements.map(b)};case"choice":return{...e,alternatives:e.alternatives.map(b)};case"optional":return{...e,element:b(e.element)};case"repetition":return{...e,element:b(e.element),separator:e.separator?b(e.separator):void 0};case"special":return{...e,text:w(e.text)}}},"sanitizeAstNode"),K=c(()=>{z="",G="",D="",O.length=0,$.clear(),P(),R.debug("[Railroad] Database cleared")},"clear"),q=c(e=>{z=w(e),R.debug("[Railroad] Title set:",e)},"setTitle"),L=c(()=>z,"getTitle"),J=c(e=>{let i={...e,name:w(e.name),definition:b(e.definition),comment:e.comment?w(e.comment):void 0};R.debug("[Railroad] Adding rule:",i.name),$.has(i.name)&&R.warn(`[Railroad] Rule '${i.name}' is already defined. Overwriting.`),O.push(i),$.set(i.name,i)},"addRule"),Q=c(()=>O,"getRules"),Z=c(e=>$.get(e),"getRule"),ee=c(e=>{G=w(e).replace(/^\s+/g,""),R.debug("[Railroad] Accessibility title set:",e)},"setAccTitle"),te=c(()=>G,"getAccTitle"),re=c(e=>{D=w(e).replace(/\n\s+/g,`
`),R.debug("[Railroad] Accessibility description set:",e)},"setAccDescription"),ie=c(()=>D,"getAccDescription"),ne=q,oe=L,V={clear:K,setTitle:q,getTitle:L,addRule:J,getRules:Q,getRule:Z,setAccTitle:ee,getAccTitle:te,setAccDescription:re,getAccDescription:ie,setDiagramTitle:ne,getDiagramTitle:oe};var f={compactMode:!1,padding:10,verticalSeparation:8,horizontalSeparation:10,arcRadius:10,fontSize:14,fontFamily:"monospace",terminalFill:"#FFFFC0",terminalStroke:"#000000",terminalTextColor:"#000000",nonTerminalFill:"#FFFFFF",nonTerminalStroke:"#000000",nonTerminalTextColor:"#000000",lineColor:"#000000",strokeWidth:2,markerFill:"#000000",commentFill:"#E8E8E8",commentStroke:"#888888",commentTextColor:"#666666",specialFill:"#F0E0FF",specialStroke:"#8800CC",ruleNameColor:"#000066",showMarkers:!0,markerRadius:5};var ae=/^#(?:[\da-f]{3,4}|[\da-f]{6}|[\da-f]{8})$|^(?:rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\([\d\s%+,./-]+\)$|^[a-z]+$/i,le=/^[\w "',.-]+$/,se=new Set(["compactMode","padding","verticalSeparation","horizontalSeparation","arcRadius","fontSize","fontFamily","terminalFill","terminalStroke","terminalTextColor","nonTerminalFill","nonTerminalStroke","nonTerminalTextColor","lineColor","strokeWidth","markerFill","commentFill","commentStroke","commentTextColor","specialFill","specialStroke","ruleNameColor","showMarkers","markerRadius"]),X=c(e=>e?Object.keys(e).every(i=>i==="railroad"||se.has(i)):!1,"isRailroadStyleOptions"),de=c(e=>e?"railroad"in e&&e.railroad?e.railroad:X(e)?e:{}:{},"extractRailroadOverrides"),me=c(e=>{if(!e||X(e))return{};let{railroad:i,svgId:o,theme:n,look:t,...r}=e;return r},"extractThemeOverrides"),p=c((e,i)=>{if(typeof e!="string")return i;let o=e.trim();return ae.test(o)?o:i},"sanitizeColorValue"),j=c((e,i)=>{if(typeof e!="string")return i;let o=e.trim();return le.test(o)?o:i},"sanitizeFontFamilyValue"),C=c((e,i)=>{let o=typeof e=="number"?e:typeof e=="string"?Number.parseFloat(e):Number.NaN;return Number.isFinite(o)&&o>=0?o:i},"sanitizeNumberValue"),ce=c(e=>{let i=typeof e=="number"?e:typeof e=="string"?Number.parseFloat(e):Number.NaN;return Number.isFinite(i)&&i>0?i:void 0},"parseThemeFontSize"),pe=c(e=>{let i=j(e.fontFamily,f.fontFamily),o=ce(e.fontSize)??f.fontSize;return{...f,fontFamily:i,fontSize:o,terminalFill:p(e.secondBkg??e.secondaryColor,f.terminalFill),terminalStroke:p(e.secondaryBorderColor??e.lineColor,f.terminalStroke),terminalTextColor:p(e.secondaryTextColor??e.textColor,f.terminalTextColor),nonTerminalFill:p(e.mainBkg??e.background,f.nonTerminalFill),nonTerminalStroke:p(e.primaryBorderColor??e.lineColor,f.nonTerminalStroke),nonTerminalTextColor:p(e.primaryTextColor??e.textColor,f.nonTerminalTextColor),lineColor:p(e.lineColor,f.lineColor),markerFill:p(e.lineColor,f.markerFill),commentFill:p(e.labelBackground??e.tertiaryColor,f.commentFill),commentStroke:p(e.tertiaryBorderColor??e.lineColor,f.commentStroke),commentTextColor:p(e.tertiaryTextColor??e.textColor,f.commentTextColor),specialFill:p(e.tertiaryColor??e.secondaryColor,f.specialFill),specialStroke:p(e.tertiaryBorderColor??e.secondaryBorderColor,f.specialStroke),ruleNameColor:p(e.titleColor??e.textColor,f.ruleNameColor)}},"buildThemeDefaults"),N=c(e=>{let i=v(),o={...I(),...i.themeVariables??{},...me(e)},n=pe(o),t={...i.railroad??{},...de(e)};return{compactMode:t.compactMode??n.compactMode,padding:C(t.padding,n.padding),verticalSeparation:C(t.verticalSeparation,n.verticalSeparation),horizontalSeparation:C(t.horizontalSeparation,n.horizontalSeparation),arcRadius:C(t.arcRadius,n.arcRadius),fontSize:C(t.fontSize,n.fontSize),fontFamily:j(t.fontFamily,n.fontFamily),terminalFill:p(t.terminalFill,n.terminalFill),terminalStroke:p(t.terminalStroke,n.terminalStroke),terminalTextColor:p(t.terminalTextColor,n.terminalTextColor),nonTerminalFill:p(t.nonTerminalFill,n.nonTerminalFill),nonTerminalStroke:p(t.nonTerminalStroke,n.nonTerminalStroke),nonTerminalTextColor:p(t.nonTerminalTextColor,n.nonTerminalTextColor),lineColor:p(t.lineColor,n.lineColor),strokeWidth:C(t.strokeWidth,n.strokeWidth),markerFill:p(t.markerFill,n.markerFill),commentFill:p(t.commentFill,n.commentFill),commentStroke:p(t.commentStroke,n.commentStroke),commentTextColor:p(t.commentTextColor,n.commentTextColor),specialFill:p(t.specialFill,n.specialFill),specialStroke:p(t.specialStroke,n.specialStroke),ruleNameColor:p(t.ruleNameColor,n.ruleNameColor),showMarkers:t.showMarkers??n.showMarkers,markerRadius:C(t.markerRadius,n.markerRadius)}},"buildRailroadStyleOptions"),Ce=c(e=>{let{fontFamily:i,fontSize:o,terminalFill:n,terminalStroke:t,terminalTextColor:r,nonTerminalFill:a,nonTerminalStroke:g,nonTerminalTextColor:l,lineColor:s,strokeWidth:u,markerFill:h,commentFill:m,commentStroke:x,commentTextColor:d,specialFill:T,specialStroke:F,ruleNameColor:k}=N(e);return`
  .railroad-diagram {
    font-family: ${i};
    font-size: ${o}px;
  }

  .railroad-terminal rect {
    fill: ${n};
    stroke: ${t};
    stroke-width: ${u}px;
  }

  .railroad-terminal text {
    fill: ${r};
    font-family: ${i};
    font-size: ${o}px;
    text-anchor: middle;
    dominant-baseline: middle;
  }

  .railroad-nonterminal rect {
    fill: ${a};
    stroke: ${g};
    stroke-width: ${u}px;
  }

  .railroad-nonterminal text {
    fill: ${l};
    font-family: ${i};
    font-size: ${o}px;
    text-anchor: middle;
    dominant-baseline: middle;
  }

  .railroad-line {
    stroke: ${s};
    stroke-width: ${u}px;
    fill: none;
  }

  .railroad-start circle,
  .railroad-end circle {
    fill: ${h};
  }

  .railroad-comment ellipse {
    fill: ${m};
    stroke: ${x};
    stroke-width: ${u}px;
  }

  .railroad-comment text {
    fill: ${d};
    font-style: italic;
    font-family: ${i};
    font-size: ${o}px;
    text-anchor: middle;
    dominant-baseline: middle;
  }

  .railroad-special rect {
    fill: ${T};
    stroke: ${F};
    stroke-width: ${u}px;
    stroke-dasharray: 5,3;
  }

  .railroad-special text {
    fill: ${l};
    font-family: ${i};
    font-size: ${o}px;
    text-anchor: middle;
    dominant-baseline: middle;
  }

  .railroad-rule-name {
    font-weight: bold;
    fill: ${k};
    font-family: ${i};
    font-size: ${o}px;
  }

  .railroad-group {
    /* Grouping container, no specific styles */
  }
`},"getStyles");var S=class{constructor(){this.d=""}static{c(this,"PathBuilder")}moveTo(i,o){return this.d+=`M ${i} ${o} `,this}lineTo(i,o){return this.d+=`L ${i} ${o} `,this}horizontalTo(i){return this.d+=`H ${i} `,this}verticalTo(i){return this.d+=`V ${i} `,this}arcTo(i,o,n,t,r,a,g){return this.d+=`A ${i} ${o} ${n} ${t?1:0} ${r?1:0} ${a} ${g} `,this}build(){return this.d.trim()}},M=class{constructor(i,o=N()){this.textCache=new Map;this.svg=i,this.config=o}static{c(this,"RailroadRenderer")}measureText(i){if(this.textCache.has(i))return this.textCache.get(i);let o=this.svg.append("text").attr("font-family",this.config.fontFamily).attr("font-size",this.config.fontSize).text(i),n=o.node().getBBox(),t={width:n.width,height:n.height};return o.remove(),this.textCache.set(i,t),t}renderTerminal(i,o){let n=this.measureText(o),t=n.width+this.config.padding*2,r=n.height+this.config.padding*2,a=i.append("g").attr("class","railroad-terminal");return a.append("rect").attr("x",0).attr("y",0).attr("width",t).attr("height",r).attr("rx",10).attr("ry",10),a.append("text").attr("x",t/2).attr("y",r/2).text(o),{element:a.node(),dimensions:{width:t,height:r,up:r/2,down:r/2}}}renderNonTerminal(i,o){let n=this.measureText(o),t=n.width+this.config.padding*2,r=n.height+this.config.padding*2,a=i.append("g").attr("class","railroad-nonterminal");return a.append("rect").attr("x",0).attr("y",0).attr("width",t).attr("height",r),a.append("text").attr("x",t/2).attr("y",r/2).text(o),{element:a.node(),dimensions:{width:t,height:r,up:r/2,down:r/2}}}renderSequence(i,o){let n=o.map(s=>this.renderExpression(i,s)),t=0,r=0,a=0;for(let s of n)t+=s.dimensions.width,r=Math.max(r,s.dimensions.up),a=Math.max(a,s.dimensions.down);t+=(n.length-1)*this.config.horizontalSeparation;let g=i.append("g").attr("class","railroad-sequence"),l=0;for(let s=0;s<n.length;s++){let u=n[s],h=r-u.dimensions.up;if(g.node().appendChild(u.element).setAttribute("transform",`translate(${l}, ${h})`),s<n.length-1){let x=l+u.dimensions.width,d=x+this.config.horizontalSeparation,T=r;g.append("path").attr("class","railroad-line").attr("d",new S().moveTo(x,T).lineTo(d,T).build())}l+=u.dimensions.width+this.config.horizontalSeparation}return{element:g.node(),dimensions:{width:t,height:r+a,up:r,down:a}}}renderChoice(i,o){let n=o.map(m=>this.renderExpression(i,m)),t=0,r=0;for(let m of n)t=Math.max(t,m.dimensions.width),r+=m.dimensions.height;r+=(n.length-1)*this.config.verticalSeparation;let a=this.config.arcRadius,g=a*4,l=t+g,s=i.append("g").attr("class","railroad-choice"),u=0,h=r/2;for(let m of n){let x=u,d=x+m.dimensions.up,T=a*2+(t-m.dimensions.width)/2;s.node().appendChild(m.element).setAttribute("transform",`translate(${T}, ${x})`);let k=new S,y=d>h;d===h?k.moveTo(0,h).lineTo(T,d):k.moveTo(0,h).arcTo(a,a,0,!1,y,a,h+(y?a:-a)).lineTo(a,d-(y?a:-a)).arcTo(a,a,0,!1,!y,a*2,d).lineTo(T,d),s.append("path").attr("class","railroad-line").attr("d",k.build());let A=new S,E=T+m.dimensions.width,U=l-a*2;d===h?A.moveTo(E,d).lineTo(l,h):A.moveTo(E,d).lineTo(U,d).arcTo(a,a,0,!1,!y,l-a,d+(y?-a:a)).lineTo(l-a,h+(y?a:-a)).arcTo(a,a,0,!1,y,l,h),s.append("path").attr("class","railroad-line").attr("d",A.build()),u+=m.dimensions.height+this.config.verticalSeparation}return{element:s.node(),dimensions:{width:l,height:r,up:h,down:r-h}}}renderOptional(i,o){let n=this.renderExpression(i,o),t=this.config.arcRadius,r=t*2,a=n.dimensions.width+t*4,g=n.dimensions.height+r,l=i.append("g").attr("class","railroad-optional"),s=t*2,u=r;l.node().appendChild(n.element).setAttribute("transform",`translate(${s}, ${u})`);let m=u+n.dimensions.up,x=new S().moveTo(0,m).lineTo(t*2,m);l.append("path").attr("class","railroad-line").attr("d",x.build());let d=new S().moveTo(s+n.dimensions.width,m).lineTo(a,m);l.append("path").attr("class","railroad-line").attr("d",d.build());let T=new S().moveTo(0,m).arcTo(t,t,0,!1,!1,t,m-t).lineTo(t,t).arcTo(t,t,0,!1,!0,t*2,0).lineTo(a-t*2,0).arcTo(t,t,0,!1,!0,a-t,t).lineTo(a-t,m-t).arcTo(t,t,0,!1,!1,a,m);return l.append("path").attr("class","railroad-line").attr("d",T.build()),{element:l.node(),dimensions:{width:a,height:g,up:m,down:g-m}}}renderRepetition(i,o,n){let t=this.renderExpression(i,o),r=this.config.arcRadius,a=r*2,g=t.dimensions.width+r*4,l=n===0,s=t.dimensions.height+a+(l?a:0),u=i.append("g").attr("class","railroad-repetition"),h=r*2,m=l?a:0;u.node().appendChild(t.element).setAttribute("transform",`translate(${h}, ${m})`);let d=m+t.dimensions.up;u.append("path").attr("class","railroad-line").attr("d",new S().moveTo(0,d).lineTo(r*2,d).build()),u.append("path").attr("class","railroad-line").attr("d",new S().moveTo(h+t.dimensions.width,d).lineTo(g,d).build());let T=m+t.dimensions.height+r,F=new S().moveTo(h+t.dimensions.width,d).arcTo(r,r,0,!1,!0,h+t.dimensions.width+r,d+r).lineTo(h+t.dimensions.width+r,T).arcTo(r,r,0,!1,!0,h+t.dimensions.width,T+r).lineTo(r*2,T+r).arcTo(r,r,0,!1,!0,r,T).lineTo(r,d+r).arcTo(r,r,0,!1,!0,r*2,d);if(u.append("path").attr("class","railroad-line").attr("d",F.build()),l){let k=new S().moveTo(0,d).arcTo(r,r,0,!1,!1,r,d-r).lineTo(r,r).arcTo(r,r,0,!1,!0,r*2,0).lineTo(g-r*2,0).arcTo(r,r,0,!1,!0,g-r,r).lineTo(g-r,d-r).arcTo(r,r,0,!1,!1,g,d);u.append("path").attr("class","railroad-line").attr("d",k.build())}return{element:u.node(),dimensions:{width:g,height:s,up:d,down:s-d}}}renderSpecial(i,o){let n=this.measureText("? "+o+" ?"),t=n.width+this.config.padding*2,r=n.height+this.config.padding*2,a=i.append("g").attr("class","railroad-special");return a.append("rect").attr("x",0).attr("y",0).attr("width",t).attr("height",r),a.append("text").attr("x",t/2).attr("y",r/2).text("? "+o+" ?"),{element:a.node(),dimensions:{width:t,height:r,up:r/2,down:r/2}}}renderExpression(i,o){switch(o.type){case"terminal":return this.renderTerminal(i,o.value);case"nonterminal":return this.renderNonTerminal(i,o.name);case"sequence":return this.renderSequence(i,o.elements);case"choice":return this.renderChoice(i,o.alternatives);case"optional":return this.renderOptional(i,o.element);case"repetition":return this.renderRepetition(i,o.element,o.min);case"special":return this.renderSpecial(i,o.text);default:throw new Error(`Unknown node type: ${o.type}`)}}renderRule(i,o){let n=this.svg.append("g").attr("class","railroad-rule").attr("transform",`translate(0, ${o})`),t=i.name+" =",r=this.measureText(t).width+20,a=r+20,g=n.append("g"),l=this.renderExpression(g,i.definition),s=Math.max(20,l.dimensions.up),u=s-l.dimensions.up;return g.attr("transform",`translate(${a}, ${u})`),n.append("g").attr("class","railroad-rule-name-group").append("text").attr("class","railroad-rule-name").attr("x",0).attr("y",s).text(t),n.append("g").attr("class","railroad-start").append("circle").attr("cx",r).attr("cy",s).attr("r",this.config.markerRadius),n.append("g").attr("class","railroad-end").append("circle").attr("cx",a+l.dimensions.width+10).attr("cy",s).attr("r",this.config.markerRadius),n.append("path").attr("class","railroad-line").attr("d",new S().moveTo(r+this.config.markerRadius,s).lineTo(a,s).build()),n.append("path").attr("class","railroad-line").attr("d",new S().moveTo(a+l.dimensions.width,s).lineTo(a+l.dimensions.width+10-this.config.markerRadius,s).build()),{height:Math.max(40,u+l.dimensions.height+this.config.padding*2),width:a+l.dimensions.width+10+this.config.markerRadius}}renderDiagram(i){let o=this.config.padding,n=0;for(let t of i){let r=this.renderRule(t,o);o+=r.height+this.config.verticalSeparation,n=Math.max(n,r.width)}return{width:n+this.config.padding*2,height:o+this.config.padding}}},H=c((e,i,o)=>{W(e,i.height,i.width,o),e.attr("viewBox",`0 0 ${i.width} ${i.height}`)},"configureRailroadSvgSize"),ue=c((e,i,o)=>{R.debug(`[Railroad] Rendering diagram
`+e);try{let n=Y(i);n.attr("class","railroad-diagram");let r=v().railroad?.useMaxWidth??!0,a=V.getRules();if(R.debug(`[Railroad] Rendering ${a.length} rules`),a.length===0){R.warn("[Railroad] No rules to render"),H(n,{height:100,width:200},r);return}let l=new M(n,N()).renderDiagram(a);H(n,l,r),R.debug("[Railroad] Render complete")}catch(n){throw R.error("[Railroad] Render error:",n),n}},"draw"),De={draw:ue};export{V as a,Ce as b,De as c};
