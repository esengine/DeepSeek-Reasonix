/*!
 * ScrambleTextPlugin 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/

/* eslint-disable */
import { emojiSafeSplit, getText } from "./utils/strings.js";

var CharSet = /*#__PURE__*/function () {
  function CharSet(chars) {
    this.chars = emojiSafeSplit(chars);
    this.sets = [];
    this.length = 50;

    for (var i = 0; i < 20; i++) {
      this.sets[i] = _scrambleText(80, this.chars); //we create 20 strings that are 80 characters long, randomly chosen and pack them into an array. We then randomly choose the scrambled text from this array in order to greatly improve efficiency compared to creating new randomized text from scratch each and every time it's needed. This is a simple lookup whereas the other technique requires looping through as many times as there are characters needed, and calling Math.random() each time through the loop, building the string, etc.
    }
  }

  var _proto = CharSet.prototype;

  _proto.grow = function grow(newLength) {
    //if we encounter a tween that has more than 80 characters, we'll need to add to the character sets accordingly. Once it's cached, it'll only need to grow again if we exceed that new length. Again, this is an efficiency tactic.
    for (var i = 0; i < 20; i++) {
      this.sets[i] += _scrambleText(newLength - this.length, this.chars);
    }

    this.length = newLength;
  };

  return CharSet;
}();

var gsap,
    _coreInitted,
    _getGSAP = function _getGSAP() {
  return gsap || typeof window !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap;
},
    _bonusValidated = 1,
    //<name>ScrambleTextPlugin</name>
_spacesExp = /\s+/g,
    _scrambleText = function _scrambleText(length, chars) {
  var l = chars.length,
      s = "";

  while (--length > -1) {
    s += chars[~~(Math.random() * l)];
  }

  return s;
},
    _upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
    _lower = _upper.toLowerCase(),
    _charsLookup = {
  upperCase: new CharSet(_upper),
  lowerCase: new CharSet(_lower),
  upperAndLowerCase: new CharSet(_upper + _lower)
},
    _initCore = function _initCore() {
  _coreInitted = gsap = _getGSAP();
};

export var ScrambleTextPlugin = {
  version: "3.15.0",
  name: "scrambleText",
  register: function register(core, Plugin, propTween) {
    gsap = core;

    _initCore();
  },
  init: function init(target, value, tween, index, targets) {
    _coreInitted || _initCore();
    this.prop = "innerHTML" in target ? "innerHTML" : "textContent" in target ? "textContent" : 0; // SVG text in IE doesn't have innerHTML, but it does have textContent.

    if (!this.prop) {
      return;
    }

    this.target = target;

    if (typeof value !== "object") {
      value = {
        text: value
      };
    }

    var text = value.text || value.value || "",
        trim = value.trim !== false,
        data = this,
        delim,
        maxLength,
        charset,
        splitByChars;
    data.delimiter = delim = value.delimiter || "";
    data.original = emojiSafeSplit(getText(target).replace(_spacesExp, " ").split("&nbsp;").join(""), delim, trim);

    if (text === "{original}" || text === true || text == null) {
      text = data.original.join(delim);
    }

    data.text = emojiSafeSplit((text || "").replace(_spacesExp, " "), delim, trim);
    data.hasClass = !!(value.newClass || value.oldClass);
    data.newClass = value.newClass;
    data.oldClass = value.oldClass;
    splitByChars = delim === "";
    data.textHasEmoji = splitByChars && !!data.text.emoji;
    data.charsHaveEmoji = !!value.chars && !!emojiSafeSplit(value.chars).emoji;
    data.length = splitByChars ? data.original.length : data.original.join(delim).length;
    data.lengthDif = (splitByChars ? data.text.length : data.text.join(delim).length) - data.length;
    data.fillChar = value.fillChar || value.chars && ~value.chars.indexOf(" ") ? "&nbsp;" : "";
    data.charSet = charset = _charsLookup[value.chars || "upperCase"] || new CharSet(value.chars);
    data.speed = 0.05 / (value.speed || 1);
    data.prevScrambleTime = 0;
    data.setIndex = Math.random() * 20 | 0;
    maxLength = data.length + Math.max(data.lengthDif, 0);

    if (maxLength > charset.length) {
      charset.grow(maxLength);
    }

    data.chars = charset.sets[data.setIndex];
    data.revealDelay = value.revealDelay || 0;
    data.tweenLength = value.tweenLength !== false;
    data.tween = tween;
    data.rightToLeft = !!value.rightToLeft;

    data._props.push("scrambleText", "text");

    return _bonusValidated;
  },
  render: function render(ratio, data) {
    var target = data.target,
        prop = data.prop,
        text = data.text,
        delimiter = data.delimiter,
        tween = data.tween,
        prevScrambleTime = data.prevScrambleTime,
        revealDelay = data.revealDelay,
        setIndex = data.setIndex,
        chars = data.chars,
        charSet = data.charSet,
        length = data.length,
        textHasEmoji = data.textHasEmoji,
        charsHaveEmoji = data.charsHaveEmoji,
        lengthDif = data.lengthDif,
        tweenLength = data.tweenLength,
        oldClass = data.oldClass,
        newClass = data.newClass,
        rightToLeft = data.rightToLeft,
        fillChar = data.fillChar,
        speed = data.speed,
        original = data.original,
        hasClass = data.hasClass,
        l = text.length,
        time = tween._time,
        timeDif = time - prevScrambleTime,
        i,
        i2,
        startText,
        endText,
        applyNew,
        applyOld,
        str,
        startClass,
        endClass,
        position,
        r;

    if (revealDelay) {
      if (tween._from) {
        time = tween._dur - time; //invert the time for from() tweens
      }

      ratio = time === 0 ? 0 : time < revealDelay ? 0.000001 : time === tween._dur ? 1 : tween._ease((time - revealDelay) / (tween._dur - revealDelay));
    }

    if (ratio < 0) {
      ratio = 0;
    } else if (ratio > 1) {
      ratio = 1;
    }

    if (rightToLeft) {
      ratio = 1 - ratio;
    }

    i = ~~(ratio * l + 0.5);

    if (ratio) {
      if (timeDif > speed || timeDif < -speed) {
        data.setIndex = setIndex = (setIndex + (Math.random() * 19 | 0)) % 20;
        data.chars = charSet.sets[setIndex];
        data.prevScrambleTime += timeDif;
      }

      endText = chars;
    } else {
      endText = original.join(delimiter);
    }

    r = tween._from ? ratio : 1 - ratio;
    position = length + (tweenLength ? tween._from ? r * r * r : 1 - r * r * r : 1) * lengthDif;

    if (rightToLeft) {
      if (ratio === 1 && (tween._from || tween.data === "isFromStart")) {
        //special case for from() tweens
        startText = "";
        endText = original.join(delimiter);
      } else {
        str = text.slice(i).join(delimiter);

        if (charsHaveEmoji) {
          startText = emojiSafeSplit(endText).slice(0, position - (textHasEmoji ? emojiSafeSplit(str) : str).length + 0.5 | 0).join("");
        } else {
          startText = endText.substr(0, position - (textHasEmoji ? emojiSafeSplit(str) : str).length + 0.5 | 0);
        }

        endText = str;
      }
    } else {
      startText = text.slice(0, i).join(delimiter);
      i2 = (textHasEmoji ? emojiSafeSplit(startText) : startText).length;

      if (charsHaveEmoji) {
        endText = emojiSafeSplit(endText).slice(i2, position + 0.5 | 0).join("");
      } else {
        endText = endText.substr(i2, position - i2 + 0.5 | 0);
      }
    }

    if (hasClass) {
      startClass = rightToLeft ? oldClass : newClass;
      endClass = rightToLeft ? newClass : oldClass;
      applyNew = startClass && i !== 0;
      applyOld = endClass && i !== l;
      str = (applyNew ? "<span class='" + startClass + "'>" : "") + startText + (applyNew ? "</span>" : "") + (applyOld ? "<span class='" + endClass + "'>" : "") + delimiter + endText + (applyOld ? "</span>" : "");
    } else {
      str = startText + delimiter + endText;
    }

    target[prop] = fillChar === "&nbsp;" && ~str.indexOf("  ") ? str.split("  ").join("&nbsp;&nbsp;") : str;
  }
};
ScrambleTextPlugin.emojiSafeSplit = emojiSafeSplit;
ScrambleTextPlugin.getText = getText;
_getGSAP() && gsap.registerPlugin(ScrambleTextPlugin);
export { ScrambleTextPlugin as default };