import{p as $e}from"./chunk-ENMKPL7Y.mjs";import{a as sn}from"./chunk-STOV2HOB.mjs";import{G as oe,O as ce,S as le,T as ue,U as de,V as fe,W as he,X as me,Y as ke,_ as rt}from"./chunk-KEUXMURM.mjs";import{A as Me,B as Ot,C as Ft,D as Ee,a as ae,b as it,d as ye,e as pe,f as ge,g as xe,h as xt,i as be,o as Te,p as $t,q as Yt,r as It,s as Lt,t as At,u as ve,v as we,w as _e,x as De,y as Se,z as Ce}from"./chunk-LIEV3EAG.mjs";import{a,b as wt,d as ot}from"./chunk-AQ6EADP3.mjs";var Ie=wt((Vt,Pt)=>{"use strict";(function(t,e){typeof Vt=="object"&&typeof Pt<"u"?Pt.exports=e():typeof define=="function"&&define.amd?define(e):(t=typeof globalThis<"u"?globalThis:t||self).dayjs_plugin_isoWeek=e()})(Vt,(function(){"use strict";var t="day";return function(e,r,i){var s=a(function(w){return w.add(4-w.isoWeekday(),t)},"a"),f=r.prototype;f.isoWeekYear=function(){return s(this).year()},f.isoWeek=function(w){if(!this.$utils().u(w))return this.add(7*(w-this.isoWeek()),t);var D,N,Y,R,z=s(this),H=(D=this.isoWeekYear(),N=this.$u,Y=(N?i.utc:i)().year(D).startOf("year"),R=4-Y.isoWeekday(),Y.isoWeekday()>4&&(R+=7),Y.add(R,t));return z.diff(H,"week")+1},f.isoWeekday=function(w){return this.$utils().u(w)?this.day()||7:this.day(this.day()%7?w:w-7)};var g=f.startOf;f.startOf=function(w,D){var N=this.$utils(),Y=!!N.u(D)||D;return N.p(w)==="isoweek"?Y?this.date(this.date()-(this.isoWeekday()-1)).startOf("day"):this.date(this.date()-1-(this.isoWeekday()-1)+7).endOf("day"):g.bind(this)(w,D)}}}))});var Le=wt((Nt,zt)=>{"use strict";(function(t,e){typeof Nt=="object"&&typeof zt<"u"?zt.exports=e():typeof define=="function"&&define.amd?define(e):(t=typeof globalThis<"u"?globalThis:t||self).dayjs_plugin_customParseFormat=e()})(Nt,(function(){"use strict";var t={LTS:"h:mm:ss A",LT:"h:mm A",L:"MM/DD/YYYY",LL:"MMMM D, YYYY",LLL:"MMMM D, YYYY h:mm A",LLLL:"dddd, MMMM D, YYYY h:mm A"},e=/(\[[^[]*\])|([-_:/.,()\s]+)|(A|a|Q|YYYY|YY?|ww?|MM?M?M?|Do|DD?|hh?|HH?|mm?|ss?|S{1,3}|z|ZZ?)/g,r=/\d/,i=/\d\d/,s=/\d\d?/,f=/\d*[^-_:/,()\s\d]+/,g={},w=a(function(v){return(v=+v)+(v>68?1900:2e3)},"a"),D=a(function(v){return function(_){this[v]=+_}},"f"),N=[/[+-]\d\d:?(\d\d)?|Z/,function(v){(this.zone||(this.zone={})).offset=(function(_){if(!_||_==="Z")return 0;var T=_.match(/([+-]|\d\d)/g),A=60*T[1]+(+T[2]||0);return A===0?0:T[0]==="+"?-A:A})(v)}],Y=a(function(v){var _=g[v];return _&&(_.indexOf?_:_.s.concat(_.f))},"u"),R=a(function(v,_){var T,A=g.meridiem;if(A){for(var X=1;X<=24;X+=1)if(v.indexOf(A(X,0,_))>-1){T=X>12;break}}else T=v===(_?"pm":"PM");return T},"d"),z={A:[f,function(v){this.afternoon=R(v,!1)}],a:[f,function(v){this.afternoon=R(v,!0)}],Q:[r,function(v){this.month=3*(v-1)+1}],S:[r,function(v){this.milliseconds=100*+v}],SS:[i,function(v){this.milliseconds=10*+v}],SSS:[/\d{3}/,function(v){this.milliseconds=+v}],s:[s,D("seconds")],ss:[s,D("seconds")],m:[s,D("minutes")],mm:[s,D("minutes")],H:[s,D("hours")],h:[s,D("hours")],HH:[s,D("hours")],hh:[s,D("hours")],D:[s,D("day")],DD:[i,D("day")],Do:[f,function(v){var _=g.ordinal,T=v.match(/\d+/);if(this.day=T[0],_)for(var A=1;A<=31;A+=1)_(A).replace(/\[|\]/g,"")===v&&(this.day=A)}],w:[s,D("week")],ww:[i,D("week")],M:[s,D("month")],MM:[i,D("month")],MMM:[f,function(v){var _=Y("months"),T=(Y("monthsShort")||_.map((function(A){return A.slice(0,3)}))).indexOf(v)+1;if(T<1)throw new Error;this.month=T%12||T}],MMMM:[f,function(v){var _=Y("months").indexOf(v)+1;if(_<1)throw new Error;this.month=_%12||_}],Y:[/[+-]?\d+/,D("year")],YY:[i,function(v){this.year=w(v)}],YYYY:[/\d{4}/,D("year")],Z:N,ZZ:N};function H(v){var _,T;_=v,T=g&&g.formats;for(var A=(v=_.replace(/(\[[^\]]+])|(LTS?|l{1,4}|L{1,4})/g,(function(W,O,k){var p=k&&k.toUpperCase();return O||T[k]||t[k]||T[p].replace(/(\[[^\]]+])|(MMMM|MM|DD|dddd)/g,(function(x,b,o){return b||o.slice(1)}))}))).match(e),X=A.length,U=0;U<X;U+=1){var I=A[U],y=z[I],m=y&&y[0],$=y&&y[1];A[U]=$?{regex:m,parser:$}:I.replace(/^\[|\]$/g,"")}return function(W){for(var O={},k=0,p=0;k<X;k+=1){var x=A[k];if(typeof x=="string")p+=x.length;else{var b=x.regex,o=x.parser,h=W.slice(p),d=b.exec(h)[0];o.call(O,d),W=W.replace(d,"")}}return(function(u){var C=u.afternoon;if(C!==void 0){var n=u.hours;C?n<12&&(u.hours+=12):n===12&&(u.hours=0),delete u.afternoon}})(O),O}}return a(H,"l"),function(v,_,T){T.p.customParseFormat=!0,v&&v.parseTwoDigitYear&&(w=v.parseTwoDigitYear);var A=_.prototype,X=A.parse;A.parse=function(U){var I=U.date,y=U.utc,m=U.args;this.$u=y;var $=m[1];if(typeof $=="string"){var W=m[2]===!0,O=m[3]===!0,k=W||O,p=m[2];O&&(p=m[2]),g=this.$locale(),!W&&p&&(g=T.Ls[p]),this.$d=(function(h,d,u,C){try{if(["x","X"].indexOf(d)>-1)return new Date((d==="X"?1e3:1)*h);var n=H(d)(h),M=n.year,l=n.month,j=n.day,c=n.hours,S=n.minutes,E=n.seconds,V=n.milliseconds,P=n.zone,L=n.week,F=new Date,tt=j||(M||l?1:F.getDate()),et=M||F.getFullYear(),lt=0;M&&!l||(lt=l>0?l-1:F.getMonth());var pt,gt=c||0,B=S||0,at=E||0,K=V||0;return P?new Date(Date.UTC(et,lt,tt,gt,B,at,K+60*P.offset*1e3)):u?new Date(Date.UTC(et,lt,tt,gt,B,at,K)):(pt=new Date(et,lt,tt,gt,B,at,K),L&&(pt=C(pt).week(L).toDate()),pt)}catch{return new Date("")}})(I,$,y,T),this.init(),p&&p!==!0&&(this.$L=this.locale(p).$L),k&&I!=this.format($)&&(this.$d=new Date("")),g={}}else if($ instanceof Array)for(var x=$.length,b=1;b<=x;b+=1){m[1]=$[b-1];var o=T.apply(this,m);if(o.isValid()){this.$d=o.$d,this.$L=o.$L,this.init();break}b===x&&(this.$d=new Date(""))}else X.call(this,U)}}}))});var Ae=wt((Rt,Ht)=>{"use strict";(function(t,e){typeof Rt=="object"&&typeof Ht<"u"?Ht.exports=e():typeof define=="function"&&define.amd?define(e):(t=typeof globalThis<"u"?globalThis:t||self).dayjs_plugin_advancedFormat=e()})(Rt,(function(){"use strict";return function(t,e){var r=e.prototype,i=r.format;r.format=function(s){var f=this,g=this.$locale();if(!this.isValid())return i.bind(this)(s);var w=this.$utils(),D=(s||"YYYY-MM-DDTHH:mm:ssZ").replace(/\[([^\]]+)]|Q|wo|ww|w|WW|W|zzz|z|gggg|GGGG|Do|X|x|k{1,2}|S/g,(function(N){switch(N){case"Q":return Math.ceil((f.$M+1)/3);case"Do":return g.ordinal(f.$D);case"gggg":return f.weekYear();case"GGGG":return f.isoWeekYear();case"wo":return g.ordinal(f.week(),"W");case"w":case"ww":return w.s(f.week(),N==="w"?1:2,"0");case"W":case"WW":return w.s(f.isoWeek(),N==="W"?1:2,"0");case"k":case"kk":return w.s(String(f.$H===0?24:f.$H),N==="k"?1:2,"0");case"X":return Math.floor(f.$d.getTime()/1e3);case"x":return f.$d.getTime();case"z":return"["+f.offsetName()+"]";case"zzz":return"["+f.offsetName("long")+"]";default:return N}}));return i.bind(this)(D)}}}))});var Ke=wt((ne,ie)=>{"use strict";(function(t,e){typeof ne=="object"&&typeof ie<"u"?ie.exports=e():typeof define=="function"&&define.amd?define(e):(t=typeof globalThis<"u"?globalThis:t||self).dayjs_plugin_duration=e()})(ne,(function(){"use strict";var t,e,r=1e3,i=6e4,s=36e5,f=864e5,g=/\[([^\]]+)]|Y{1,4}|M{1,4}|D{1,2}|d{1,4}|H{1,2}|h{1,2}|a|A|m{1,2}|s{1,2}|Z{1,2}|SSS/g,w=31536e6,D=2628e6,N=/^(-|\+)?P(?:([-+]?[0-9,.]*)Y)?(?:([-+]?[0-9,.]*)M)?(?:([-+]?[0-9,.]*)W)?(?:([-+]?[0-9,.]*)D)?(?:T(?:([-+]?[0-9,.]*)H)?(?:([-+]?[0-9,.]*)M)?(?:([-+]?[0-9,.]*)S)?)?$/,Y={years:w,months:D,days:f,hours:s,minutes:i,seconds:r,milliseconds:1,weeks:6048e5},R=a(function(I){return I instanceof X},"c"),z=a(function(I,y,m){return new X(I,m,y.$l)},"f"),H=a(function(I){return e.p(I)+"s"},"m"),v=a(function(I){return I<0},"l"),_=a(function(I){return v(I)?Math.ceil(I):Math.floor(I)},"$"),T=a(function(I){return Math.abs(I)},"y"),A=a(function(I,y){return I?v(I)?{negative:!0,format:""+T(I)+y}:{negative:!1,format:""+I+y}:{negative:!1,format:""}},"v"),X=(function(){function I(m,$,W){var O=this;if(this.$d={},this.$l=W,m===void 0&&(this.$ms=0,this.parseFromMilliseconds()),$)return z(m*Y[H($)],this);if(typeof m=="number")return this.$ms=m,this.parseFromMilliseconds(),this;if(typeof m=="object")return Object.keys(m).forEach((function(x){O.$d[H(x)]=m[x]})),this.calMilliseconds(),this;if(typeof m=="string"){var k=m.match(N);if(k){var p=k.slice(2).map((function(x){return x!=null?Number(x):0}));return this.$d.years=p[0],this.$d.months=p[1],this.$d.weeks=p[2],this.$d.days=p[3],this.$d.hours=p[4],this.$d.minutes=p[5],this.$d.seconds=p[6],this.calMilliseconds(),this}}return this}a(I,"l");var y=I.prototype;return y.calMilliseconds=function(){var m=this;this.$ms=Object.keys(this.$d).reduce((function($,W){return $+(m.$d[W]||0)*Y[W]}),0)},y.parseFromMilliseconds=function(){var m=this.$ms;this.$d.years=_(m/w),m%=w,this.$d.months=_(m/D),m%=D,this.$d.days=_(m/f),m%=f,this.$d.hours=_(m/s),m%=s,this.$d.minutes=_(m/i),m%=i,this.$d.seconds=_(m/r),m%=r,this.$d.milliseconds=m},y.toISOString=function(){var m=A(this.$d.years,"Y"),$=A(this.$d.months,"M"),W=+this.$d.days||0;this.$d.weeks&&(W+=7*this.$d.weeks);var O=A(W,"D"),k=A(this.$d.hours,"H"),p=A(this.$d.minutes,"M"),x=this.$d.seconds||0;this.$d.milliseconds&&(x+=this.$d.milliseconds/1e3,x=Math.round(1e3*x)/1e3);var b=A(x,"S"),o=m.negative||$.negative||O.negative||k.negative||p.negative||b.negative,h=k.format||p.format||b.format?"T":"",d=(o?"-":"")+"P"+m.format+$.format+O.format+h+k.format+p.format+b.format;return d==="P"||d==="-P"?"P0D":d},y.toJSON=function(){return this.toISOString()},y.format=function(m){var $=m||"YYYY-MM-DDTHH:mm:ss",W={Y:this.$d.years,YY:e.s(this.$d.years,2,"0"),YYYY:e.s(this.$d.years,4,"0"),M:this.$d.months,MM:e.s(this.$d.months,2,"0"),D:this.$d.days,DD:e.s(this.$d.days,2,"0"),H:this.$d.hours,HH:e.s(this.$d.hours,2,"0"),m:this.$d.minutes,mm:e.s(this.$d.minutes,2,"0"),s:this.$d.seconds,ss:e.s(this.$d.seconds,2,"0"),SSS:e.s(this.$d.milliseconds,3,"0")};return $.replace(g,(function(O,k){return k||String(W[O])}))},y.as=function(m){return this.$ms/Y[H(m)]},y.get=function(m){var $=this.$ms,W=H(m);return W==="milliseconds"?$%=1e3:$=W==="weeks"?_($/Y[W]):this.$d[W],$||0},y.add=function(m,$,W){var O;return O=$?m*Y[H($)]:R(m)?m.$ms:z(m,this).$ms,z(this.$ms+O*(W?-1:1),this)},y.subtract=function(m,$){return this.add(m,$,!0)},y.locale=function(m){var $=this.clone();return $.$l=m,$},y.clone=function(){return z(this.$ms,this)},y.humanize=function(m){return t().add(this.$ms,"ms").locale(this.$l).fromNow(!m)},y.valueOf=function(){return this.asMilliseconds()},y.milliseconds=function(){return this.get("milliseconds")},y.asMilliseconds=function(){return this.as("milliseconds")},y.seconds=function(){return this.get("seconds")},y.asSeconds=function(){return this.as("seconds")},y.minutes=function(){return this.get("minutes")},y.asMinutes=function(){return this.as("minutes")},y.hours=function(){return this.get("hours")},y.asHours=function(){return this.as("hours")},y.days=function(){return this.get("days")},y.asDays=function(){return this.as("days")},y.weeks=function(){return this.get("weeks")},y.asWeeks=function(){return this.as("weeks")},y.months=function(){return this.get("months")},y.asMonths=function(){return this.as("months")},y.years=function(){return this.get("years")},y.asYears=function(){return this.as("years")},I})(),U=a(function(I,y,m){return I.add(y.years()*m,"y").add(y.months()*m,"M").add(y.days()*m,"d").add(y.hours()*m,"h").add(y.minutes()*m,"m").add(y.seconds()*m,"s").add(y.milliseconds()*m,"ms")},"p");return function(I,y,m){t=m,e=m().$utils(),m.duration=function(O,k){var p=m.locale();return z(O,{$l:p},k)},m.isDuration=R;var $=y.prototype.add,W=y.prototype.subtract;y.prototype.add=function(O,k){return R(O)?U(this,O,1):$.bind(this)(O,k)},y.prototype.subtract=function(O,k){return R(O)?U(this,O,-1):W.bind(this)(O,k)}}}))});var Wt=(function(){var t=a(function(b,o,h,d){for(h=h||{},d=b.length;d--;h[b[d]]=o);return h},"o"),e=[6,8,10,12,13,14,15,16,17,18,20,21,22,23,24,25,26,27,28,29,30,31,33,35,36,38,40],r=[1,26],i=[1,27],s=[1,28],f=[1,29],g=[1,30],w=[1,31],D=[1,32],N=[1,33],Y=[1,34],R=[1,9],z=[1,10],H=[1,11],v=[1,12],_=[1,13],T=[1,14],A=[1,15],X=[1,16],U=[1,19],I=[1,20],y=[1,21],m=[1,22],$=[1,23],W=[1,25],O=[1,35],k={trace:a(function(){},"trace"),yy:{},symbols_:{error:2,start:3,gantt:4,document:5,EOF:6,line:7,SPACE:8,statement:9,NL:10,weekday:11,weekday_monday:12,weekday_tuesday:13,weekday_wednesday:14,weekday_thursday:15,weekday_friday:16,weekday_saturday:17,weekday_sunday:18,weekend:19,weekend_friday:20,weekend_saturday:21,dateFormat:22,inclusiveEndDates:23,topAxis:24,axisFormat:25,tickInterval:26,excludes:27,includes:28,todayMarker:29,title:30,acc_title:31,acc_title_value:32,acc_descr:33,acc_descr_value:34,acc_descr_multiline_value:35,section:36,clickStatement:37,taskTxt:38,taskData:39,click:40,callbackname:41,callbackargs:42,href:43,clickStatementDebug:44,$accept:0,$end:1},terminals_:{2:"error",4:"gantt",6:"EOF",8:"SPACE",10:"NL",12:"weekday_monday",13:"weekday_tuesday",14:"weekday_wednesday",15:"weekday_thursday",16:"weekday_friday",17:"weekday_saturday",18:"weekday_sunday",20:"weekend_friday",21:"weekend_saturday",22:"dateFormat",23:"inclusiveEndDates",24:"topAxis",25:"axisFormat",26:"tickInterval",27:"excludes",28:"includes",29:"todayMarker",30:"title",31:"acc_title",32:"acc_title_value",33:"acc_descr",34:"acc_descr_value",35:"acc_descr_multiline_value",36:"section",38:"taskTxt",39:"taskData",40:"click",41:"callbackname",42:"callbackargs",43:"href"},productions_:[0,[3,3],[5,0],[5,2],[7,2],[7,1],[7,1],[7,1],[11,1],[11,1],[11,1],[11,1],[11,1],[11,1],[11,1],[19,1],[19,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,1],[9,2],[9,2],[9,1],[9,1],[9,1],[9,2],[37,2],[37,3],[37,3],[37,4],[37,3],[37,4],[37,2],[44,2],[44,3],[44,3],[44,4],[44,3],[44,4],[44,2]],performAction:a(function(o,h,d,u,C,n,M){var l=n.length-1;switch(C){case 1:return n[l-1];case 2:this.$=[];break;case 3:n[l-1].push(n[l]),this.$=n[l-1];break;case 4:case 5:this.$=n[l];break;case 6:case 7:this.$=[];break;case 8:u.setWeekday("monday");break;case 9:u.setWeekday("tuesday");break;case 10:u.setWeekday("wednesday");break;case 11:u.setWeekday("thursday");break;case 12:u.setWeekday("friday");break;case 13:u.setWeekday("saturday");break;case 14:u.setWeekday("sunday");break;case 15:u.setWeekend("friday");break;case 16:u.setWeekend("saturday");break;case 17:u.setDateFormat(n[l].substr(11)),this.$=n[l].substr(11);break;case 18:u.enableInclusiveEndDates(),this.$=n[l].substr(18);break;case 19:u.TopAxis(),this.$=n[l].substr(8);break;case 20:u.setAxisFormat(n[l].substr(11)),this.$=n[l].substr(11);break;case 21:u.setTickInterval(n[l].substr(13)),this.$=n[l].substr(13);break;case 22:u.setExcludes(n[l].substr(9)),this.$=n[l].substr(9);break;case 23:u.setIncludes(n[l].substr(9)),this.$=n[l].substr(9);break;case 24:u.setTodayMarker(n[l].substr(12)),this.$=n[l].substr(12);break;case 27:u.setDiagramTitle(n[l].substr(6)),this.$=n[l].substr(6);break;case 28:this.$=n[l].trim(),u.setAccTitle(this.$);break;case 29:case 30:this.$=n[l].trim(),u.setAccDescription(this.$);break;case 31:u.addSection(n[l].substr(8)),this.$=n[l].substr(8);break;case 33:u.addTask(n[l-1],n[l]),this.$="task";break;case 34:this.$=n[l-1],u.setClickEvent(n[l-1],n[l],null);break;case 35:this.$=n[l-2],u.setClickEvent(n[l-2],n[l-1],n[l]);break;case 36:this.$=n[l-2],u.setClickEvent(n[l-2],n[l-1],null),u.setLink(n[l-2],n[l]);break;case 37:this.$=n[l-3],u.setClickEvent(n[l-3],n[l-2],n[l-1]),u.setLink(n[l-3],n[l]);break;case 38:this.$=n[l-2],u.setClickEvent(n[l-2],n[l],null),u.setLink(n[l-2],n[l-1]);break;case 39:this.$=n[l-3],u.setClickEvent(n[l-3],n[l-1],n[l]),u.setLink(n[l-3],n[l-2]);break;case 40:this.$=n[l-1],u.setLink(n[l-1],n[l]);break;case 41:case 47:this.$=n[l-1]+" "+n[l];break;case 42:case 43:case 45:this.$=n[l-2]+" "+n[l-1]+" "+n[l];break;case 44:case 46:this.$=n[l-3]+" "+n[l-2]+" "+n[l-1]+" "+n[l];break}},"anonymous"),table:[{3:1,4:[1,2]},{1:[3]},t(e,[2,2],{5:3}),{6:[1,4],7:5,8:[1,6],9:7,10:[1,8],11:17,12:r,13:i,14:s,15:f,16:g,17:w,18:D,19:18,20:N,21:Y,22:R,23:z,24:H,25:v,26:_,27:T,28:A,29:X,30:U,31:I,33:y,35:m,36:$,37:24,38:W,40:O},t(e,[2,7],{1:[2,1]}),t(e,[2,3]),{9:36,11:17,12:r,13:i,14:s,15:f,16:g,17:w,18:D,19:18,20:N,21:Y,22:R,23:z,24:H,25:v,26:_,27:T,28:A,29:X,30:U,31:I,33:y,35:m,36:$,37:24,38:W,40:O},t(e,[2,5]),t(e,[2,6]),t(e,[2,17]),t(e,[2,18]),t(e,[2,19]),t(e,[2,20]),t(e,[2,21]),t(e,[2,22]),t(e,[2,23]),t(e,[2,24]),t(e,[2,25]),t(e,[2,26]),t(e,[2,27]),{32:[1,37]},{34:[1,38]},t(e,[2,30]),t(e,[2,31]),t(e,[2,32]),{39:[1,39]},t(e,[2,8]),t(e,[2,9]),t(e,[2,10]),t(e,[2,11]),t(e,[2,12]),t(e,[2,13]),t(e,[2,14]),t(e,[2,15]),t(e,[2,16]),{41:[1,40],43:[1,41]},t(e,[2,4]),t(e,[2,28]),t(e,[2,29]),t(e,[2,33]),t(e,[2,34],{42:[1,42],43:[1,43]}),t(e,[2,40],{41:[1,44]}),t(e,[2,35],{43:[1,45]}),t(e,[2,36]),t(e,[2,38],{42:[1,46]}),t(e,[2,37]),t(e,[2,39])],defaultActions:{},parseError:a(function(o,h){if(h.recoverable)this.trace(o);else{var d=new Error(o);throw d.hash=h,d}},"parseError"),parse:a(function(o){var h=this,d=[0],u=[],C=[null],n=[],M=this.table,l="",j=0,c=0,S=0,E=2,V=1,P=n.slice.call(arguments,1),L=Object.create(this.lexer),F={yy:{}};for(var tt in this.yy)Object.prototype.hasOwnProperty.call(this.yy,tt)&&(F.yy[tt]=this.yy[tt]);L.setInput(o,F.yy),F.yy.lexer=L,F.yy.parser=this,typeof L.yylloc>"u"&&(L.yylloc={});var et=L.yylloc;n.push(et);var lt=L.options&&L.options.ranges;typeof F.yy.parseError=="function"?this.parseError=F.yy.parseError:this.parseError=Object.getPrototypeOf(this).parseError;function pt(q){d.length=d.length-2*q,C.length=C.length-q,n.length=n.length-q}a(pt,"popStack");function gt(){var q;return q=u.pop()||L.lex()||V,typeof q!="number"&&(q instanceof Array&&(u=q,q=u.pop()),q=h.symbols_[q]||q),q}a(gt,"lex");for(var B,at,K,Z,Bn,Mt,ut={},Tt,nt,re,vt;;){if(K=d[d.length-1],this.defaultActions[K]?Z=this.defaultActions[K]:((B===null||typeof B>"u")&&(B=gt()),Z=M[K]&&M[K][B]),typeof Z>"u"||!Z.length||!Z[0]){var Et="";vt=[];for(Tt in M[K])this.terminals_[Tt]&&Tt>E&&vt.push("'"+this.terminals_[Tt]+"'");L.showPosition?Et="Parse error on line "+(j+1)+`:
`+L.showPosition()+`
Expecting `+vt.join(", ")+", got '"+(this.terminals_[B]||B)+"'":Et="Parse error on line "+(j+1)+": Unexpected "+(B==V?"end of input":"'"+(this.terminals_[B]||B)+"'"),this.parseError(Et,{text:L.match,token:this.terminals_[B]||B,line:L.yylineno,loc:et,expected:vt})}if(Z[0]instanceof Array&&Z.length>1)throw new Error("Parse Error: multiple actions possible at state: "+K+", token: "+B);switch(Z[0]){case 1:d.push(B),C.push(L.yytext),n.push(L.yylloc),d.push(Z[1]),B=null,at?(B=at,at=null):(c=L.yyleng,l=L.yytext,j=L.yylineno,et=L.yylloc,S>0&&S--);break;case 2:if(nt=this.productions_[Z[1]][1],ut.$=C[C.length-nt],ut._$={first_line:n[n.length-(nt||1)].first_line,last_line:n[n.length-1].last_line,first_column:n[n.length-(nt||1)].first_column,last_column:n[n.length-1].last_column},lt&&(ut._$.range=[n[n.length-(nt||1)].range[0],n[n.length-1].range[1]]),Mt=this.performAction.apply(ut,[l,c,j,F.yy,Z[1],C,n].concat(P)),typeof Mt<"u")return Mt;nt&&(d=d.slice(0,-1*nt*2),C=C.slice(0,-1*nt),n=n.slice(0,-1*nt)),d.push(this.productions_[Z[1]][0]),C.push(ut.$),n.push(ut._$),re=M[d[d.length-2]][d[d.length-1]],d.push(re);break;case 3:return!0}}return!0},"parse")},p=(function(){var b={EOF:1,parseError:a(function(h,d){if(this.yy.parser)this.yy.parser.parseError(h,d);else throw new Error(h)},"parseError"),setInput:a(function(o,h){return this.yy=h||this.yy||{},this._input=o,this._more=this._backtrack=this.done=!1,this.yylineno=this.yyleng=0,this.yytext=this.matched=this.match="",this.conditionStack=["INITIAL"],this.yylloc={first_line:1,first_column:0,last_line:1,last_column:0},this.options.ranges&&(this.yylloc.range=[0,0]),this.offset=0,this},"setInput"),input:a(function(){var o=this._input[0];this.yytext+=o,this.yyleng++,this.offset++,this.match+=o,this.matched+=o;var h=o.match(/(?:\r\n?|\n).*/g);return h?(this.yylineno++,this.yylloc.last_line++):this.yylloc.last_column++,this.options.ranges&&this.yylloc.range[1]++,this._input=this._input.slice(1),o},"input"),unput:a(function(o){var h=o.length,d=o.split(/(?:\r\n?|\n)/g);this._input=o+this._input,this.yytext=this.yytext.substr(0,this.yytext.length-h),this.offset-=h;var u=this.match.split(/(?:\r\n?|\n)/g);this.match=this.match.substr(0,this.match.length-1),this.matched=this.matched.substr(0,this.matched.length-1),d.length-1&&(this.yylineno-=d.length-1);var C=this.yylloc.range;return this.yylloc={first_line:this.yylloc.first_line,last_line:this.yylineno+1,first_column:this.yylloc.first_column,last_column:d?(d.length===u.length?this.yylloc.first_column:0)+u[u.length-d.length].length-d[0].length:this.yylloc.first_column-h},this.options.ranges&&(this.yylloc.range=[C[0],C[0]+this.yyleng-h]),this.yyleng=this.yytext.length,this},"unput"),more:a(function(){return this._more=!0,this},"more"),reject:a(function(){if(this.options.backtrack_lexer)this._backtrack=!0;else return this.parseError("Lexical error on line "+(this.yylineno+1)+`. You can only invoke reject() in the lexer when the lexer is of the backtracking persuasion (options.backtrack_lexer = true).
`+this.showPosition(),{text:"",token:null,line:this.yylineno});return this},"reject"),less:a(function(o){this.unput(this.match.slice(o))},"less"),pastInput:a(function(){var o=this.matched.substr(0,this.matched.length-this.match.length);return(o.length>20?"...":"")+o.substr(-20).replace(/\n/g,"")},"pastInput"),upcomingInput:a(function(){var o=this.match;return o.length<20&&(o+=this._input.substr(0,20-o.length)),(o.substr(0,20)+(o.length>20?"...":"")).replace(/\n/g,"")},"upcomingInput"),showPosition:a(function(){var o=this.pastInput(),h=new Array(o.length+1).join("-");return o+this.upcomingInput()+`
`+h+"^"},"showPosition"),test_match:a(function(o,h){var d,u,C;if(this.options.backtrack_lexer&&(C={yylineno:this.yylineno,yylloc:{first_line:this.yylloc.first_line,last_line:this.last_line,first_column:this.yylloc.first_column,last_column:this.yylloc.last_column},yytext:this.yytext,match:this.match,matches:this.matches,matched:this.matched,yyleng:this.yyleng,offset:this.offset,_more:this._more,_input:this._input,yy:this.yy,conditionStack:this.conditionStack.slice(0),done:this.done},this.options.ranges&&(C.yylloc.range=this.yylloc.range.slice(0))),u=o[0].match(/(?:\r\n?|\n).*/g),u&&(this.yylineno+=u.length),this.yylloc={first_line:this.yylloc.last_line,last_line:this.yylineno+1,first_column:this.yylloc.last_column,last_column:u?u[u.length-1].length-u[u.length-1].match(/\r?\n?/)[0].length:this.yylloc.last_column+o[0].length},this.yytext+=o[0],this.match+=o[0],this.matches=o,this.yyleng=this.yytext.length,this.options.ranges&&(this.yylloc.range=[this.offset,this.offset+=this.yyleng]),this._more=!1,this._backtrack=!1,this._input=this._input.slice(o[0].length),this.matched+=o[0],d=this.performAction.call(this,this.yy,this,h,this.conditionStack[this.conditionStack.length-1]),this.done&&this._input&&(this.done=!1),d)return d;if(this._backtrack){for(var n in C)this[n]=C[n];return!1}return!1},"test_match"),next:a(function(){if(this.done)return this.EOF;this._input||(this.done=!0);var o,h,d,u;this._more||(this.yytext="",this.match="");for(var C=this._currentRules(),n=0;n<C.length;n++)if(d=this._input.match(this.rules[C[n]]),d&&(!h||d[0].length>h[0].length)){if(h=d,u=n,this.options.backtrack_lexer){if(o=this.test_match(d,C[n]),o!==!1)return o;if(this._backtrack){h=!1;continue}else return!1}else if(!this.options.flex)break}return h?(o=this.test_match(h,C[u]),o!==!1?o:!1):this._input===""?this.EOF:this.parseError("Lexical error on line "+(this.yylineno+1)+`. Unrecognized text.
`+this.showPosition(),{text:"",token:null,line:this.yylineno})},"next"),lex:a(function(){var h=this.next();return h||this.lex()},"lex"),begin:a(function(h){this.conditionStack.push(h)},"begin"),popState:a(function(){var h=this.conditionStack.length-1;return h>0?this.conditionStack.pop():this.conditionStack[0]},"popState"),_currentRules:a(function(){return this.conditionStack.length&&this.conditionStack[this.conditionStack.length-1]?this.conditions[this.conditionStack[this.conditionStack.length-1]].rules:this.conditions.INITIAL.rules},"_currentRules"),topState:a(function(h){return h=this.conditionStack.length-1-Math.abs(h||0),h>=0?this.conditionStack[h]:"INITIAL"},"topState"),pushState:a(function(h){this.begin(h)},"pushState"),stateStackSize:a(function(){return this.conditionStack.length},"stateStackSize"),options:{"case-insensitive":!0},performAction:a(function(h,d,u,C){var n=C;switch(u){case 0:return this.begin("open_directive"),"open_directive";break;case 1:return this.begin("acc_title"),31;break;case 2:return this.popState(),"acc_title_value";break;case 3:return this.begin("acc_descr"),33;break;case 4:return this.popState(),"acc_descr_value";break;case 5:this.begin("acc_descr_multiline");break;case 6:this.popState();break;case 7:return"acc_descr_multiline_value";case 8:break;case 9:break;case 10:break;case 11:return 10;case 12:break;case 13:break;case 14:this.begin("href");break;case 15:this.popState();break;case 16:return 43;case 17:this.begin("callbackname");break;case 18:this.popState();break;case 19:this.popState(),this.begin("callbackargs");break;case 20:return 41;case 21:this.popState();break;case 22:return 42;case 23:this.begin("click");break;case 24:this.popState();break;case 25:return 40;case 26:return 4;case 27:return 22;case 28:return 23;case 29:return 24;case 30:return 25;case 31:return 26;case 32:return 28;case 33:return 27;case 34:return 29;case 35:return 12;case 36:return 13;case 37:return 14;case 38:return 15;case 39:return 16;case 40:return 17;case 41:return 18;case 42:return 20;case 43:return 21;case 44:return"date";case 45:return 30;case 46:return"accDescription";case 47:return 36;case 48:return 38;case 49:return 39;case 50:return":";case 51:return 6;case 52:return"INVALID"}},"anonymous"),rules:[/^(?:%%\{)/i,/^(?:accTitle\s*:\s*)/i,/^(?:(?!\n||)*[^\n]*)/i,/^(?:accDescr\s*:\s*)/i,/^(?:(?!\n||)*[^\n]*)/i,/^(?:accDescr\s*\{\s*)/i,/^(?:[\}])/i,/^(?:[^\}]*)/i,/^(?:%%(?!\{)*[^\n]*)/i,/^(?:[^\}]%%*[^\n]*)/i,/^(?:%%*[^\n]*[\n]*)/i,/^(?:[\n]+)/i,/^(?:\s+)/i,/^(?:%[^\n]*)/i,/^(?:href[\s]+["])/i,/^(?:["])/i,/^(?:[^"]*)/i,/^(?:call[\s]+)/i,/^(?:\([\s]*\))/i,/^(?:\()/i,/^(?:[^(]*)/i,/^(?:\))/i,/^(?:[^)]*)/i,/^(?:click[\s]+)/i,/^(?:[\s\n])/i,/^(?:[^\s\n]*)/i,/^(?:gantt\b)/i,/^(?:dateFormat\s[^#\n;]+)/i,/^(?:inclusiveEndDates\b)/i,/^(?:topAxis\b)/i,/^(?:axisFormat\s[^#\n;]+)/i,/^(?:tickInterval\s[^#\n;]+)/i,/^(?:includes\s[^#\n;]+)/i,/^(?:excludes\s[^#\n;]+)/i,/^(?:todayMarker\s[^\n;]+)/i,/^(?:weekday\s+monday\b)/i,/^(?:weekday\s+tuesday\b)/i,/^(?:weekday\s+wednesday\b)/i,/^(?:weekday\s+thursday\b)/i,/^(?:weekday\s+friday\b)/i,/^(?:weekday\s+saturday\b)/i,/^(?:weekday\s+sunday\b)/i,/^(?:weekend\s+friday\b)/i,/^(?:weekend\s+saturday\b)/i,/^(?:\d\d\d\d-\d\d-\d\d\b)/i,/^(?:title\s[^\n]+)/i,/^(?:accDescription\s[^#\n;]+)/i,/^(?:section\s[^\n]+)/i,/^(?:[^:\n]+)/i,/^(?::[^#\n;]+)/i,/^(?::)/i,/^(?:$)/i,/^(?:.)/i],conditions:{acc_descr_multiline:{rules:[6,7],inclusive:!1},acc_descr:{rules:[4],inclusive:!1},acc_title:{rules:[2],inclusive:!1},callbackargs:{rules:[21,22],inclusive:!1},callbackname:{rules:[18,19,20],inclusive:!1},href:{rules:[15,16],inclusive:!1},click:{rules:[24,25],inclusive:!1},INITIAL:{rules:[0,1,3,5,8,9,10,11,12,13,14,17,23,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52],inclusive:!0}}};return b})();k.lexer=p;function x(){this.yy={}}return a(x,"Parser"),x.prototype=k,k.Parser=x,new x})();Wt.parser=Wt;var Ye=Wt;var We=ot(sn(),1),Q=ot(ae(),1),Ve=ot(Ie(),1),Pe=ot(Le(),1),Ne=ot(Ae(),1);Q.default.extend(Ve.default);Q.default.extend(Pe.default);Q.default.extend(Ne.default);var Oe={friday:5,saturday:6},J="",Xt="",Ut,Zt="",ht=[],mt=[],qt=new Map,Qt=[],St=[],kt="",Kt="",ze=["active","done","crit","milestone","vert"],Jt=[],dt="",bt=!1,te=!1,ee="sunday",Ct="saturday",jt=0,rn=a(function(){Qt=[],St=[],kt="",Jt=[],_t=0,Gt=void 0,Dt=void 0,G=[],J="",Xt="",Kt="",Ut=void 0,Zt="",ht=[],mt=[],bt=!1,te=!1,jt=0,qt=new Map,dt="",le(),ee="sunday",Ct="saturday"},"clear"),an=a(function(t){dt=t},"setDiagramId"),on=a(function(t){Xt=t},"setAxisFormat"),cn=a(function(){return Xt},"getAxisFormat"),ln=a(function(t){Ut=t},"setTickInterval"),un=a(function(){return Ut},"getTickInterval"),dn=a(function(t){Zt=t},"setTodayMarker"),fn=a(function(){return Zt},"getTodayMarker"),hn=a(function(t){J=t},"setDateFormat"),mn=a(function(){bt=!0},"enableInclusiveEndDates"),kn=a(function(){return bt},"endDatesAreInclusive"),yn=a(function(){te=!0},"enableTopAxis"),pn=a(function(){return te},"topAxisEnabled"),gn=a(function(t){Kt=t},"setDisplayMode"),xn=a(function(){return Kt},"getDisplayMode"),bn=a(function(){return J},"getDateFormat"),Re=a((t,e)=>{let r=e.toLowerCase().split(/[\s,]+/).filter(i=>i!=="");return[...new Set([...t,...r])]},"mergeTokens"),Tn=a(function(t){ht=Re(ht,t)},"setIncludes"),vn=a(function(){return ht},"getIncludes"),wn=a(function(t){mt=Re(mt,t)},"setExcludes"),_n=a(function(){return mt},"getExcludes"),Dn=a(function(){return qt},"getLinks"),Sn=a(function(t){kt=t,Qt.push(t)},"addSection"),Cn=a(function(){return Qt},"getSections"),Mn=a(function(){let t=Fe(),e=10,r=0;for(;!t&&r<e;)t=Fe(),r++;return St=G,St},"getTasks"),He=a(function(t,e,r,i){let s=t.format(e.trim()),f=t.format("YYYY-MM-DD");return i.includes(s)||i.includes(f)?!1:r.includes("weekends")&&(t.isoWeekday()===Oe[Ct]||t.isoWeekday()===Oe[Ct]+1)||r.includes(t.format("dddd").toLowerCase())?!0:r.includes(s)||r.includes(f)},"isInvalidDate"),En=a(function(t){ee=t},"setWeekday"),$n=a(function(){return ee},"getWeekday"),Yn=a(function(t){Ct=t},"setWeekend"),je=a(function(t,e,r,i){if(!r.length||t.manualEndTime)return;let s;t.startTime instanceof Date?s=(0,Q.default)(t.startTime):s=(0,Q.default)(t.startTime,e,!0),s=s.add(1,"d");let f;t.endTime instanceof Date?f=(0,Q.default)(t.endTime):f=(0,Q.default)(t.endTime,e,!0);let[g,w]=In(s,f,e,r,i);t.endTime=g.toDate(),t.renderEndTime=w},"checkTaskDates"),In=a(function(t,e,r,i,s){let f=!1,g=null,w=e.add(1e4,"d");for(;t<=e;){if(f||(g=e.toDate()),f=He(t,r,i,s),f&&(e=e.add(1,"d"),e>w))throw new Error("Failed to find a valid date that was not excluded by `excludes` after 10,000 iterations.");t=t.add(1,"d")}return[e,g]},"fixTaskDates"),Bt=a(function(t,e,r){if(r=r.trim(),a(w=>{let D=w.trim();return D==="x"||D==="X"},"isTimestampFormat")(e)&&/^\d+$/.test(r))return new Date(Number(r));let f=/^after\s+(?<ids>[\d\w- ]+)/.exec(r);if(f!==null){let w=null;for(let N of f.groups.ids.split(" ")){let Y=ct(N);Y!==void 0&&(!w||Y.endTime>w.endTime)&&(w=Y)}if(w)return w.endTime;let D=new Date;return D.setHours(0,0,0,0),D}let g=(0,Q.default)(r,e.trim(),!0);if(g.isValid())return g.toDate();{it.debug("Invalid date:"+r),it.debug("With date format:"+e.trim());let w=new Date(r);if(w===void 0||isNaN(w.getTime())||w.getFullYear()<-1e4||w.getFullYear()>1e4)throw new Error("Invalid date:"+r);return w}},"getStartDate"),Be=a(function(t){let e=/^(\d+(?:\.\d+)?)([Mdhmswy]|ms)$/.exec(t.trim());return e!==null?[Number.parseFloat(e[1]),e[2]]:[NaN,"ms"]},"parseDuration"),Ge=a(function(t,e,r,i=!1){r=r.trim();let f=/^until\s+(?<ids>[\d\w- ]+)/.exec(r);if(f!==null){let Y=null;for(let z of f.groups.ids.split(" ")){let H=ct(z);H!==void 0&&(!Y||H.startTime<Y.startTime)&&(Y=H)}if(Y)return Y.startTime;let R=new Date;return R.setHours(0,0,0,0),R}let g=(0,Q.default)(r,e.trim(),!0);if(g.isValid())return i&&(g=g.add(1,"d")),g.toDate();let w=(0,Q.default)(t),[D,N]=Be(r);if(!Number.isNaN(D)){let Y=w.add(D,N);Y.isValid()&&(w=Y)}return w.toDate()},"getEndDate"),_t=0,ft=a(function(t){return t===void 0?(_t=_t+1,"task"+_t):t},"parseId"),Ln=a(function(t,e){let r;e.substr(0,1)===":"?r=e.substr(1,e.length):r=e;let i=r.split(","),s={};Qe(i,s,ze);for(let g=0;g<i.length;g++)i[g]=i[g].trim();let f="";switch(i.length){case 1:s.id=ft(),s.startTime=t.endTime,f=i[0];break;case 2:s.id=ft(),s.startTime=Bt(void 0,J,i[0]),f=i[1];break;case 3:s.id=ft(i[0]),s.startTime=Bt(void 0,J,i[1]),f=i[2];break;default:}return f&&(s.endTime=Ge(s.startTime,J,f,bt),s.manualEndTime=(0,Q.default)(f,"YYYY-MM-DD",!0).isValid(),je(s,J,mt,ht)),s},"compileData"),An=a(function(t,e){let r;e.substr(0,1)===":"?r=e.substr(1,e.length):r=e;let i=r.split(","),s={};Qe(i,s,ze);for(let f=0;f<i.length;f++)i[f]=i[f].trim();switch(i.length){case 1:s.id=ft(),s.startTime={type:"prevTaskEnd",id:t},s.endTime={data:i[0]};break;case 2:s.id=ft(),s.startTime={type:"getStartDate",startData:i[0]},s.endTime={data:i[1]};break;case 3:s.id=ft(i[0]),s.startTime={type:"getStartDate",startData:i[1]},s.endTime={data:i[2]};break;default:}return s},"parseData"),Gt,Dt,G=[],Xe={},On=a(function(t,e){let r={section:kt,type:kt,processed:!1,manualEndTime:!1,renderEndTime:null,raw:{data:e},task:t,classes:[]},i=An(Dt,e);r.raw.startTime=i.startTime,r.raw.endTime=i.endTime,r.id=i.id,r.prevTaskId=Dt,r.active=i.active,r.done=i.done,r.crit=i.crit,r.milestone=i.milestone,r.vert=i.vert,r.vert?r.order=-1:(r.order=jt,jt++);let s=G.push(r);Dt=r.id,Xe[r.id]=s-1},"addTask"),ct=a(function(t){let e=Xe[t];return G[e]},"findTaskById"),Fn=a(function(t,e){let r={section:kt,type:kt,description:t,task:t,classes:[]},i=Ln(Gt,e);r.startTime=i.startTime,r.endTime=i.endTime,r.id=i.id,r.active=i.active,r.done=i.done,r.crit=i.crit,r.milestone=i.milestone,r.vert=i.vert,Gt=r,St.push(r)},"addTaskOrg"),Fe=a(function(){let t=a(function(r){let i=G[r],s="";switch(G[r].raw.startTime.type){case"prevTaskEnd":{let f=ct(i.prevTaskId);i.startTime=f.endTime;break}case"getStartDate":s=Bt(void 0,J,G[r].raw.startTime.startData),s&&(G[r].startTime=s);break}return G[r].startTime&&(G[r].endTime=Ge(G[r].startTime,J,G[r].raw.endTime.data,bt),G[r].endTime&&(G[r].processed=!0,G[r].manualEndTime=(0,Q.default)(G[r].raw.endTime.data,"YYYY-MM-DD",!0).isValid(),je(G[r],J,mt,ht))),G[r].processed},"compileTask"),e=!0;for(let[r,i]of G.entries())t(r),e=e&&i.processed;return e},"compileTasks"),Wn=a(function(t,e){let r=e;rt().securityLevel!=="loose"&&(r=(0,We.sanitizeUrl)(e)),t.split(",").forEach(function(i){ct(i)!==void 0&&(Ze(i,()=>{window.open(r,"_self")}),qt.set(i,r))}),Ue(t,"clickable")},"setLink"),Ue=a(function(t,e){t.split(",").forEach(function(r){let i=ct(r);i!==void 0&&i.classes.push(e)})},"setClass"),Vn=a(function(t,e,r){if(rt().securityLevel!=="loose"||e===void 0)return;let i=[];if(typeof r=="string"){i=r.split(/,(?=(?:(?:[^"]*"){2})*[^"]*$)/);for(let f=0;f<i.length;f++){let g=i[f].trim();g.startsWith('"')&&g.endsWith('"')&&(g=g.substr(1,g.length-2)),i[f]=g}}i.length===0&&i.push(t),ct(t)!==void 0&&Ze(t,()=>{$e.runFunc(e,...i)})},"setClickFun"),Ze=a(function(t,e){Jt.push(function(){let r=dt?`${dt}-${t}`:t,i=document.querySelector(`[id="${r}"]`);i!==null&&i.addEventListener("click",function(){e()})},function(){let r=dt?`${dt}-${t}`:t,i=document.querySelector(`[id="${r}-text"]`);i!==null&&i.addEventListener("click",function(){e()})})},"pushFun"),Pn=a(function(t,e,r){t.split(",").forEach(function(i){Vn(i,e,r)}),Ue(t,"clickable")},"setClickEvent"),Nn=a(function(t){Jt.forEach(function(e){e(t)})},"bindFunctions"),qe={getConfig:a(()=>rt().gantt,"getConfig"),clear:rn,setDateFormat:hn,getDateFormat:bn,enableInclusiveEndDates:mn,endDatesAreInclusive:kn,enableTopAxis:yn,topAxisEnabled:pn,setAxisFormat:on,getAxisFormat:cn,setTickInterval:ln,getTickInterval:un,setTodayMarker:dn,getTodayMarker:fn,setAccTitle:ue,getAccTitle:de,setDiagramTitle:me,getDiagramTitle:ke,setDiagramId:an,setDisplayMode:gn,getDisplayMode:xn,setAccDescription:fe,getAccDescription:he,addSection:Sn,getSections:Cn,getTasks:Mn,addTask:On,findTaskById:ct,addTaskOrg:Fn,setIncludes:Tn,getIncludes:vn,setExcludes:wn,getExcludes:_n,setClickEvent:Pn,setLink:Wn,getLinks:Dn,bindFunctions:Nn,parseDuration:Be,isInvalidDate:He,setWeekday:En,getWeekday:$n,setWeekend:Yn};function Qe(t,e,r){let i=!0;for(;i;)i=!1,r.forEach(function(s){let f="^\\s*"+s+"\\s*$",g=new RegExp(f);t[0].match(g)&&(e[s]=!0,t.shift(1),i=!0)})}a(Qe,"getTaskTags");var yt=ot(ae(),1),tn=ot(Ke(),1);yt.default.extend(tn.default);var zn=a(function(){it.debug("Something is calling, setConf, remove the call")},"setConf"),Je={monday:we,tuesday:_e,wednesday:De,thursday:Se,friday:Ce,saturday:Me,sunday:ve},Rn=a((t,e)=>{let r=[...t].map(()=>-1/0),i=[...t].sort((f,g)=>f.startTime-g.startTime||f.order-g.order),s=0;for(let f of i)for(let g=0;g<r.length;g++)if(f.startTime>=r[g]){r[g]=f.endTime,f.order=g+e,g>s&&(s=g);break}return s},"getMaxIntersections"),st,se=1e4,Hn=a(function(t,e,r,i){let s=rt().gantt;i.db.setDiagramId(e);let f=rt().securityLevel,g;f==="sandbox"&&(g=xt("#i"+e));let w=f==="sandbox"?xt(g.nodes()[0].contentDocument.body):xt("body"),D=f==="sandbox"?g.nodes()[0].contentDocument:document,N=D.getElementById(e);st=N.parentElement.offsetWidth,st===void 0&&(st=1200),s.useWidth!==void 0&&(st=s.useWidth);let Y=i.db.getTasks(),R=Y.filter(k=>!k.vert),z=[];for(let k of R)z.push(k.type);z=O(z);let H={},v=2*s.topPadding;if(i.db.getDisplayMode()==="compact"||s.displayMode==="compact"){let k={};for(let x of R)k[x.section]===void 0?k[x.section]=[x]:k[x.section].push(x);let p=0;for(let x of Object.keys(k)){let b=Rn(k[x],p)+1;p+=b,v+=b*(s.barHeight+s.barGap),H[x]=b}}else{v+=R.length*(s.barHeight+s.barGap);for(let k of z)H[k]=R.filter(p=>p.type===k).length}N.setAttribute("viewBox","0 0 "+st+" "+v);let _=w.select(`[id="${e}"]`),T=Ee().domain([pe(Y,function(k){return k.startTime}),ye(Y,function(k){return k.endTime})]).rangeRound([0,st-s.leftPadding-s.rightPadding]);function A(k,p){let x=k.startTime,b=p.startTime,o=0;return x>b?o=1:x<b&&(o=-1),o}a(A,"taskCompare"),Y.sort(A),X(Y,st,v),ce(_,v,st,s.useMaxWidth),_.append("text").text(i.db.getDiagramTitle()).attr("x",st/2).attr("y",s.titleTopMargin).attr("class","titleText");function X(k,p,x){let b=s.barHeight,o=b+s.barGap,h=s.topPadding,d=s.leftPadding,u=Te().domain([0,z.length]).range(["#00B9FA","#F95002"]).interpolate(be);I(o,h,d,p,x,k,i.db.getExcludes(),i.db.getIncludes()),m(d,h,p,x),U(k,o,h,d,b,u,p,x),$(o,h,d,b,u),W(d,h,p,x)}a(X,"makeGantt");function U(k,p,x,b,o,h,d){k.sort((c,S)=>c.vert===S.vert?0:c.vert?1:-1);let u=k.filter(c=>!c.vert),n=[...new Set(u.map(c=>c.order))].map(c=>u.find(S=>S.order===c));_.append("g").selectAll("rect").data(n).enter().append("rect").attr("x",0).attr("y",function(c,S){return S=c.order,S*p+x-2}).attr("width",function(){return d-s.rightPadding/2}).attr("height",p).attr("class",function(c){for(let[S,E]of z.entries())if(c.type===E)return"section section"+S%s.numberSectionStyles;return"section section0"}).enter();let M=_.append("g").selectAll("rect").data(k).enter(),l=i.db.getLinks();if(M.append("rect").attr("id",function(c){return e+"-"+c.id}).attr("rx",3).attr("ry",3).attr("x",function(c){return c.milestone?T(c.startTime)+b+.5*(T(c.endTime)-T(c.startTime))-.5*o:T(c.startTime)+b}).attr("y",function(c,S){return S=c.order,c.vert?s.gridLineStartPadding:S*p+x}).attr("width",function(c){return c.milestone?o:c.vert?.08*o:T(c.renderEndTime||c.endTime)-T(c.startTime)}).attr("height",function(c){return c.vert?u.length*(s.barHeight+s.barGap)+s.barHeight*2:o}).attr("transform-origin",function(c,S){return S=c.order,(T(c.startTime)+b+.5*(T(c.endTime)-T(c.startTime))).toString()+"px "+(S*p+x+.5*o).toString()+"px"}).attr("class",function(c){let S="task",E="";c.classes.length>0&&(E=c.classes.join(" "));let V=0;for(let[L,F]of z.entries())c.type===F&&(V=L%s.numberSectionStyles);let P="";return c.active?c.crit?P+=" activeCrit":P=" active":c.done?c.crit?P=" doneCrit":P=" done":c.crit&&(P+=" crit"),P.length===0&&(P=" task"),c.milestone&&(P=" milestone "+P),c.vert&&(P=" vert "+P),P+=V,P+=" "+E,S+P}),M.append("text").attr("id",function(c){return e+"-"+c.id+"-text"}).text(function(c){return c.task}).attr("font-size",s.fontSize).attr("x",function(c){let S=T(c.startTime),E=T(c.renderEndTime||c.endTime);if(c.milestone&&(S+=.5*(T(c.endTime)-T(c.startTime))-.5*o,E=S+o),c.vert)return T(c.startTime)+b;let V=this.getBBox().width;return V>E-S?E+V+1.5*s.leftPadding>d?S+b-5:E+b+5:(E-S)/2+S+b}).attr("y",function(c,S){return c.vert?s.gridLineStartPadding+u.length*(s.barHeight+s.barGap)+60:(S=c.order,S*p+s.barHeight/2+(s.fontSize/2-2)+x)}).attr("text-height",o).attr("class",function(c){let S=T(c.startTime),E=T(c.endTime);c.milestone&&(E=S+o);let V=this.getBBox().width,P="";c.classes.length>0&&(P=c.classes.join(" "));let L=0;for(let[tt,et]of z.entries())c.type===et&&(L=tt%s.numberSectionStyles);let F="";return c.active&&(c.crit?F="activeCritText"+L:F="activeText"+L),c.done?c.crit?F=F+" doneCritText"+L:F=F+" doneText"+L:c.crit&&(F=F+" critText"+L),c.milestone&&(F+=" milestoneText"),c.vert&&(F+=" vertText"),V>E-S?E+V+1.5*s.leftPadding>d?P+" taskTextOutsideLeft taskTextOutside"+L+" "+F:P+" taskTextOutsideRight taskTextOutside"+L+" "+F+" width-"+V:P+" taskText taskText"+L+" "+F+" width-"+V}),rt().securityLevel==="sandbox"){let c;c=xt("#i"+e);let S=c.nodes()[0].contentDocument;M.filter(function(E){return l.has(E.id)}).each(function(E){var V=S.querySelector("#"+CSS.escape(e+"-"+E.id)),P=S.querySelector("#"+CSS.escape(e+"-"+E.id+"-text"));let L=V.parentNode;var F=S.createElement("a");F.setAttribute("xlink:href",l.get(E.id)),F.setAttribute("target","_top"),L.appendChild(F),F.appendChild(V),F.appendChild(P)})}}a(U,"drawRects");function I(k,p,x,b,o,h,d,u){if(d.length===0&&u.length===0)return;let C,n;for(let{startTime:E,endTime:V}of h)(C===void 0||E<C)&&(C=E),(n===void 0||V>n)&&(n=V);if(!C||!n)return;if((0,yt.default)(n).diff((0,yt.default)(C),"year")>5){it.warn("The difference between the min and max time is more than 5 years. This will cause performance issues. Skipping drawing exclude days.");return}let M=i.db.getDateFormat(),l=[],j=null,c=(0,yt.default)(C);for(;c.valueOf()<=n;)i.db.isInvalidDate(c,M,d,u)?j?j.end=c:j={start:c,end:c}:j&&(l.push(j),j=null),c=c.add(1,"d");_.append("g").selectAll("rect").data(l).enter().append("rect").attr("id",E=>e+"-exclude-"+E.start.format("YYYY-MM-DD")).attr("x",E=>T(E.start.startOf("day"))+x).attr("y",s.gridLineStartPadding).attr("width",E=>T(E.end.endOf("day"))-T(E.start.startOf("day"))).attr("height",o-p-s.gridLineStartPadding).attr("transform-origin",function(E,V){return(T(E.start)+x+.5*(T(E.end)-T(E.start))).toString()+"px "+(V*k+.5*o).toString()+"px"}).attr("class","exclude-range")}a(I,"drawExcludeDays");function y(k,p,x,b){if(x<=0||k>p)return 1/0;let o=p-k,h=yt.default.duration({[b??"day"]:x}).asMilliseconds();return h<=0?1/0:Math.ceil(o/h)}a(y,"getEstimatedTickCount");function m(k,p,x,b){let o=i.db.getDateFormat(),h=i.db.getAxisFormat(),d;h?d=h:o==="D"?d="%d":d=s.axisFormat??"%Y-%m-%d";let u=xe(T).tickSize(-b+p+s.gridLineStartPadding).tickFormat(Ft(d)),n=/^([1-9]\d*)(millisecond|second|minute|hour|day|week|month)$/.exec(i.db.getTickInterval()||s.tickInterval);if(n!==null){let M=parseInt(n[1],10);if(isNaN(M)||M<=0)it.warn(`Invalid tick interval value: "${n[1]}". Skipping custom tick interval.`);else{let l=n[2],j=i.db.getWeekday()||s.weekday,c=T.domain(),S=c[0],E=c[1],V=y(S,E,M,l);if(V>se)it.warn(`The tick interval "${M}${l}" would generate ${V} ticks, which exceeds the maximum allowed (${se}). This may indicate an invalid date or time range. Skipping custom tick interval.`);else switch(l){case"millisecond":u.ticks($t.every(M));break;case"second":u.ticks(Yt.every(M));break;case"minute":u.ticks(It.every(M));break;case"hour":u.ticks(Lt.every(M));break;case"day":u.ticks(At.every(M));break;case"week":u.ticks(Je[j].every(M));break;case"month":u.ticks(Ot.every(M));break}}}if(_.append("g").attr("class","grid").attr("transform","translate("+k+", "+(b-50)+")").call(u).selectAll("text").style("text-anchor","middle").attr("fill","#000").attr("stroke","none").attr("font-size",10).attr("dy","1em"),i.db.topAxisEnabled()||s.topAxis){let M=ge(T).tickSize(-b+p+s.gridLineStartPadding).tickFormat(Ft(d));if(n!==null){let l=parseInt(n[1],10);if(isNaN(l)||l<=0)it.warn(`Invalid tick interval value: "${n[1]}". Skipping custom tick interval.`);else{let j=n[2],c=i.db.getWeekday()||s.weekday,S=T.domain(),E=S[0],V=S[1];if(y(E,V,l,j)<=se)switch(j){case"millisecond":M.ticks($t.every(l));break;case"second":M.ticks(Yt.every(l));break;case"minute":M.ticks(It.every(l));break;case"hour":M.ticks(Lt.every(l));break;case"day":M.ticks(At.every(l));break;case"week":M.ticks(Je[c].every(l));break;case"month":M.ticks(Ot.every(l));break}}}_.append("g").attr("class","grid").attr("transform","translate("+k+", "+p+")").call(M).selectAll("text").style("text-anchor","middle").attr("fill","#000").attr("stroke","none").attr("font-size",10)}}a(m,"makeGrid");function $(k,p){let x=0,b=Object.keys(H).map(o=>[o,H[o]]);_.append("g").selectAll("text").data(b).enter().append(function(o){let h=o[0].split(oe.lineBreakRegex),d=-(h.length-1)/2,u=D.createElementNS("http://www.w3.org/2000/svg","text");u.setAttribute("dy",d+"em");for(let[C,n]of h.entries()){let M=D.createElementNS("http://www.w3.org/2000/svg","tspan");M.setAttribute("alignment-baseline","central"),M.setAttribute("x","10"),C>0&&M.setAttribute("dy","1em"),M.textContent=n,u.appendChild(M)}return u}).attr("x",10).attr("y",function(o,h){if(h>0)for(let d=0;d<h;d++)return x+=b[h-1][1],o[1]*k/2+x*k+p;else return o[1]*k/2+p}).attr("font-size",s.sectionFontSize).attr("class",function(o){for(let[h,d]of z.entries())if(o[0]===d)return"sectionTitle sectionTitle"+h%s.numberSectionStyles;return"sectionTitle"})}a($,"vertLabels");function W(k,p,x,b){let o=i.db.getTodayMarker();if(o==="off")return;let h=_.append("g").attr("class","today"),d=new Date,u=h.append("line");u.attr("x1",T(d)+k).attr("x2",T(d)+k).attr("y1",s.titleTopMargin).attr("y2",b-s.titleTopMargin).attr("class","today"),o!==""&&u.attr("style",o.replace(/,/g,";"))}a(W,"drawToday");function O(k){let p={},x=[];for(let b=0,o=k.length;b<o;++b)Object.prototype.hasOwnProperty.call(p,k[b])||(p[k[b]]=!0,x.push(k[b]));return x}a(O,"checkUnique")},"draw"),en={setConf:zn,draw:Hn};var jn=a(t=>`
  .mermaid-main-font {
        font-family: ${t.fontFamily};
  }

  .exclude-range {
    fill: ${t.excludeBkgColor};
  }

  .section {
    stroke: none;
    opacity: 0.2;
  }

  .section0 {
    fill: ${t.sectionBkgColor};
  }

  .section2 {
    fill: ${t.sectionBkgColor2};
  }

  .section1,
  .section3 {
    fill: ${t.altSectionBkgColor};
    opacity: 0.2;
  }

  .sectionTitle0 {
    fill: ${t.titleColor};
  }

  .sectionTitle1 {
    fill: ${t.titleColor};
  }

  .sectionTitle2 {
    fill: ${t.titleColor};
  }

  .sectionTitle3 {
    fill: ${t.titleColor};
  }

  .sectionTitle {
    text-anchor: start;
    font-family: ${t.fontFamily};
  }


  /* Grid and axis */

  .grid .tick {
    stroke: ${t.gridColor};
    opacity: 0.8;
    shape-rendering: crispEdges;
  }

  .grid .tick text {
    font-family: ${t.fontFamily};
    fill: ${t.textColor};
  }

  .grid path {
    stroke-width: 0;
  }


  /* Today line */

  .today {
    fill: none;
    stroke: ${t.todayLineColor};
    stroke-width: 2px;
  }


  /* Task styling */

  /* Default task */

  .task {
    stroke-width: 2;
  }

  .taskText {
    text-anchor: middle;
    font-family: ${t.fontFamily};
  }

  .taskTextOutsideRight {
    fill: ${t.taskTextDarkColor};
    text-anchor: start;
    font-family: ${t.fontFamily};
  }

  .taskTextOutsideLeft {
    fill: ${t.taskTextDarkColor};
    text-anchor: end;
  }


  /* Special case clickable */

  .task.clickable {
    cursor: pointer;
  }

  .taskText.clickable {
    cursor: pointer;
    fill: ${t.taskTextClickableColor} !important;
    font-weight: bold;
  }

  .taskTextOutsideLeft.clickable {
    cursor: pointer;
    fill: ${t.taskTextClickableColor} !important;
    font-weight: bold;
  }

  .taskTextOutsideRight.clickable {
    cursor: pointer;
    fill: ${t.taskTextClickableColor} !important;
    font-weight: bold;
  }


  /* Specific task settings for the sections*/

  .taskText0,
  .taskText1,
  .taskText2,
  .taskText3 {
    fill: ${t.taskTextColor};
  }

  .task0,
  .task1,
  .task2,
  .task3 {
    fill: ${t.taskBkgColor};
    stroke: ${t.taskBorderColor};
  }

  .taskTextOutside0,
  .taskTextOutside2
  {
    fill: ${t.taskTextOutsideColor};
  }

  .taskTextOutside1,
  .taskTextOutside3 {
    fill: ${t.taskTextOutsideColor};
  }


  /* Active task */

  .active0,
  .active1,
  .active2,
  .active3 {
    fill: ${t.activeTaskBkgColor};
    stroke: ${t.activeTaskBorderColor};
  }

  .activeText0,
  .activeText1,
  .activeText2,
  .activeText3 {
    fill: ${t.taskTextDarkColor} !important;
  }


  /* Completed task */

  .done0,
  .done1,
  .done2,
  .done3 {
    stroke: ${t.doneTaskBorderColor};
    fill: ${t.doneTaskBkgColor};
    stroke-width: 2;
  }

  .doneText0,
  .doneText1,
  .doneText2,
  .doneText3 {
    fill: ${t.taskTextDarkColor} !important;
  }

  /* Done task text displayed outside the bar sits against the diagram background,
     not against the done-task bar, so it must use the outside/contrast color. */
  .doneText0.taskTextOutsideLeft,
  .doneText0.taskTextOutsideRight,
  .doneText1.taskTextOutsideLeft,
  .doneText1.taskTextOutsideRight,
  .doneText2.taskTextOutsideLeft,
  .doneText2.taskTextOutsideRight,
  .doneText3.taskTextOutsideLeft,
  .doneText3.taskTextOutsideRight {
    fill: ${t.taskTextOutsideColor} !important;
  }


  /* Tasks on the critical line */

  .crit0,
  .crit1,
  .crit2,
  .crit3 {
    stroke: ${t.critBorderColor};
    fill: ${t.critBkgColor};
    stroke-width: 2;
  }

  .activeCrit0,
  .activeCrit1,
  .activeCrit2,
  .activeCrit3 {
    stroke: ${t.critBorderColor};
    fill: ${t.activeTaskBkgColor};
    stroke-width: 2;
  }

  .doneCrit0,
  .doneCrit1,
  .doneCrit2,
  .doneCrit3 {
    stroke: ${t.critBorderColor};
    fill: ${t.doneTaskBkgColor};
    stroke-width: 2;
    cursor: pointer;
    shape-rendering: crispEdges;
  }

  .milestone {
    transform: rotate(45deg) scale(0.8,0.8);
  }

  .milestoneText {
    font-style: italic;
  }
  .doneCritText0,
  .doneCritText1,
  .doneCritText2,
  .doneCritText3 {
    fill: ${t.taskTextDarkColor} !important;
  }

  /* Done-crit task text outside the bar \u2014 same reasoning as doneText above. */
  .doneCritText0.taskTextOutsideLeft,
  .doneCritText0.taskTextOutsideRight,
  .doneCritText1.taskTextOutsideLeft,
  .doneCritText1.taskTextOutsideRight,
  .doneCritText2.taskTextOutsideLeft,
  .doneCritText2.taskTextOutsideRight,
  .doneCritText3.taskTextOutsideLeft,
  .doneCritText3.taskTextOutsideRight {
    fill: ${t.taskTextOutsideColor} !important;
  }

  .vert {
    stroke: ${t.vertLineColor};
  }

  .vertText {
    font-size: 15px;
    text-anchor: middle;
    fill: ${t.vertLineColor} !important;
  }

  .activeCritText0,
  .activeCritText1,
  .activeCritText2,
  .activeCritText3 {
    fill: ${t.taskTextDarkColor} !important;
  }

  .titleText {
    text-anchor: middle;
    font-size: 18px;
    fill: ${t.titleColor||t.textColor};
    font-family: ${t.fontFamily};
  }
`,"getStyles"),nn=jn;var yi={parser:Ye,db:qe,renderer:en,styles:nn};export{yi as diagram};
