import katex from '../katex.mjs';

var scripts = document.body.getElementsByTagName("script");
scripts = Array.prototype.slice.call(scripts);
scripts.forEach(function (script) {
  if (!script.type || !script.type.match(/math\/tex/i)) {
    return -1;
  }
  var display = script.type.match(/mode\s*=\s*display(;|\s|\n|$)/) != null;
  var katexElement = document.createElement(display ? "div" : "span");
  katexElement.setAttribute("class", display ? "equation" : "inline-equation");
  try {
    katex.render(script.text, katexElement, {
      displayMode: display
    });
  } catch (err) {
    // eslint-disable-next-line no-console
    console.error(err);
    katexElement.textContent = script.text;
  }
  script.parentNode.replaceChild(katexElement, script);
});
