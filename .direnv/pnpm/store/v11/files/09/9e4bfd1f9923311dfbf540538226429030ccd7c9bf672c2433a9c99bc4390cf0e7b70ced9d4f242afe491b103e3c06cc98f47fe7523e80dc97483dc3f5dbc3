/*!
 * CustomBounce 3.15.0
 * https://gsap.com
 * 
 * @license Copyright 2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license.
 * @author: Jack Doyle, jack@greensock.com
 */

!function(e,n){"object"==typeof exports&&"undefined"!=typeof module?n(exports):"function"==typeof define&&define.amd?define(["exports"],n):n((e=e||self).window=e.window||{})}(this,function(e){"use strict";function g(){return n||"undefined"!=typeof window&&(n=window.gsap)&&n.registerPlugin&&n}function h(e){n=g(),(j=n&&n.parseEase("_CE"))?(b=1,n.parseEase("bounce").config=function(e){return"object"==typeof e?t("",e):t("bounce("+e+")",{strength:+e})}):e&&console.warn("Please gsap.registerPlugin(CustomEase, CustomBounce)")}function i(e){var n,t=e.length,o=1/e[t-2];for(n=2;n<t;n+=2)e[n]=~~(e[n]*o*1e3)/1e3;e[t-2]=1}var n,b,j,t=function _create(e,n){b||h(1),n=n||{};var t,o,u,s,r,f,c,a=Math.min(.999,n.strength||.7),g=a,d=(n.squash||0)/100,p=d,l=1/.03,m=.2,C=1,w=.1,y=[0,0,.07,0,.1,1,.1,1],B=[0,0,0,0,.1,0,.1,0];for(r=0;r<200&&(f=w+(m*=g*((g+1)/2)),s=1-(C*=a*a),o=(u=w+.49*m)+.8*(u-(t=w+C/l)),d&&(w+=d,t+=d,u+=d,o+=d,f+=d,c=d/p,B.push(w-d,0,w-d,c,w-d/2,c,w,c,w,0,w,0,w,-.6*c,w+(f-w)/6,0,f,0),y.push(w-d,1,w,1,w,1),d*=a*a),y.push(w,1,t,s,u,s,o,s,f,1,f,1),a*=.95,l=C/(f-o),w=f,!(.999<s));r++);if(n.endAtStart&&"false"!==n.endAtStart){if(u=-.1,y.unshift(u,1,u,1,-.07,0),p)for(u-=d=2.5*p,y.unshift(u,1,u,1,u,1),B.splice(0,6),B.unshift(u,0,u,0,u,1,u+d/2,1,u+d,1,u+d,0,u+d,0,u+d,-.6,u+d+.033,0),r=0;r<B.length;r+=2)B[r]-=u;for(r=0;r<y.length;r+=2)y[r]-=u,y[r+1]=1-y[r+1]}return d&&(i(B),B[2]="C"+B[2],j(n.squashID||e+"-squash","M"+B.join(","))),i(y),y[2]="C"+y[2],j(e,"M"+y.join(","))},o=(CustomBounce.create=function create(e,n){return t(e,n)},CustomBounce.register=function register(e){n=e,h()},CustomBounce);function CustomBounce(e,n){this.ease=t(e,n)}g()&&n.registerPlugin(o),o.version="3.15.0",e.CustomBounce=o,e.default=o;if (typeof(window)==="undefined"||window!==e){Object.defineProperty(e,"__esModule",{value:!0})} else {delete e.default}});

