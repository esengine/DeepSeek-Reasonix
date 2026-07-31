import {
  selectSvgElement
} from "./chunk-VAUOI2AC.mjs";
import {
  parseFontSize
} from "./chunk-ICXQ74PX.mjs";
import {
  clear,
  common_default,
  configureSvgSize,
  getAccDescription,
  getAccTitle,
  getConfig2 as getConfig,
  getDiagramTitle,
  setAccDescription,
  setAccTitle,
  setDiagramTitle
} from "./chunk-WYO6CB5R.mjs";
import "./chunk-X3CZISLH.mjs";
import {
  __name
} from "./chunk-Y2CYZVJY.mjs";

// src/diagrams/ishikawa/parser/ishikawa.jison
var parser = (function() {
  var o = /* @__PURE__ */ __name(function(k, v, o2, l) {
    for (o2 = o2 || {}, l = k.length; l--; o2[k[l]] = v) ;
    return o2;
  }, "o"), $V0 = [1, 4], $V1 = [1, 14], $V2 = [1, 12], $V3 = [1, 13], $V4 = [6, 7, 8], $V5 = [1, 20], $V6 = [1, 18], $V7 = [1, 19], $V8 = [6, 7, 11], $V9 = [1, 6, 13, 14], $Va = [1, 23], $Vb = [1, 24], $Vc = [1, 6, 7, 11, 13, 14];
  var parser2 = {
    trace: /* @__PURE__ */ __name(function trace() {
    }, "trace"),
    yy: {},
    symbols_: { "error": 2, "start": 3, "ishikawa": 4, "spaceLines": 5, "SPACELINE": 6, "NL": 7, "ISHIKAWA": 8, "document": 9, "stop": 10, "EOF": 11, "statement": 12, "SPACELIST": 13, "TEXT": 14, "$accept": 0, "$end": 1 },
    terminals_: { 2: "error", 6: "SPACELINE", 7: "NL", 8: "ISHIKAWA", 11: "EOF", 13: "SPACELIST", 14: "TEXT" },
    productions_: [0, [3, 1], [3, 2], [5, 1], [5, 2], [5, 2], [4, 2], [4, 3], [10, 1], [10, 1], [10, 1], [10, 2], [10, 2], [9, 3], [9, 2], [12, 2], [12, 1], [12, 1], [12, 1]],
    performAction: /* @__PURE__ */ __name(function anonymous(yytext, yyleng, yylineno, yy, yystate, $$, _$) {
      var $0 = $$.length - 1;
      switch (yystate) {
        case 6:
        case 7:
          return yy;
          break;
        case 15:
          yy.addNode($$[$0 - 1].length, $$[$0].trim());
          break;
        case 16:
          yy.addNode(0, $$[$0].trim());
          break;
      }
    }, "anonymous"),
    table: [{ 3: 1, 4: 2, 5: 3, 6: [1, 5], 8: $V0 }, { 1: [3] }, { 1: [2, 1] }, { 4: 6, 6: [1, 7], 7: [1, 8], 8: $V0 }, { 6: $V1, 7: [1, 10], 9: 9, 12: 11, 13: $V2, 14: $V3 }, o($V4, [2, 3]), { 1: [2, 2] }, o($V4, [2, 4]), o($V4, [2, 5]), { 1: [2, 6], 6: $V1, 12: 15, 13: $V2, 14: $V3 }, { 6: $V1, 9: 16, 12: 11, 13: $V2, 14: $V3 }, { 6: $V5, 7: $V6, 10: 17, 11: $V7 }, o($V8, [2, 18], { 14: [1, 21] }), o($V8, [2, 16]), o($V8, [2, 17]), { 6: $V5, 7: $V6, 10: 22, 11: $V7 }, { 1: [2, 7], 6: $V1, 12: 15, 13: $V2, 14: $V3 }, o($V9, [2, 14], { 7: $Va, 11: $Vb }), o($Vc, [2, 8]), o($Vc, [2, 9]), o($Vc, [2, 10]), o($V8, [2, 15]), o($V9, [2, 13], { 7: $Va, 11: $Vb }), o($Vc, [2, 11]), o($Vc, [2, 12])],
    defaultActions: { 2: [2, 1], 6: [2, 2] },
    parseError: /* @__PURE__ */ __name(function parseError(str, hash) {
      if (hash.recoverable) {
        this.trace(str);
      } else {
        var error = new Error(str);
        error.hash = hash;
        throw error;
      }
    }, "parseError"),
    parse: /* @__PURE__ */ __name(function parse(input) {
      var self = this, stack = [0], tstack = [], vstack = [null], lstack = [], table = this.table, yytext = "", yylineno = 0, yyleng = 0, recovering = 0, TERROR = 2, EOF = 1;
      var args = lstack.slice.call(arguments, 1);
      var lexer2 = Object.create(this.lexer);
      var sharedState = { yy: {} };
      for (var k in this.yy) {
        if (Object.prototype.hasOwnProperty.call(this.yy, k)) {
          sharedState.yy[k] = this.yy[k];
        }
      }
      lexer2.setInput(input, sharedState.yy);
      sharedState.yy.lexer = lexer2;
      sharedState.yy.parser = this;
      if (typeof lexer2.yylloc == "undefined") {
        lexer2.yylloc = {};
      }
      var yyloc = lexer2.yylloc;
      lstack.push(yyloc);
      var ranges = lexer2.options && lexer2.options.ranges;
      if (typeof sharedState.yy.parseError === "function") {
        this.parseError = sharedState.yy.parseError;
      } else {
        this.parseError = Object.getPrototypeOf(this).parseError;
      }
      function popStack(n) {
        stack.length = stack.length - 2 * n;
        vstack.length = vstack.length - n;
        lstack.length = lstack.length - n;
      }
      __name(popStack, "popStack");
      function lex() {
        var token;
        token = tstack.pop() || lexer2.lex() || EOF;
        if (typeof token !== "number") {
          if (token instanceof Array) {
            tstack = token;
            token = tstack.pop();
          }
          token = self.symbols_[token] || token;
        }
        return token;
      }
      __name(lex, "lex");
      var symbol, preErrorSymbol, state, action, a, r, yyval = {}, p, len, newState, expected;
      while (true) {
        state = stack[stack.length - 1];
        if (this.defaultActions[state]) {
          action = this.defaultActions[state];
        } else {
          if (symbol === null || typeof symbol == "undefined") {
            symbol = lex();
          }
          action = table[state] && table[state][symbol];
        }
        if (typeof action === "undefined" || !action.length || !action[0]) {
          var errStr = "";
          expected = [];
          for (p in table[state]) {
            if (this.terminals_[p] && p > TERROR) {
              expected.push("'" + this.terminals_[p] + "'");
            }
          }
          if (lexer2.showPosition) {
            errStr = "Parse error on line " + (yylineno + 1) + ":\n" + lexer2.showPosition() + "\nExpecting " + expected.join(", ") + ", got '" + (this.terminals_[symbol] || symbol) + "'";
          } else {
            errStr = "Parse error on line " + (yylineno + 1) + ": Unexpected " + (symbol == EOF ? "end of input" : "'" + (this.terminals_[symbol] || symbol) + "'");
          }
          this.parseError(errStr, {
            text: lexer2.match,
            token: this.terminals_[symbol] || symbol,
            line: lexer2.yylineno,
            loc: yyloc,
            expected
          });
        }
        if (action[0] instanceof Array && action.length > 1) {
          throw new Error("Parse Error: multiple actions possible at state: " + state + ", token: " + symbol);
        }
        switch (action[0]) {
          case 1:
            stack.push(symbol);
            vstack.push(lexer2.yytext);
            lstack.push(lexer2.yylloc);
            stack.push(action[1]);
            symbol = null;
            if (!preErrorSymbol) {
              yyleng = lexer2.yyleng;
              yytext = lexer2.yytext;
              yylineno = lexer2.yylineno;
              yyloc = lexer2.yylloc;
              if (recovering > 0) {
                recovering--;
              }
            } else {
              symbol = preErrorSymbol;
              preErrorSymbol = null;
            }
            break;
          case 2:
            len = this.productions_[action[1]][1];
            yyval.$ = vstack[vstack.length - len];
            yyval._$ = {
              first_line: lstack[lstack.length - (len || 1)].first_line,
              last_line: lstack[lstack.length - 1].last_line,
              first_column: lstack[lstack.length - (len || 1)].first_column,
              last_column: lstack[lstack.length - 1].last_column
            };
            if (ranges) {
              yyval._$.range = [
                lstack[lstack.length - (len || 1)].range[0],
                lstack[lstack.length - 1].range[1]
              ];
            }
            r = this.performAction.apply(yyval, [
              yytext,
              yyleng,
              yylineno,
              sharedState.yy,
              action[1],
              vstack,
              lstack
            ].concat(args));
            if (typeof r !== "undefined") {
              return r;
            }
            if (len) {
              stack = stack.slice(0, -1 * len * 2);
              vstack = vstack.slice(0, -1 * len);
              lstack = lstack.slice(0, -1 * len);
            }
            stack.push(this.productions_[action[1]][0]);
            vstack.push(yyval.$);
            lstack.push(yyval._$);
            newState = table[stack[stack.length - 2]][stack[stack.length - 1]];
            stack.push(newState);
            break;
          case 3:
            return true;
        }
      }
      return true;
    }, "parse")
  };
  var lexer = /* @__PURE__ */ (function() {
    var lexer2 = {
      EOF: 1,
      parseError: /* @__PURE__ */ __name(function parseError(str, hash) {
        if (this.yy.parser) {
          this.yy.parser.parseError(str, hash);
        } else {
          throw new Error(str);
        }
      }, "parseError"),
      // resets the lexer, sets new input
      setInput: /* @__PURE__ */ __name(function(input, yy) {
        this.yy = yy || this.yy || {};
        this._input = input;
        this._more = this._backtrack = this.done = false;
        this.yylineno = this.yyleng = 0;
        this.yytext = this.matched = this.match = "";
        this.conditionStack = ["INITIAL"];
        this.yylloc = {
          first_line: 1,
          first_column: 0,
          last_line: 1,
          last_column: 0
        };
        if (this.options.ranges) {
          this.yylloc.range = [0, 0];
        }
        this.offset = 0;
        return this;
      }, "setInput"),
      // consumes and returns one char from the input
      input: /* @__PURE__ */ __name(function() {
        var ch = this._input[0];
        this.yytext += ch;
        this.yyleng++;
        this.offset++;
        this.match += ch;
        this.matched += ch;
        var lines = ch.match(/(?:\r\n?|\n).*/g);
        if (lines) {
          this.yylineno++;
          this.yylloc.last_line++;
        } else {
          this.yylloc.last_column++;
        }
        if (this.options.ranges) {
          this.yylloc.range[1]++;
        }
        this._input = this._input.slice(1);
        return ch;
      }, "input"),
      // unshifts one char (or a string) into the input
      unput: /* @__PURE__ */ __name(function(ch) {
        var len = ch.length;
        var lines = ch.split(/(?:\r\n?|\n)/g);
        this._input = ch + this._input;
        this.yytext = this.yytext.substr(0, this.yytext.length - len);
        this.offset -= len;
        var oldLines = this.match.split(/(?:\r\n?|\n)/g);
        this.match = this.match.substr(0, this.match.length - 1);
        this.matched = this.matched.substr(0, this.matched.length - 1);
        if (lines.length - 1) {
          this.yylineno -= lines.length - 1;
        }
        var r = this.yylloc.range;
        this.yylloc = {
          first_line: this.yylloc.first_line,
          last_line: this.yylineno + 1,
          first_column: this.yylloc.first_column,
          last_column: lines ? (lines.length === oldLines.length ? this.yylloc.first_column : 0) + oldLines[oldLines.length - lines.length].length - lines[0].length : this.yylloc.first_column - len
        };
        if (this.options.ranges) {
          this.yylloc.range = [r[0], r[0] + this.yyleng - len];
        }
        this.yyleng = this.yytext.length;
        return this;
      }, "unput"),
      // When called from action, caches matched text and appends it on next action
      more: /* @__PURE__ */ __name(function() {
        this._more = true;
        return this;
      }, "more"),
      // When called from action, signals the lexer that this rule fails to match the input, so the next matching rule (regex) should be tested instead.
      reject: /* @__PURE__ */ __name(function() {
        if (this.options.backtrack_lexer) {
          this._backtrack = true;
        } else {
          return this.parseError("Lexical error on line " + (this.yylineno + 1) + ". You can only invoke reject() in the lexer when the lexer is of the backtracking persuasion (options.backtrack_lexer = true).\n" + this.showPosition(), {
            text: "",
            token: null,
            line: this.yylineno
          });
        }
        return this;
      }, "reject"),
      // retain first n characters of the match
      less: /* @__PURE__ */ __name(function(n) {
        this.unput(this.match.slice(n));
      }, "less"),
      // displays already matched input, i.e. for error messages
      pastInput: /* @__PURE__ */ __name(function() {
        var past = this.matched.substr(0, this.matched.length - this.match.length);
        return (past.length > 20 ? "..." : "") + past.substr(-20).replace(/\n/g, "");
      }, "pastInput"),
      // displays upcoming input, i.e. for error messages
      upcomingInput: /* @__PURE__ */ __name(function() {
        var next = this.match;
        if (next.length < 20) {
          next += this._input.substr(0, 20 - next.length);
        }
        return (next.substr(0, 20) + (next.length > 20 ? "..." : "")).replace(/\n/g, "");
      }, "upcomingInput"),
      // displays the character position where the lexing error occurred, i.e. for error messages
      showPosition: /* @__PURE__ */ __name(function() {
        var pre = this.pastInput();
        var c = new Array(pre.length + 1).join("-");
        return pre + this.upcomingInput() + "\n" + c + "^";
      }, "showPosition"),
      // test the lexed token: return FALSE when not a match, otherwise return token
      test_match: /* @__PURE__ */ __name(function(match, indexed_rule) {
        var token, lines, backup;
        if (this.options.backtrack_lexer) {
          backup = {
            yylineno: this.yylineno,
            yylloc: {
              first_line: this.yylloc.first_line,
              last_line: this.last_line,
              first_column: this.yylloc.first_column,
              last_column: this.yylloc.last_column
            },
            yytext: this.yytext,
            match: this.match,
            matches: this.matches,
            matched: this.matched,
            yyleng: this.yyleng,
            offset: this.offset,
            _more: this._more,
            _input: this._input,
            yy: this.yy,
            conditionStack: this.conditionStack.slice(0),
            done: this.done
          };
          if (this.options.ranges) {
            backup.yylloc.range = this.yylloc.range.slice(0);
          }
        }
        lines = match[0].match(/(?:\r\n?|\n).*/g);
        if (lines) {
          this.yylineno += lines.length;
        }
        this.yylloc = {
          first_line: this.yylloc.last_line,
          last_line: this.yylineno + 1,
          first_column: this.yylloc.last_column,
          last_column: lines ? lines[lines.length - 1].length - lines[lines.length - 1].match(/\r?\n?/)[0].length : this.yylloc.last_column + match[0].length
        };
        this.yytext += match[0];
        this.match += match[0];
        this.matches = match;
        this.yyleng = this.yytext.length;
        if (this.options.ranges) {
          this.yylloc.range = [this.offset, this.offset += this.yyleng];
        }
        this._more = false;
        this._backtrack = false;
        this._input = this._input.slice(match[0].length);
        this.matched += match[0];
        token = this.performAction.call(this, this.yy, this, indexed_rule, this.conditionStack[this.conditionStack.length - 1]);
        if (this.done && this._input) {
          this.done = false;
        }
        if (token) {
          return token;
        } else if (this._backtrack) {
          for (var k in backup) {
            this[k] = backup[k];
          }
          return false;
        }
        return false;
      }, "test_match"),
      // return next match in input
      next: /* @__PURE__ */ __name(function() {
        if (this.done) {
          return this.EOF;
        }
        if (!this._input) {
          this.done = true;
        }
        var token, match, tempMatch, index;
        if (!this._more) {
          this.yytext = "";
          this.match = "";
        }
        var rules = this._currentRules();
        for (var i = 0; i < rules.length; i++) {
          tempMatch = this._input.match(this.rules[rules[i]]);
          if (tempMatch && (!match || tempMatch[0].length > match[0].length)) {
            match = tempMatch;
            index = i;
            if (this.options.backtrack_lexer) {
              token = this.test_match(tempMatch, rules[i]);
              if (token !== false) {
                return token;
              } else if (this._backtrack) {
                match = false;
                continue;
              } else {
                return false;
              }
            } else if (!this.options.flex) {
              break;
            }
          }
        }
        if (match) {
          token = this.test_match(match, rules[index]);
          if (token !== false) {
            return token;
          }
          return false;
        }
        if (this._input === "") {
          return this.EOF;
        } else {
          return this.parseError("Lexical error on line " + (this.yylineno + 1) + ". Unrecognized text.\n" + this.showPosition(), {
            text: "",
            token: null,
            line: this.yylineno
          });
        }
      }, "next"),
      // return next match that has a token
      lex: /* @__PURE__ */ __name(function lex() {
        var r = this.next();
        if (r) {
          return r;
        } else {
          return this.lex();
        }
      }, "lex"),
      // activates a new lexer condition state (pushes the new lexer condition state onto the condition stack)
      begin: /* @__PURE__ */ __name(function begin(condition) {
        this.conditionStack.push(condition);
      }, "begin"),
      // pop the previously active lexer condition state off the condition stack
      popState: /* @__PURE__ */ __name(function popState() {
        var n = this.conditionStack.length - 1;
        if (n > 0) {
          return this.conditionStack.pop();
        } else {
          return this.conditionStack[0];
        }
      }, "popState"),
      // produce the lexer rule set which is active for the currently active lexer condition state
      _currentRules: /* @__PURE__ */ __name(function _currentRules() {
        if (this.conditionStack.length && this.conditionStack[this.conditionStack.length - 1]) {
          return this.conditions[this.conditionStack[this.conditionStack.length - 1]].rules;
        } else {
          return this.conditions["INITIAL"].rules;
        }
      }, "_currentRules"),
      // return the currently active lexer condition state; when an index argument is provided it produces the N-th previous condition state, if available
      topState: /* @__PURE__ */ __name(function topState(n) {
        n = this.conditionStack.length - 1 - Math.abs(n || 0);
        if (n >= 0) {
          return this.conditionStack[n];
        } else {
          return "INITIAL";
        }
      }, "topState"),
      // alias for begin(condition)
      pushState: /* @__PURE__ */ __name(function pushState(condition) {
        this.begin(condition);
      }, "pushState"),
      // return the number of states currently on the stack
      stateStackSize: /* @__PURE__ */ __name(function stateStackSize() {
        return this.conditionStack.length;
      }, "stateStackSize"),
      options: { "case-insensitive": true },
      performAction: /* @__PURE__ */ __name(function anonymous(yy, yy_, $avoiding_name_collisions, YY_START) {
        var YYSTATE = YY_START;
        switch ($avoiding_name_collisions) {
          case 0:
            return 6;
            break;
          case 1:
            return 8;
            break;
          case 2:
            return 8;
            break;
          case 3:
            return 6;
            break;
          case 4:
            return 7;
            break;
          case 5:
            return 13;
            break;
          case 6:
            return 14;
            break;
          case 7:
            return 11;
            break;
        }
      }, "anonymous"),
      rules: [/^(?:\s*%%.*)/i, /^(?:ishikawa-beta\b)/i, /^(?:ishikawa\b)/i, /^(?:[\s]+[\n])/i, /^(?:[\n]+)/i, /^(?:[\s]+)/i, /^(?:[^\n]+)/i, /^(?:$)/i],
      conditions: { "INITIAL": { "rules": [0, 1, 2, 3, 4, 5, 6, 7], "inclusive": true } }
    };
    return lexer2;
  })();
  parser2.lexer = lexer;
  function Parser() {
    this.yy = {};
  }
  __name(Parser, "Parser");
  Parser.prototype = parser2;
  parser2.Parser = Parser;
  return new Parser();
})();
parser.parser = parser;
var ishikawa_default = parser;

// src/diagrams/ishikawa/ishikawaDb.ts
var IshikawaDB = class {
  constructor() {
    this.stack = [];
    this.clear = this.clear.bind(this);
    this.addNode = this.addNode.bind(this);
    this.getRoot = this.getRoot.bind(this);
  }
  static {
    __name(this, "IshikawaDB");
  }
  clear() {
    this.root = void 0;
    this.stack = [];
    this.baseLevel = void 0;
    clear();
  }
  getRoot() {
    return this.root;
  }
  addNode(rawLevel, text) {
    const label = common_default.sanitizeText(text, getConfig());
    if (!this.root) {
      this.root = { text: label, children: [] };
      this.stack = [{ level: 0, node: this.root }];
      setDiagramTitle(label);
      return;
    }
    this.baseLevel ??= rawLevel;
    let level = rawLevel - this.baseLevel + 1;
    if (level <= 0) {
      level = 1;
    }
    while (this.stack.length > 1 && this.stack[this.stack.length - 1].level >= level) {
      this.stack.pop();
    }
    const parent = this.stack[this.stack.length - 1].node;
    const node = { text: label, children: [] };
    parent.children.push(node);
    this.stack.push({ level, node });
  }
  getAccTitle() {
    return getAccTitle();
  }
  setAccTitle(title) {
    setAccTitle(title);
  }
  getAccDescription() {
    return getAccDescription();
  }
  setAccDescription(description) {
    setAccDescription(description);
  }
  getDiagramTitle() {
    return getDiagramTitle();
  }
  setDiagramTitle(title) {
    setDiagramTitle(title);
  }
};

// src/diagrams/ishikawa/ishikawaRenderer.ts
import rough from "roughjs";
var FONT_SIZE_DEFAULT = 14;
var SPINE_BASE_LENGTH = 250;
var BONE_STUB = 30;
var BONE_BASE = 60;
var BONE_PER_CHILD = 5;
var ANGLE = 82 * Math.PI / 180;
var COS_A = Math.cos(ANGLE);
var SIN_A = Math.sin(ANGLE);
var applyPaddedViewBox = /* @__PURE__ */ __name((svgEl, pad, maxW) => {
  const bbox = svgEl.node().getBBox();
  const w = bbox.width + pad * 2;
  const h = bbox.height + pad * 2;
  configureSvgSize(svgEl, h, w, maxW);
  svgEl.attr("viewBox", `${bbox.x - pad} ${bbox.y - pad} ${w} ${h}`);
}, "applyPaddedViewBox");
var draw = /* @__PURE__ */ __name((_text, id, _version, diagram2) => {
  const db = diagram2.db;
  const root = db.getRoot();
  if (!root) {
    return;
  }
  const drawConfig = getConfig();
  const { look, handDrawnSeed, themeVariables } = drawConfig;
  const fontSize = parseFontSize(drawConfig.fontSize)[0] ?? FONT_SIZE_DEFAULT;
  const isHandDrawn = look === "handDrawn";
  const causes = root.children ?? [];
  const padding = drawConfig.ishikawa?.diagramPadding ?? 20;
  const useMaxWidth = drawConfig.ishikawa?.useMaxWidth ?? false;
  const svg = selectSvgElement(id);
  const g = svg.append("g").attr("class", "ishikawa");
  const roughSvg = isHandDrawn ? rough.svg(svg.node()) : void 0;
  const roughContext = roughSvg ? {
    roughSvg,
    seed: handDrawnSeed ?? 0,
    lineColor: themeVariables?.lineColor ?? "#333",
    fillColor: themeVariables?.mainBkg ?? "#fff"
  } : void 0;
  const markerId = `ishikawa-arrow-${id}`;
  if (!isHandDrawn) {
    g.append("defs").append("marker").attr("id", markerId).attr("viewBox", "0 0 10 10").attr("refX", 0).attr("refY", 5).attr("markerWidth", 6).attr("markerHeight", 6).attr("orient", "auto").append("path").attr("d", "M 10 0 L 0 5 L 10 10 Z").attr("class", "ishikawa-arrow");
  }
  let spineX = 0;
  let spineY = SPINE_BASE_LENGTH;
  const spineLine = isHandDrawn ? void 0 : drawLine(g, spineX, spineY, spineX, spineY, "ishikawa-spine");
  drawHead(g, spineX, spineY, root.text, fontSize, roughContext);
  if (!causes.length) {
    if (isHandDrawn) {
      drawLine(g, spineX, spineY, spineX, spineY, "ishikawa-spine", roughContext);
    }
    applyPaddedViewBox(svg, padding, useMaxWidth);
    return;
  }
  spineX -= 20;
  const upperCauses = causes.filter((_, i) => i % 2 === 0);
  const lowerCauses = causes.filter((_, i) => i % 2 === 1);
  const upperStats = sideStats(upperCauses);
  const lowerStats = sideStats(lowerCauses);
  const descendantTotal = upperStats.total + lowerStats.total;
  let upperLen = SPINE_BASE_LENGTH;
  let lowerLen = SPINE_BASE_LENGTH;
  if (descendantTotal > 0) {
    const pool = SPINE_BASE_LENGTH * 2;
    const minLen = SPINE_BASE_LENGTH * 0.3;
    upperLen = Math.max(minLen, pool * (upperStats.total / descendantTotal));
    lowerLen = Math.max(minLen, pool * (lowerStats.total / descendantTotal));
  }
  const minSpacing = fontSize * 2;
  upperLen = Math.max(upperLen, upperStats.max * minSpacing);
  lowerLen = Math.max(lowerLen, lowerStats.max * minSpacing);
  spineY = Math.max(upperLen, SPINE_BASE_LENGTH);
  if (spineLine) {
    spineLine.attr("y1", spineY).attr("y2", spineY);
  }
  g.select(".ishikawa-head-group").attr("transform", `translate(0,${spineY})`);
  const pairCount = Math.ceil(causes.length / 2);
  for (let p = 0; p < pairCount; p++) {
    const pg = g.append("g").attr("class", "ishikawa-pair");
    for (const [cause, dir, len] of [
      [causes[p * 2], -1, upperLen],
      [causes[p * 2 + 1], 1, lowerLen]
    ]) {
      if (cause) {
        drawBranch(pg, cause, spineX, spineY, dir, len, fontSize, roughContext);
      }
    }
    spineX = pg.selectAll("text").nodes().reduce((left, n) => Math.min(left, n.getBBox().x), Infinity);
  }
  if (isHandDrawn) {
    drawLine(g, spineX, spineY, 0, spineY, "ishikawa-spine", roughContext);
  } else {
    spineLine.attr("x1", spineX);
    const markerUrl = `url(#${markerId})`;
    g.selectAll("line.ishikawa-branch, line.ishikawa-sub-branch").attr("marker-start", markerUrl);
  }
  applyPaddedViewBox(svg, padding, useMaxWidth);
}, "draw");
var sideStats = /* @__PURE__ */ __name((nodes) => {
  const countDescendants = /* @__PURE__ */ __name((node) => node.children.reduce((sum, child) => sum + 1 + countDescendants(child), 0), "countDescendants");
  return nodes.reduce(
    (stats, node) => {
      const descendants = countDescendants(node);
      stats.total += descendants;
      stats.max = Math.max(stats.max, descendants);
      return stats;
    },
    { total: 0, max: 0 }
  );
}, "sideStats");
var drawHead = /* @__PURE__ */ __name((svg, x, y, label, fontSize, roughContext) => {
  const maxChars = Math.max(6, Math.floor(110 / (fontSize * 0.6)));
  const headGroup = svg.append("g").attr("class", "ishikawa-head-group").attr("transform", `translate(${x},${y})`);
  const textEl = drawMultilineText(
    headGroup,
    wrapText(label, maxChars),
    0,
    0,
    "ishikawa-head-label",
    "start",
    fontSize
  );
  const tb = textEl.node().getBBox();
  const w = Math.max(60, tb.width + 6);
  const h = Math.max(40, tb.height * 2 + 40);
  const headPath = `M 0 ${-h / 2} L 0 ${h / 2} Q ${w * 2.4} 0 0 ${-h / 2} Z`;
  if (roughContext) {
    const roughNode = roughContext.roughSvg.path(headPath, {
      roughness: 1.5,
      seed: roughContext.seed,
      fill: roughContext.fillColor,
      fillStyle: "hachure",
      fillWeight: 2.5,
      hachureGap: 5,
      stroke: roughContext.lineColor,
      strokeWidth: 2
    });
    headGroup.insert(() => roughNode, ":first-child").attr("class", "ishikawa-head");
  } else {
    headGroup.insert("path", ":first-child").attr("class", "ishikawa-head").attr("d", headPath);
  }
  textEl.attr("transform", `translate(${(w - tb.width) / 2 - tb.x + 3},${-tb.y - tb.height / 2})`);
}, "drawHead");
var flattenTree = /* @__PURE__ */ __name((children, direction) => {
  const entries = [];
  const yOrder = [];
  const walk = /* @__PURE__ */ __name((nodes, pid, depth) => {
    const ordered = direction === -1 ? [...nodes].reverse() : nodes;
    for (const child of ordered) {
      const idx = entries.length;
      const gc = child.children ?? [];
      entries.push({
        depth,
        text: wrapText(child.text, 15),
        parentIndex: pid,
        childCount: gc.length
      });
      if (depth % 2 === 0) {
        yOrder.push(idx);
        if (gc.length) {
          walk(gc, idx, depth + 1);
        }
      } else {
        if (gc.length) {
          walk(gc, idx, depth + 1);
        }
        yOrder.push(idx);
      }
    }
  }, "walk");
  walk(children, -1, 2);
  return { entries, yOrder };
}, "flattenTree");
var drawCauseLabel = /* @__PURE__ */ __name((svg, text, x, y, direction, fontSize, roughContext) => {
  const lg = svg.append("g").attr("class", "ishikawa-label-group");
  const lt = drawMultilineText(
    lg,
    text,
    x,
    y + 11 * direction,
    "ishikawa-label cause",
    "middle",
    fontSize
  );
  const tb = lt.node().getBBox();
  if (roughContext) {
    const roughNode = roughContext.roughSvg.rectangle(
      tb.x - 20,
      tb.y - 2,
      tb.width + 40,
      tb.height + 4,
      {
        roughness: 1.5,
        seed: roughContext.seed,
        fill: roughContext.fillColor,
        fillStyle: "hachure",
        fillWeight: 2.5,
        hachureGap: 5,
        stroke: roughContext.lineColor,
        strokeWidth: 2
      }
    );
    lg.insert(() => roughNode, ":first-child").attr("class", "ishikawa-label-box");
  } else {
    lg.insert("rect", ":first-child").attr("class", "ishikawa-label-box").attr("x", tb.x - 20).attr("y", tb.y - 2).attr("width", tb.width + 40).attr("height", tb.height + 4);
  }
}, "drawCauseLabel");
var drawArrowMarker = /* @__PURE__ */ __name((g, x, y, dx, dy, roughContext) => {
  const len = Math.sqrt(dx * dx + dy * dy);
  if (len === 0) {
    return;
  }
  const ux = dx / len;
  const uy = dy / len;
  const s = 6;
  const px = -uy * s;
  const py = ux * s;
  const tipX = x;
  const tipY = y;
  const d = `M ${tipX} ${tipY} L ${tipX - ux * s * 2 + px} ${tipY - uy * s * 2 + py} L ${tipX - ux * s * 2 - px} ${tipY - uy * s * 2 - py} Z`;
  const roughNode = roughContext.roughSvg.path(d, {
    roughness: 1,
    seed: roughContext.seed,
    fill: roughContext.lineColor,
    fillStyle: "solid",
    stroke: roughContext.lineColor,
    strokeWidth: 1
  });
  g.append(() => roughNode);
}, "drawArrowMarker");
var drawBranch = /* @__PURE__ */ __name((svg, node, startX, startY, direction, length, fontSize, roughContext) => {
  const children = node.children ?? [];
  const lineLen = length * (children.length ? 1 : 0.2);
  const dx = -COS_A * lineLen;
  const dy = SIN_A * lineLen * direction;
  const endX = startX + dx;
  const endY = startY + dy;
  drawLine(svg, startX, startY, endX, endY, "ishikawa-branch", roughContext);
  if (roughContext) {
    drawArrowMarker(svg, startX, startY, startX - endX, startY - endY, roughContext);
  }
  drawCauseLabel(svg, node.text, endX, endY, direction, fontSize, roughContext);
  if (!children.length) {
    return;
  }
  const { entries, yOrder } = flattenTree(children, direction);
  const entryCount = entries.length;
  const ys = new Array(entryCount);
  for (const [slot, entryIdx] of yOrder.entries()) {
    ys[entryIdx] = startY + dy * ((slot + 1) / (entryCount + 1));
  }
  const bones = /* @__PURE__ */ new Map();
  bones.set(-1, {
    x0: startX,
    y0: startY,
    x1: endX,
    y1: endY,
    childCount: children.length,
    childrenDrawn: 0
  });
  const diagonalX = -COS_A;
  const diagonalY = SIN_A * direction;
  const oddLabel = direction < 0 ? "ishikawa-label up" : "ishikawa-label down";
  for (const [i, e] of entries.entries()) {
    const y = ys[i];
    const par = bones.get(e.parentIndex);
    const grp = svg.append("g").attr("class", "ishikawa-sub-group");
    let bx0 = 0;
    let by0 = 0;
    let bx1 = 0;
    if (e.depth % 2 === 0) {
      const dyP = par.y1 - par.y0;
      bx0 = lerp(par.x0, par.x1, dyP ? (y - par.y0) / dyP : 0.5);
      by0 = y;
      bx1 = bx0 - (e.childCount > 0 ? BONE_BASE + e.childCount * BONE_PER_CHILD : BONE_STUB);
      drawLine(grp, bx0, y, bx1, y, "ishikawa-sub-branch", roughContext);
      if (roughContext) {
        drawArrowMarker(grp, bx0, y, 1, 0, roughContext);
      }
      drawMultilineText(grp, e.text, bx1, y, "ishikawa-label align", "end", fontSize);
    } else {
      const k = par.childrenDrawn++;
      bx0 = lerp(par.x0, par.x1, (par.childCount - k) / (par.childCount + 1));
      by0 = par.y0;
      bx1 = bx0 + diagonalX * ((y - by0) / diagonalY);
      drawLine(grp, bx0, by0, bx1, y, "ishikawa-sub-branch", roughContext);
      if (roughContext) {
        drawArrowMarker(grp, bx0, by0, bx0 - bx1, by0 - y, roughContext);
      }
      drawMultilineText(grp, e.text, bx1, y, oddLabel, "end", fontSize);
    }
    if (e.childCount > 0) {
      bones.set(i, {
        x0: bx0,
        y0: by0,
        x1: bx1,
        y1: y,
        childCount: e.childCount,
        childrenDrawn: 0
      });
    }
  }
}, "drawBranch");
var splitLines = /* @__PURE__ */ __name((text) => text.split(/<br\s*\/?>|\n/), "splitLines");
var wrapText = /* @__PURE__ */ __name((text, maxChars) => {
  if (text.length <= maxChars) {
    return text;
  }
  const lines = [];
  for (const word of text.split(/\s+/)) {
    const last = lines.length - 1;
    if (last >= 0 && lines[last].length + 1 + word.length <= maxChars) {
      lines[last] += " " + word;
    } else {
      lines.push(word);
    }
  }
  return lines.join("\n");
}, "wrapText");
var drawMultilineText = /* @__PURE__ */ __name((g, text, x, y, cls, anchor, fontSize) => {
  const lines = splitLines(text);
  const lh = fontSize * 1.05;
  const el = g.append("text").attr("class", cls).attr("text-anchor", anchor).attr("x", x).attr("y", y - (lines.length - 1) * lh / 2);
  for (const [i, line] of lines.entries()) {
    el.append("tspan").attr("x", x).attr("dy", i === 0 ? 0 : lh).text(line);
  }
  return el;
}, "drawMultilineText");
var lerp = /* @__PURE__ */ __name((a, b, t) => a + (b - a) * t, "lerp");
var drawLine = /* @__PURE__ */ __name((g, x1, y1, x2, y2, cls, roughContext) => {
  if (roughContext) {
    const roughNode = roughContext.roughSvg.line(x1, y1, x2, y2, {
      roughness: 1.5,
      seed: roughContext.seed,
      stroke: roughContext.lineColor,
      strokeWidth: 2
    });
    g.append(() => roughNode).attr("class", cls);
    return void 0;
  }
  return g.append("line").attr("class", cls).attr("x1", x1).attr("y1", y1).attr("x2", x2).attr("y2", y2);
}, "drawLine");
var renderer = { draw };

// src/diagrams/ishikawa/ishikawaStyles.ts
var getStyles = /* @__PURE__ */ __name((options) => `
.ishikawa .ishikawa-spine,
.ishikawa .ishikawa-branch,
.ishikawa .ishikawa-sub-branch {
  stroke: ${options.lineColor};
  stroke-width: 2;
  fill: none;
}

.ishikawa .ishikawa-sub-branch {
  stroke-width: 1;
}

.ishikawa .ishikawa-arrow {
  fill: ${options.lineColor};
}

.ishikawa .ishikawa-head {
  fill: ${options.mainBkg};
  stroke: ${options.lineColor};
  stroke-width: 2;
}

.ishikawa .ishikawa-label-box {
  fill: ${options.mainBkg};
  stroke: ${options.lineColor};
  stroke-width: 2;
}

.ishikawa text {
  font-family: ${options.fontFamily};
  font-size: ${options.fontSize};
  fill: ${options.textColor};
}

.ishikawa .ishikawa-head-label {
  font-weight: 600;
  text-anchor: middle;
  dominant-baseline: middle;
  font-size: 14px;
}

.ishikawa .ishikawa-label {
  text-anchor: end;
}

.ishikawa .ishikawa-label.cause {
  text-anchor: middle;
  dominant-baseline: middle;
}

.ishikawa .ishikawa-label.align {
  text-anchor: end;
  dominant-baseline: middle;
}

.ishikawa .ishikawa-label.up {
  dominant-baseline: baseline;
}

.ishikawa .ishikawa-label.down {
  dominant-baseline: hanging;
}
`, "getStyles");
var ishikawaStyles_default = getStyles;

// src/diagrams/ishikawa/ishikawaDiagram.ts
var diagram = {
  parser: ishikawa_default,
  get db() {
    return new IshikawaDB();
  },
  renderer,
  styles: ishikawaStyles_default
};
export {
  diagram
};
