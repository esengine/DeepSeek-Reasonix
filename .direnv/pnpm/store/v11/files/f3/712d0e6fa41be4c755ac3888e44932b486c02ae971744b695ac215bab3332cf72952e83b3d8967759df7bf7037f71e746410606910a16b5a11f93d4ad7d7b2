import {
  selectSvgElement
} from "./chunk-VAUOI2AC.mjs";
import {
  parseFontSize
} from "./chunk-ICXQ74PX.mjs";
import {
  clear,
  commonDb_exports,
  getConfig,
  getConfig2,
  setupGraphViewbox
} from "./chunk-WYO6CB5R.mjs";
import {
  log
} from "./chunk-X3CZISLH.mjs";
import {
  __export,
  __name
} from "./chunk-Y2CYZVJY.mjs";

// src/diagrams/timeline/parser/timeline.jison
var parser = (function() {
  var o = /* @__PURE__ */ __name(function(k, v, o2, l) {
    for (o2 = o2 || {}, l = k.length; l--; o2[k[l]] = v) ;
    return o2;
  }, "o"), $V0 = [6, 11, 13, 14, 15, 17, 19, 20, 23, 24], $V1 = [1, 12], $V2 = [1, 13], $V3 = [1, 14], $V4 = [1, 15], $V5 = [1, 16], $V6 = [1, 19], $V7 = [1, 20];
  var parser2 = {
    trace: /* @__PURE__ */ __name(function trace() {
    }, "trace"),
    yy: {},
    symbols_: { "error": 2, "start": 3, "timeline_header": 4, "document": 5, "EOF": 6, "timeline": 7, "timeline_lr": 8, "timeline_td": 9, "line": 10, "SPACE": 11, "statement": 12, "NEWLINE": 13, "title": 14, "acc_title": 15, "acc_title_value": 16, "acc_descr": 17, "acc_descr_value": 18, "acc_descr_multiline_value": 19, "section": 20, "period_statement": 21, "event_statement": 22, "period": 23, "event": 24, "$accept": 0, "$end": 1 },
    terminals_: { 2: "error", 6: "EOF", 7: "timeline", 8: "timeline_lr", 9: "timeline_td", 11: "SPACE", 13: "NEWLINE", 14: "title", 15: "acc_title", 16: "acc_title_value", 17: "acc_descr", 18: "acc_descr_value", 19: "acc_descr_multiline_value", 20: "section", 23: "period", 24: "event" },
    productions_: [0, [3, 3], [4, 1], [4, 1], [4, 1], [5, 0], [5, 2], [10, 2], [10, 1], [10, 1], [10, 1], [12, 1], [12, 2], [12, 2], [12, 1], [12, 1], [12, 1], [12, 1], [21, 1], [22, 1]],
    performAction: /* @__PURE__ */ __name(function anonymous(yytext, yyleng, yylineno, yy, yystate, $$, _$) {
      var $0 = $$.length - 1;
      switch (yystate) {
        case 1:
          return $$[$0 - 1];
          break;
        case 3:
          yy.setDirection("LR");
          break;
        case 4:
          yy.setDirection("TD");
          break;
        case 5:
          this.$ = [];
          break;
        case 6:
          $$[$0 - 1].push($$[$0]);
          this.$ = $$[$0 - 1];
          break;
        case 7:
        case 8:
          this.$ = $$[$0];
          break;
        case 9:
        case 10:
          this.$ = [];
          break;
        case 11:
          yy.getCommonDb().setDiagramTitle($$[$0].substr(6));
          this.$ = $$[$0].substr(6);
          break;
        case 12:
          this.$ = $$[$0].trim();
          yy.getCommonDb().setAccTitle(this.$);
          break;
        case 13:
        case 14:
          this.$ = $$[$0].trim();
          yy.getCommonDb().setAccDescription(this.$);
          break;
        case 15:
          yy.addSection($$[$0].substr(8));
          this.$ = $$[$0].substr(8);
          break;
        case 18:
          yy.addTask($$[$0], 0, "");
          this.$ = $$[$0];
          break;
        case 19:
          yy.addEvent($$[$0].substr(2));
          this.$ = $$[$0];
          break;
      }
    }, "anonymous"),
    table: [{ 3: 1, 4: 2, 7: [1, 3], 8: [1, 4], 9: [1, 5] }, { 1: [3] }, o($V0, [2, 5], { 5: 6 }), o($V0, [2, 2]), o($V0, [2, 3]), o($V0, [2, 4]), { 6: [1, 7], 10: 8, 11: [1, 9], 12: 10, 13: [1, 11], 14: $V1, 15: $V2, 17: $V3, 19: $V4, 20: $V5, 21: 17, 22: 18, 23: $V6, 24: $V7 }, o($V0, [2, 10], { 1: [2, 1] }), o($V0, [2, 6]), { 12: 21, 14: $V1, 15: $V2, 17: $V3, 19: $V4, 20: $V5, 21: 17, 22: 18, 23: $V6, 24: $V7 }, o($V0, [2, 8]), o($V0, [2, 9]), o($V0, [2, 11]), { 16: [1, 22] }, { 18: [1, 23] }, o($V0, [2, 14]), o($V0, [2, 15]), o($V0, [2, 16]), o($V0, [2, 17]), o($V0, [2, 18]), o($V0, [2, 19]), o($V0, [2, 7]), o($V0, [2, 12]), o($V0, [2, 13])],
    defaultActions: {},
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
            break;
          case 1:
            break;
          case 2:
            return 13;
            break;
          case 3:
            break;
          case 4:
            break;
          case 5:
            return 8;
            break;
          case 6:
            return 9;
            break;
          case 7:
            return 7;
            break;
          case 8:
            return 14;
            break;
          case 9:
            this.begin("acc_title");
            return 15;
            break;
          case 10:
            this.popState();
            return "acc_title_value";
            break;
          case 11:
            this.begin("acc_descr");
            return 17;
            break;
          case 12:
            this.popState();
            return "acc_descr_value";
            break;
          case 13:
            this.begin("acc_descr_multiline");
            break;
          case 14:
            this.popState();
            break;
          case 15:
            return "acc_descr_multiline_value";
            break;
          case 16:
            return 20;
            break;
          case 17:
            return 24;
            break;
          case 18:
            return 23;
            break;
          case 19:
            return 6;
            break;
          case 20:
            return "INVALID";
            break;
        }
      }, "anonymous"),
      rules: [/^(?:%(?!\{)[^\n]*)/i, /^(?:[^\}]%%[^\n]*)/i, /^(?:[\n]+)/i, /^(?:\s+)/i, /^(?:#[^\n]*)/i, /^(?:timeline[ \t]+LR\b)/i, /^(?:timeline[ \t]+TD\b)/i, /^(?:timeline\b)/i, /^(?:title\s[^\n]+)/i, /^(?:accTitle\s*:\s*)/i, /^(?:(?!\n||)*[^\n]*)/i, /^(?:accDescr\s*:\s*)/i, /^(?:(?!\n||)*[^\n]*)/i, /^(?:accDescr\s*\{\s*)/i, /^(?:[\}])/i, /^(?:[^\}]*)/i, /^(?:section\s[^:\n]+)/i, /^(?::\s(?:[^:\n]|:(?!\s))+)/i, /^(?:[^#:\n]+)/i, /^(?:$)/i, /^(?:.)/i],
      conditions: { "acc_descr_multiline": { "rules": [14, 15], "inclusive": false }, "acc_descr": { "rules": [12], "inclusive": false }, "acc_title": { "rules": [10], "inclusive": false }, "INITIAL": { "rules": [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 11, 13, 16, 17, 18, 19, 20], "inclusive": true } }
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
var timeline_default = parser;

// src/diagrams/timeline/timelineDb.js
var timelineDb_exports = {};
__export(timelineDb_exports, {
  addEvent: () => addEvent,
  addSection: () => addSection,
  addTask: () => addTask,
  addTaskOrg: () => addTaskOrg,
  clear: () => clear2,
  default: () => timelineDb_default,
  getCommonDb: () => getCommonDb,
  getDirection: () => getDirection,
  getSections: () => getSections,
  getTasks: () => getTasks,
  setDirection: () => setDirection
});
var currentSection = "";
var currentTaskId = 0;
var direction = "LR";
var sections = [];
var tasks = [];
var rawTasks = [];
var getCommonDb = /* @__PURE__ */ __name(() => commonDb_exports, "getCommonDb");
var clear2 = /* @__PURE__ */ __name(function() {
  sections.length = 0;
  tasks.length = 0;
  currentSection = "";
  rawTasks.length = 0;
  direction = "LR";
  clear();
}, "clear");
var setDirection = /* @__PURE__ */ __name(function(dir) {
  direction = dir;
}, "setDirection");
var getDirection = /* @__PURE__ */ __name(function() {
  return direction;
}, "getDirection");
var addSection = /* @__PURE__ */ __name(function(txt) {
  currentSection = txt;
  sections.push(txt);
}, "addSection");
var getSections = /* @__PURE__ */ __name(function() {
  return sections;
}, "getSections");
var getTasks = /* @__PURE__ */ __name(function() {
  let allItemsProcessed = compileTasks();
  const maxDepth = 100;
  let iterationCount = 0;
  while (!allItemsProcessed && iterationCount < maxDepth) {
    allItemsProcessed = compileTasks();
    iterationCount++;
  }
  tasks.push(...rawTasks);
  return tasks;
}, "getTasks");
var addTask = /* @__PURE__ */ __name(function(period, length, event) {
  const rawTask = {
    id: currentTaskId++,
    section: currentSection,
    type: currentSection,
    task: period,
    score: length ? length : 0,
    //if event is defined, then add it the events array
    events: event ? [event] : []
  };
  rawTasks.push(rawTask);
}, "addTask");
var addEvent = /* @__PURE__ */ __name(function(event) {
  const currentTask = rawTasks.find((task) => task.id === currentTaskId - 1);
  currentTask.events.push(event);
}, "addEvent");
var addTaskOrg = /* @__PURE__ */ __name(function(descr) {
  const newTask = {
    section: currentSection,
    type: currentSection,
    description: descr,
    task: descr,
    classes: []
  };
  tasks.push(newTask);
}, "addTaskOrg");
var compileTasks = /* @__PURE__ */ __name(function() {
  const compileTask = /* @__PURE__ */ __name(function(pos) {
    return rawTasks[pos].processed;
  }, "compileTask");
  let allProcessed = true;
  for (const [i, rawTask] of rawTasks.entries()) {
    compileTask(i);
    allProcessed = allProcessed && rawTask.processed;
  }
  return allProcessed;
}, "compileTasks");
var timelineDb_default = {
  clear: clear2,
  getCommonDb,
  getDirection,
  setDirection,
  addSection,
  getSections,
  getTasks,
  addTask,
  addTaskOrg,
  addEvent
};

// src/diagrams/timeline/timelineRenderer.ts
import { select as select2 } from "d3";

// src/diagrams/timeline/svgDraw.js
import { arc as d3arc, select } from "d3";
var nodeCount = 0;
var drawRect = /* @__PURE__ */ __name(function(elem, rectData) {
  const rectElem = elem.append("rect");
  rectElem.attr("x", rectData.x);
  rectElem.attr("y", rectData.y);
  rectElem.attr("fill", rectData.fill);
  rectElem.attr("stroke", rectData.stroke);
  rectElem.attr("width", rectData.width);
  rectElem.attr("height", rectData.height);
  rectElem.attr("rx", rectData.rx);
  rectElem.attr("ry", rectData.ry);
  if (rectData.class !== void 0) {
    rectElem.attr("class", rectData.class);
  }
  return rectElem;
}, "drawRect");
var drawFace = /* @__PURE__ */ __name(function(element, faceData) {
  const radius = 15;
  const circleElement = element.append("circle").attr("cx", faceData.cx).attr("cy", faceData.cy).attr("class", "face").attr("r", radius).attr("stroke-width", 2).attr("overflow", "visible");
  const face = element.append("g");
  face.append("circle").attr("cx", faceData.cx - radius / 3).attr("cy", faceData.cy - radius / 3).attr("r", 1.5).attr("stroke-width", 2).attr("fill", "#666").attr("stroke", "#666");
  face.append("circle").attr("cx", faceData.cx + radius / 3).attr("cy", faceData.cy - radius / 3).attr("r", 1.5).attr("stroke-width", 2).attr("fill", "#666").attr("stroke", "#666");
  function smile(face2) {
    const arc = d3arc().startAngle(Math.PI / 2).endAngle(3 * (Math.PI / 2)).innerRadius(radius / 2).outerRadius(radius / 2.2);
    face2.append("path").attr("class", "mouth").attr("d", arc).attr("transform", "translate(" + faceData.cx + "," + (faceData.cy + 2) + ")");
  }
  __name(smile, "smile");
  function sad(face2) {
    const arc = d3arc().startAngle(3 * Math.PI / 2).endAngle(5 * (Math.PI / 2)).innerRadius(radius / 2).outerRadius(radius / 2.2);
    face2.append("path").attr("class", "mouth").attr("d", arc).attr("transform", "translate(" + faceData.cx + "," + (faceData.cy + 7) + ")");
  }
  __name(sad, "sad");
  function ambivalent(face2) {
    face2.append("line").attr("class", "mouth").attr("stroke", 2).attr("x1", faceData.cx - 5).attr("y1", faceData.cy + 7).attr("x2", faceData.cx + 5).attr("y2", faceData.cy + 7).attr("class", "mouth").attr("stroke-width", "1px").attr("stroke", "#666");
  }
  __name(ambivalent, "ambivalent");
  if (faceData.score > 3) {
    smile(face);
  } else if (faceData.score < 3) {
    sad(face);
  } else {
    ambivalent(face);
  }
  return circleElement;
}, "drawFace");
var drawCircle = /* @__PURE__ */ __name(function(element, circleData) {
  const circleElement = element.append("circle");
  circleElement.attr("cx", circleData.cx);
  circleElement.attr("cy", circleData.cy);
  circleElement.attr("class", "actor-" + circleData.pos);
  circleElement.attr("fill", circleData.fill);
  circleElement.attr("stroke", circleData.stroke);
  circleElement.attr("r", circleData.r);
  if (circleElement.class !== void 0) {
    circleElement.attr("class", circleElement.class);
  }
  if (circleData.title !== void 0) {
    circleElement.append("title").text(circleData.title);
  }
  return circleElement;
}, "drawCircle");
var drawText = /* @__PURE__ */ __name(function(elem, textData) {
  const nText = textData.text.replace(/<br\s*\/?>/gi, " ");
  const textElem = elem.append("text");
  textElem.attr("x", textData.x);
  textElem.attr("y", textData.y);
  textElem.attr("class", "legend");
  textElem.style("text-anchor", textData.anchor);
  if (textData.class !== void 0) {
    textElem.attr("class", textData.class);
  }
  const span = textElem.append("tspan");
  span.attr("x", textData.x + textData.textMargin * 2);
  span.text(nText);
  return textElem;
}, "drawText");
var drawLabel = /* @__PURE__ */ __name(function(elem, txtObject) {
  function genPoints(x, y, width, height, cut) {
    return x + "," + y + " " + (x + width) + "," + y + " " + (x + width) + "," + (y + height - cut) + " " + (x + width - cut * 1.2) + "," + (y + height) + " " + x + "," + (y + height);
  }
  __name(genPoints, "genPoints");
  const polygon = elem.append("polygon");
  polygon.attr("points", genPoints(txtObject.x, txtObject.y, 50, 20, 7));
  polygon.attr("class", "labelBox");
  txtObject.y = txtObject.y + txtObject.labelMargin;
  txtObject.x = txtObject.x + 0.5 * txtObject.labelMargin;
  drawText(elem, txtObject);
}, "drawLabel");
var drawSection = /* @__PURE__ */ __name(function(elem, section, conf) {
  const g = elem.append("g");
  const rect = getNoteRect();
  rect.x = section.x;
  rect.y = section.y;
  rect.fill = section.fill;
  rect.width = conf.width;
  rect.height = conf.height;
  rect.class = "journey-section section-type-" + section.num;
  rect.rx = 3;
  rect.ry = 3;
  drawRect(g, rect);
  _drawTextCandidateFunc(conf)(
    section.text,
    g,
    rect.x,
    rect.y,
    rect.width,
    rect.height,
    { class: "journey-section section-type-" + section.num },
    conf,
    section.colour
  );
}, "drawSection");
var taskCount = -1;
var drawTask = /* @__PURE__ */ __name(function(elem, task, conf, diagramId) {
  const center = task.x + conf.width / 2;
  const g = elem.append("g");
  taskCount++;
  const maxHeight = 300 + 5 * 30;
  g.append("line").attr("id", diagramId + "-task" + taskCount).attr("x1", center).attr("y1", task.y).attr("x2", center).attr("y2", maxHeight).attr("class", "task-line").attr("stroke-width", "1px").attr("stroke-dasharray", "4 2").attr("stroke", "#666");
  drawFace(g, {
    cx: center,
    cy: 300 + (5 - task.score) * 30,
    score: task.score
  });
  const rect = getNoteRect();
  rect.x = task.x;
  rect.y = task.y;
  rect.fill = task.fill;
  rect.width = conf.width;
  rect.height = conf.height;
  rect.class = "task task-type-" + task.num;
  rect.rx = 3;
  rect.ry = 3;
  drawRect(g, rect);
  _drawTextCandidateFunc(conf)(
    task.task,
    g,
    rect.x,
    rect.y,
    rect.width,
    rect.height,
    { class: "task" },
    conf,
    task.colour
  );
}, "drawTask");
var drawBackgroundRect = /* @__PURE__ */ __name(function(elem, bounds) {
  const rectElem = drawRect(elem, {
    x: bounds.startx,
    y: bounds.starty,
    width: bounds.stopx - bounds.startx,
    height: bounds.stopy - bounds.starty,
    fill: bounds.fill,
    class: "rect"
  });
  rectElem.lower();
}, "drawBackgroundRect");
var getTextObj = /* @__PURE__ */ __name(function() {
  return {
    x: 0,
    y: 0,
    fill: void 0,
    "text-anchor": "start",
    width: 100,
    height: 100,
    textMargin: 0,
    rx: 0,
    ry: 0
  };
}, "getTextObj");
var getNoteRect = /* @__PURE__ */ __name(function() {
  return {
    x: 0,
    y: 0,
    width: 100,
    anchor: "start",
    height: 100,
    rx: 0,
    ry: 0
  };
}, "getNoteRect");
var _drawTextCandidateFunc = /* @__PURE__ */ (function() {
  function byText(content, g, x, y, width, height, textAttrs, colour) {
    const text = g.append("text").attr("x", x + width / 2).attr("y", y + height / 2 + 5).style("font-color", colour).style("text-anchor", "middle").text(content);
    _setTextAttrs(text, textAttrs);
  }
  __name(byText, "byText");
  function byTspan(content, g, x, y, width, height, textAttrs, conf, colour) {
    const { taskFontSize, taskFontFamily } = conf;
    const lines = content.split(/<br\s*\/?>/gi);
    for (let i = 0; i < lines.length; i++) {
      const dy = i * taskFontSize - taskFontSize * (lines.length - 1) / 2;
      const text = g.append("text").attr("x", x + width / 2).attr("y", y).attr("fill", colour).style("text-anchor", "middle").style("font-size", taskFontSize).style("font-family", taskFontFamily);
      text.append("tspan").attr("x", x + width / 2).attr("dy", dy).text(lines[i]);
      text.attr("y", y + height / 2).attr("dominant-baseline", "central").attr("alignment-baseline", "central");
      _setTextAttrs(text, textAttrs);
    }
  }
  __name(byTspan, "byTspan");
  function byFo(content, g, x, y, width, height, textAttrs, conf) {
    const body = g.append("switch");
    const f = body.append("foreignObject").attr("x", x).attr("y", y).attr("width", width).attr("height", height).attr("position", "fixed");
    const text = f.append("xhtml:div").style("display", "table").style("height", "100%").style("width", "100%");
    text.append("div").attr("class", "label").style("display", "table-cell").style("text-align", "center").style("vertical-align", "middle").text(content);
    byTspan(content, body, x, y, width, height, textAttrs, conf);
    _setTextAttrs(text, textAttrs);
  }
  __name(byFo, "byFo");
  function _setTextAttrs(toText, fromTextAttrsDict) {
    for (const key in fromTextAttrsDict) {
      if (key in fromTextAttrsDict) {
        toText.attr(key, fromTextAttrsDict[key]);
      }
    }
  }
  __name(_setTextAttrs, "_setTextAttrs");
  return function(conf) {
    return conf.textPlacement === "fo" ? byFo : conf.textPlacement === "old" ? byText : byTspan;
  };
})();
var initGraphics = /* @__PURE__ */ __name(function(graphics, id) {
  nodeCount = 0;
  taskCount = -1;
  graphics.append("defs").append("marker").attr("id", id + "-arrowhead").attr("refX", 5).attr("refY", 2).attr("markerWidth", 6).attr("markerHeight", 4).attr("orient", "auto").append("path").attr("d", "M 0,0 V 4 L6,2 Z");
}, "initGraphics");
function wrap(text, width) {
  text.each(function() {
    var text2 = select(this), words = text2.text().split(/(\s+|<br>)/).reverse(), word, line = [], lineHeight = 1.1, y = text2.attr("y"), dy = parseFloat(text2.attr("dy")), tspan = text2.text(null).append("tspan").attr("x", 0).attr("y", y).attr("dy", dy + "em");
    for (let j = 0; j < words.length; j++) {
      word = words[words.length - 1 - j];
      line.push(word);
      tspan.text(line.join(" ").trim());
      if (tspan.node().getComputedTextLength() > width || word === "<br>") {
        line.pop();
        tspan.text(line.join(" ").trim());
        if (word === "<br>") {
          line = [""];
        } else {
          line = [word];
        }
        tspan = text2.append("tspan").attr("x", 0).attr("y", y).attr("dy", lineHeight + "em").text(word);
      }
    }
  });
}
__name(wrap, "wrap");
var drawNode = /* @__PURE__ */ __name(function(elem, node, fullSection, conf, diagramId, isEvent = false) {
  const { theme, look } = conf;
  const isReduxTheme = theme?.includes("redux");
  const maxSections = conf?.themeVariables?.THEME_COLOR_LIMIT ?? 12;
  const section = fullSection % maxSections - 1;
  const nodeElem = elem.append("g");
  node.section = section;
  nodeElem.attr(
    "class",
    (node.class ? node.class + " " : "") + "timeline-node " + ("section-" + section)
  );
  const bkgElem = nodeElem.append("g");
  const textElem = nodeElem.append("g");
  const txt = textElem.append("text").text(node.descr).attr("dy", "1em").attr("alignment-baseline", "middle").attr("dominant-baseline", "middle").attr("text-anchor", "middle").call(wrap, node.width);
  const bbox = txt.node().getBBox();
  const fontSize = conf.fontSize?.replace ? conf.fontSize.replace("px", "") : conf.fontSize;
  node.height = bbox.height + fontSize * 1.1 * 0.5 + node.padding;
  node.height = Math.max(node.height, node.maxHeight);
  node.width = node.width + 2 * node.padding;
  textElem.attr("transform", "translate(" + node.width / 2 + ", " + node.padding / 2 + ")");
  if (isReduxTheme) {
    textElem.attr(
      "transform",
      `translate(${node.width / 2}, ${isEvent ? node.padding / 2 + 3 : node.padding})`
    );
  }
  defaultBkg(bkgElem, node, section, diagramId, conf);
  if (look === "neo") {
    nodeElem.attr("data-look", `neo`);
    if (isReduxTheme) {
      const isDark2 = theme.includes("dark");
      const rootSvgNode = elem.node()?.ownerSVGElement ?? elem.node();
      const rootSvg = select(rootSvgNode);
      const svgId = rootSvg.attr("id") ?? "";
      const dropShadowId = svgId ? `${svgId}-drop-shadow` : "drop-shadow";
      if (rootSvg.select(`#${dropShadowId}`).empty()) {
        const existingDefs = rootSvg.select("defs");
        const defsEl = existingDefs.empty() ? rootSvg.append("defs") : existingDefs;
        defsEl.append("filter").attr("id", dropShadowId).attr("height", "130%").attr("width", "130%").append("feDropShadow").attr("dx", "4").attr("dy", "4").attr("stdDeviation", 0).attr("flood-opacity", isDark2 ? "0.2" : "0.06").attr("flood-color", isDark2 ? "#FFFFFF" : "#000000");
      }
    }
  }
  return node;
}, "drawNode");
var getVirtualNodeHeight = /* @__PURE__ */ __name(function(elem, node, conf) {
  const textElem = elem.append("g");
  const txt = textElem.append("text").text(node.descr).attr("dy", "1em").attr("alignment-baseline", "middle").attr("dominant-baseline", "middle").attr("text-anchor", "middle").call(wrap, node.width);
  const bbox = txt.node().getBBox();
  const fontSize = conf.fontSize?.replace ? conf.fontSize.replace("px", "") : conf.fontSize;
  textElem.remove();
  return bbox.height + fontSize * 1.1 * 0.5 + node.padding;
}, "getVirtualNodeHeight");
var defaultBkg = /* @__PURE__ */ __name(function(elem, node, section, diagramId, config) {
  const { theme } = config;
  const r = theme?.includes("redux") ? 0 : 5;
  const rd = 5;
  const d = r > 0 ? `M0 ${node.height - rd} v${-node.height + 2 * rd} q0,-${r},${r},-${r} h${node.width - 2 * rd} q${r},0,${r},${r} v${node.height - rd} H0 Z` : `M0 ${node.height - rd} v${-(node.height - rd)} h${node.width} v${node.height} H0 Z`;
  elem.append("path").attr("id", diagramId + "-node-" + nodeCount++).attr("class", "node-bkg node-" + node.type).attr("d", d);
  if (!theme?.includes("redux")) {
    elem.append("line").attr("class", "node-line-" + section).attr("x1", 0).attr("y1", node.height).attr("x2", node.width).attr("y2", node.height);
  }
}, "defaultBkg");
var svgDraw_default = {
  drawRect,
  drawCircle,
  drawSection,
  drawText,
  drawLabel,
  drawTask,
  drawBackgroundRect,
  getTextObj,
  getNoteRect,
  initGraphics,
  drawNode,
  getVirtualNodeHeight
};

// src/diagrams/timeline/timelineRenderer.ts
var draw = /* @__PURE__ */ __name(function(text, id, version, diagObj) {
  const conf = getConfig2();
  const { look, theme, themeVariables } = conf;
  const { useGradient, gradientStart, gradientStop } = themeVariables;
  const LEFT_MARGIN = conf.timeline?.leftMargin ?? 50;
  log.debug("timeline", diagObj.db);
  const securityLevel = conf.securityLevel;
  let sandboxElement;
  if (securityLevel === "sandbox") {
    sandboxElement = select2("#i" + id);
  }
  const root = securityLevel === "sandbox" ? select2(sandboxElement.nodes()[0].contentDocument.body) : select2("body");
  const svg = root.select("#" + id);
  svg.append("g");
  const tasks2 = diagObj.db.getTasks();
  const title = diagObj.db.getCommonDb().getDiagramTitle();
  log.debug("task", tasks2);
  svgDraw_default.initGraphics(svg, id);
  const sections2 = diagObj.db.getSections();
  log.debug("sections", sections2);
  let maxSectionHeight = 0;
  let maxTaskHeight = 0;
  let depthY = 0;
  let sectionBeginY = 0;
  let masterX = 50 + LEFT_MARGIN;
  let masterY = 50;
  sectionBeginY = 50;
  let sectionNumber = 0;
  let hasSections = true;
  sections2.forEach(function(section) {
    const sectionNode = {
      number: sectionNumber,
      descr: section,
      section: sectionNumber,
      width: 150,
      padding: 20,
      maxHeight: maxSectionHeight
    };
    const sectionHeight = svgDraw_default.getVirtualNodeHeight(svg, sectionNode, conf);
    log.debug("sectionHeight before draw", sectionHeight);
    maxSectionHeight = Math.max(maxSectionHeight, sectionHeight + 20);
  });
  let maxEventCount = 0;
  let maxEventLineLength = 0;
  log.debug("tasks.length", tasks2.length);
  for (const [i, task] of tasks2.entries()) {
    const taskNode = {
      number: i,
      descr: task,
      section: task.section,
      width: 150,
      padding: 20,
      maxHeight: maxTaskHeight
    };
    const taskHeight = svgDraw_default.getVirtualNodeHeight(svg, taskNode, conf);
    log.debug("taskHeight before draw", taskHeight);
    maxTaskHeight = Math.max(maxTaskHeight, taskHeight + 20);
    maxEventCount = Math.max(maxEventCount, task.events.length);
    let maxEventLineLengthTemp = 0;
    for (const event of task.events) {
      const eventNode = {
        descr: event,
        section: task.section,
        number: task.section,
        width: 150,
        padding: 20,
        maxHeight: 50
      };
      maxEventLineLengthTemp += svgDraw_default.getVirtualNodeHeight(svg, eventNode, conf);
    }
    if (task.events.length > 0) {
      maxEventLineLengthTemp += (task.events.length - 1) * 10;
    }
    maxEventLineLength = Math.max(maxEventLineLength, maxEventLineLengthTemp);
  }
  log.debug("maxSectionHeight before draw", maxSectionHeight);
  log.debug("maxTaskHeight before draw", maxTaskHeight);
  if (sections2 && sections2.length > 0) {
    sections2.forEach((section) => {
      const tasksForSection = tasks2.filter((task) => task.section === section);
      const sectionNode = {
        number: sectionNumber,
        descr: section,
        section: sectionNumber,
        width: 200 * Math.max(tasksForSection.length, 1) - 50,
        padding: 20,
        maxHeight: maxSectionHeight
      };
      log.debug("sectionNode", sectionNode);
      const sectionNodeWrapper = svg.append("g");
      const node = svgDraw_default.drawNode(sectionNodeWrapper, sectionNode, sectionNumber, conf, id);
      log.debug("sectionNode output", node);
      sectionNodeWrapper.attr("transform", `translate(${masterX}, ${sectionBeginY})`);
      masterY += maxSectionHeight + 50;
      if (tasksForSection.length > 0) {
        drawTasks(
          svg,
          tasksForSection,
          sectionNumber,
          masterX,
          masterY,
          maxTaskHeight,
          conf,
          maxEventCount,
          maxEventLineLength,
          maxSectionHeight,
          false,
          id
        );
      }
      masterX += 200 * Math.max(tasksForSection.length, 1);
      masterY = sectionBeginY;
      sectionNumber++;
    });
  } else {
    hasSections = false;
    drawTasks(
      svg,
      tasks2,
      sectionNumber,
      masterX,
      masterY,
      maxTaskHeight,
      conf,
      maxEventCount,
      maxEventLineLength,
      maxSectionHeight,
      true,
      id
    );
  }
  const box = svg.node().getBBox();
  log.debug("bounds", box);
  if (title) {
    svg.append("text").text(title).attr("x", look === "neo" ? box.x * 2 + LEFT_MARGIN : box.width / 2 - LEFT_MARGIN).attr("font-size", "4ex").attr("font-weight", "bold").attr("y", 20);
  }
  depthY = hasSections ? maxSectionHeight + maxTaskHeight + 150 : maxTaskHeight + 100;
  const lineWrapper = svg.append("g").attr("class", "lineWrapper");
  lineWrapper.append("line").attr("x1", LEFT_MARGIN).attr("y1", depthY).attr("x2", box.width + 3 * LEFT_MARGIN).attr("y2", depthY).attr("stroke-width", 4).attr("stroke", "black").attr("marker-end", `url(#${id}-arrowhead)`);
  if (look === "neo" && useGradient && theme !== "neutral") {
    const existingDefs = svg.select("defs");
    const defsEl = existingDefs.empty() ? svg.append("defs") : existingDefs;
    const gradient = defsEl.append("linearGradient").attr("id", svg.attr("id") + "-gradient").attr("gradientUnits", "objectBoundingBox").attr("x1", "0%").attr("y1", "0%").attr("x2", "100%").attr("y2", "0%");
    gradient.append("stop").attr("offset", "0%").attr("stop-color", gradientStart).attr("stop-opacity", 1);
    gradient.append("stop").attr("offset", "100%").attr("stop-color", gradientStop).attr("stop-opacity", 1);
  }
  setupGraphViewbox(
    void 0,
    svg,
    conf.timeline?.padding ?? 50,
    conf.timeline?.useMaxWidth ?? false
  );
}, "draw");
var drawTasks = /* @__PURE__ */ __name(function(diagram2, tasks2, sectionColor, masterX, masterY, maxTaskHeight, conf, maxEventCount, maxEventLineLength, maxSectionHeight, isWithoutSections, diagramId) {
  for (const task of tasks2) {
    const taskNode = {
      descr: task.task,
      section: sectionColor,
      number: sectionColor,
      width: 150,
      padding: 20,
      maxHeight: maxTaskHeight
    };
    log.debug("taskNode", taskNode);
    const taskWrapper = diagram2.append("g").attr("class", "taskWrapper");
    const node = svgDraw_default.drawNode(taskWrapper, taskNode, sectionColor, conf, diagramId);
    const taskHeight = node.height;
    log.debug("taskHeight after draw", taskHeight);
    taskWrapper.attr("transform", `translate(${masterX}, ${masterY})`);
    maxTaskHeight = Math.max(maxTaskHeight, taskHeight);
    if (task.events) {
      const lineWrapper = diagram2.append("g").attr("class", "lineWrapper");
      let lineLength = maxTaskHeight;
      masterY += 100;
      lineLength = lineLength + drawEvents(diagram2, task.events, sectionColor, masterX, masterY, conf, diagramId);
      masterY -= 100;
      lineWrapper.append("line").attr("x1", masterX + 190 / 2).attr("y1", masterY + maxTaskHeight).attr("x2", masterX + 190 / 2).attr("y2", masterY + maxTaskHeight + 100 + maxEventLineLength + 100).attr("stroke-width", 2).attr("stroke", "black").attr("marker-end", `url(#${diagramId}-arrowhead)`).attr("stroke-dasharray", "5,5");
    }
    masterX = masterX + 200;
    if (isWithoutSections && !conf.timeline?.disableMulticolor) {
      sectionColor++;
    }
  }
  masterY = masterY - 10;
}, "drawTasks");
var drawEvents = /* @__PURE__ */ __name(function(diagram2, events, sectionColor, masterX, masterY, conf, diagramId) {
  let maxEventHeight = 0;
  const eventBeginY = masterY;
  masterY = masterY + 100;
  for (const event of events) {
    const eventNode = {
      descr: event,
      section: sectionColor,
      number: sectionColor,
      width: 150,
      padding: 20,
      maxHeight: 50
    };
    log.debug("eventNode", eventNode);
    const eventWrapper = diagram2.append("g").attr("class", "eventWrapper");
    const node = svgDraw_default.drawNode(eventWrapper, eventNode, sectionColor, conf, diagramId, true);
    const eventHeight = node.height;
    maxEventHeight = maxEventHeight + eventHeight;
    eventWrapper.attr("transform", `translate(${masterX}, ${masterY})`);
    masterY = masterY + 10 + eventHeight;
  }
  masterY = eventBeginY;
  return maxEventHeight;
}, "drawEvents");
var timelineRenderer_default = {
  setConf: /* @__PURE__ */ __name(() => {
  }, "setConf"),
  draw
};

// src/diagrams/timeline/timelineRendererVertical.ts
var NODE_WIDTH = 200;
var NODE_PADDING = 5;
var NODE_TOTAL_WIDTH = NODE_WIDTH + NODE_PADDING * 2;
var EVENT_WIDTH = NODE_WIDTH + 100;
var EVENT_TOTAL_WIDTH = EVENT_WIDTH + NODE_PADDING * 2;
var EVENT_SPACING = 10;
var EVENT_VERTICAL_GAP = 0;
var SECTION_TASK_GAP = 20;
var TASK_AXIS_GAP = 20;
var TASK_VERTICAL_GAP = 30;
var EVENT_AXIS_GAP = 50;
var draw2 = /* @__PURE__ */ __name(function(text, id, version, diagObj) {
  const conf = getConfig2();
  const LEFT_MARGIN = conf.timeline?.leftMargin ?? 50;
  log.debug("timeline", diagObj.db);
  const svg = selectSvgElement(id);
  svg.append("g");
  const tasks2 = diagObj.db.getTasks();
  const title = diagObj.db.getCommonDb().getDiagramTitle();
  log.debug("task", tasks2);
  svgDraw_default.initGraphics(svg);
  const sections2 = diagObj.db.getSections();
  log.debug("sections", sections2);
  let maxSectionHeight = 0;
  let maxTaskHeight = 0;
  const masterX = 50 + LEFT_MARGIN;
  let masterY = 50;
  const contentTopY = masterY;
  const sectionBeginX = masterX;
  const leftWidth = NODE_TOTAL_WIDTH + TASK_AXIS_GAP;
  const rightWidth = EVENT_TOTAL_WIDTH + EVENT_AXIS_GAP;
  const axisX = sectionBeginX + leftWidth;
  let sectionNumber = 0;
  const hasSections = sections2 && sections2.length > 0;
  const timelineX = hasSections ? axisX : masterX + leftWidth;
  const sectionWidth = Math.max(50, leftWidth + rightWidth - NODE_PADDING * 2);
  sections2.forEach(function(section) {
    const sectionNode = {
      number: sectionNumber,
      descr: section,
      section: sectionNumber,
      width: sectionWidth,
      padding: NODE_PADDING,
      maxHeight: maxSectionHeight
    };
    const sectionHeight = svgDraw_default.getVirtualNodeHeight(svg, sectionNode, conf);
    log.debug("sectionHeight before draw", sectionHeight);
    maxSectionHeight = Math.max(maxSectionHeight, sectionHeight);
  });
  let maxEventStackHeight = 0;
  log.debug("tasks.length", tasks2.length);
  for (const [i, task] of tasks2.entries()) {
    const taskNode = {
      number: i,
      descr: task,
      section: task.section,
      width: NODE_WIDTH,
      padding: NODE_PADDING,
      maxHeight: maxTaskHeight
    };
    const taskHeight = svgDraw_default.getVirtualNodeHeight(svg, taskNode, conf);
    log.debug("taskHeight before draw", taskHeight);
    maxTaskHeight = Math.max(maxTaskHeight, taskHeight);
    let maxEventStackHeightTemp = 0;
    for (const event of task.events) {
      const eventNode = {
        descr: event,
        section: task.section,
        number: task.section,
        width: EVENT_WIDTH,
        padding: NODE_PADDING,
        maxHeight: 50
      };
      maxEventStackHeightTemp += svgDraw_default.getVirtualNodeHeight(svg, eventNode, conf);
    }
    if (task.events.length > 0) {
      maxEventStackHeightTemp += (task.events.length - 1) * EVENT_SPACING;
    }
    maxEventStackHeight = Math.max(maxEventStackHeight, maxEventStackHeightTemp) + EVENT_VERTICAL_GAP;
  }
  log.debug("maxSectionHeight before draw", maxSectionHeight);
  log.debug("maxTaskHeight before draw", maxTaskHeight);
  const taskBlockHeight = Math.max(maxTaskHeight, maxEventStackHeight);
  const taskSpacing = taskBlockHeight + TASK_VERTICAL_GAP;
  if (hasSections) {
    sections2.forEach((section) => {
      const tasksForSection = tasks2.filter((task) => task.section === section);
      const sectionNode = {
        number: sectionNumber,
        descr: section,
        section: sectionNumber,
        width: sectionWidth,
        padding: NODE_PADDING,
        maxHeight: maxSectionHeight
      };
      log.debug("sectionNode", sectionNode);
      const sectionNodeWrapper = svg.append("g");
      const node = svgDraw_default.drawNode(sectionNodeWrapper, sectionNode, sectionNumber, conf);
      log.debug("sectionNode output", node);
      const sectionX = timelineX - leftWidth;
      sectionNodeWrapper.attr("transform", `translate(${sectionX}, ${masterY})`);
      const taskStartY = masterY + node.height + SECTION_TASK_GAP;
      if (tasksForSection.length > 0) {
        drawTasks2(
          svg,
          tasksForSection,
          sectionNumber,
          timelineX,
          taskStartY,
          maxTaskHeight,
          conf,
          taskSpacing,
          false
        );
      }
      const taskCount2 = tasksForSection.length;
      const sectionHeight = node.height + SECTION_TASK_GAP + taskSpacing * Math.max(taskCount2, 1) - (taskCount2 > 0 ? TASK_VERTICAL_GAP * 2 : 0);
      masterY += sectionHeight;
      sectionNumber++;
    });
  } else {
    drawTasks2(
      svg,
      tasks2,
      sectionNumber,
      timelineX,
      masterY,
      maxTaskHeight,
      conf,
      taskSpacing,
      true
    );
  }
  let box = svg.node()?.getBBox();
  if (!box) {
    throw new Error("bbox not found");
  }
  log.debug("bounds", box);
  if (title) {
    svg.append("text").text(title).attr("x", box.width / 2 - LEFT_MARGIN).attr("font-size", "4ex").attr("font-weight", "bold").attr("y", 20);
    box = svg.node()?.getBBox();
    if (!box) {
      throw new Error("bbox not found");
    }
    log.debug("bounds after title", box);
  }
  const [fontSize] = parseFontSize(conf.fontSize);
  const arrowTopOffset = (fontSize ?? 16) * 2;
  const arrowBottomPadding = (fontSize ?? 16) * 0.5 + 20;
  const lineWrapper = svg.append("g").attr("class", "lineWrapper");
  lineWrapper.append("line").attr("x1", timelineX).attr("y1", contentTopY - arrowTopOffset).attr("x2", timelineX).attr("y2", box.y + box.height + arrowBottomPadding).attr("stroke-width", 4).attr("stroke", "black").attr("marker-end", "url(#arrowhead)");
  lineWrapper.lower();
  setupGraphViewbox(
    void 0,
    svg,
    conf.timeline?.padding ?? 50,
    conf.timeline?.useMaxWidth ?? false
  );
}, "draw");
var drawTasks2 = /* @__PURE__ */ __name(function(diagram2, tasks2, sectionColor, timelineX, masterY, maxTaskHeight, conf, taskSpacing, isWithoutSections) {
  for (const task of tasks2) {
    const taskNode = {
      descr: task.task,
      section: sectionColor,
      number: sectionColor,
      width: NODE_WIDTH,
      padding: NODE_PADDING,
      maxHeight: maxTaskHeight
    };
    log.debug("taskNode", taskNode);
    const taskWrapper = diagram2.append("g").attr("class", "taskWrapper");
    const node = svgDraw_default.drawNode(taskWrapper, taskNode, sectionColor, conf);
    const taskHeight = node.height;
    log.debug("taskHeight after draw", taskHeight);
    const taskX = timelineX - TASK_AXIS_GAP - node.width;
    taskWrapper.attr("transform", `translate(${taskX}, ${masterY})`);
    maxTaskHeight = Math.max(maxTaskHeight, taskHeight);
    if (task.events && task.events.length > 0) {
      const eventsStartY = masterY;
      const eventsX = timelineX + EVENT_AXIS_GAP;
      drawEvents2(diagram2, task.events, sectionColor, timelineX, eventsX, eventsStartY, conf);
    }
    masterY = masterY + taskSpacing;
    if (isWithoutSections && !conf.timeline?.disableMulticolor) {
      sectionColor++;
    }
  }
}, "drawTasks");
var drawEvents2 = /* @__PURE__ */ __name(function(diagram2, events, sectionColor, axisX, eventsX, startY, conf) {
  let currentY = startY;
  for (const event of events) {
    const eventNode = {
      descr: event,
      section: sectionColor,
      number: sectionColor,
      width: EVENT_WIDTH,
      padding: NODE_PADDING,
      maxHeight: 0
    };
    log.debug("eventNode", eventNode);
    const eventWrapper = diagram2.append("g").attr("class", "eventWrapper");
    const node = svgDraw_default.drawNode(eventWrapper, eventNode, sectionColor, conf);
    const eventHeight = node.height;
    eventWrapper.attr("transform", `translate(${eventsX}, ${currentY})`);
    const lineWrapper = diagram2.append("g").attr("class", "lineWrapper");
    const lineY = currentY + eventHeight / 2;
    lineWrapper.append("line").attr("x1", axisX).attr("y1", lineY).attr("x2", eventsX).attr("y2", lineY).attr("stroke-width", 2).attr("stroke", "black").attr("marker-end", "url(#arrowhead)").attr("stroke-dasharray", "5,5");
    currentY = currentY + eventHeight + EVENT_SPACING;
  }
  return currentY - startY;
}, "drawEvents");
var timelineRendererVertical_default = {
  setConf: /* @__PURE__ */ __name(() => {
  }, "setConf"),
  draw: draw2
};

// src/diagrams/timeline/styles.js
import { darken, lighten, isDark } from "khroma";
var genReduxSections = /* @__PURE__ */ __name((options) => {
  const { theme } = getConfig();
  const isDarkTheme = theme?.includes("dark");
  const isColorTheme = theme?.includes("color");
  const rawSvgId = options.svgId?.replace(/^#/, "") ?? "";
  const scopedDropShadow = rawSvgId ? `url(#${rawSvgId}-drop-shadow)` : options.dropShadow ?? "none";
  let sections2 = "";
  for (let i = 0; i < options.THEME_COLOR_LIMIT; i++) {
    const sw = `${17 - 3 * i}`;
    const color = isColorTheme ? options.borderColorArray[i] : options.mainBkg;
    const stroke = isColorTheme ? options.borderColorArray[i] : options.nodeBorder;
    sections2 += `
    .section-${i - 1} rect,
    .section-${i - 1} path,
    .section-${i - 1} circle {
      fill: ${isDarkTheme && isColorTheme ? options.mainBkg : color};
      stroke: ${stroke};
      stroke-width: ${options.strokeWidth};
      filter: ${scopedDropShadow};
    }

    .section-${i - 1} text {
      fill: ${options.nodeBorder};
      font-weight: ${options.fontWeight}
    }

    .node-icon-${i - 1} {
      font-size: 40px;
      color: ${options["cScaleLabel" + i]};
    }

    .section-edge-${i - 1} {
      stroke: ${options["cScale" + i]};
    }

    .edge-depth-${i - 1} {
      stroke-width: ${sw};
    }

    .section-${i - 1} line {
      stroke: ${options["cScaleInv" + i]};
      stroke-width: 3;
    }

    .lineWrapper line {
      stroke: ${options.nodeBorder};
      stroke-width:${options.strokeWidth}
    }

    .disabled,
    .disabled circle,
    .disabled text {
      fill: ${options.tertiaryColor ?? "lightgray"};
    }

    .disabled text {
      fill: ${options.clusterBorder ?? "#efefef"};
    }
    `;
  }
  return sections2;
}, "genReduxSections");
var genSections = /* @__PURE__ */ __name((options) => {
  let sections2 = "";
  for (let i = 0; i < options.THEME_COLOR_LIMIT; i++) {
    options["lineColor" + i] = options["lineColor" + i] || options["cScaleInv" + i];
    if (isDark(options["lineColor" + i])) {
      options["lineColor" + i] = lighten(options["lineColor" + i], 20);
    } else {
      options["lineColor" + i] = darken(options["lineColor" + i], 20);
    }
  }
  for (let i = 0; i < options.THEME_COLOR_LIMIT; i++) {
    const sw = "" + (17 - 3 * i);
    sections2 += `
    .section-${i - 1} rect, .section-${i - 1} path, .section-${i - 1} circle, .section-${i - 1} path  {
      fill: ${options["cScale" + i]};
    }
    .section-${i - 1} text {
     fill: ${options["cScaleLabel" + i]};
    }
    .node-icon-${i - 1} {
      font-size: 40px;
      color: ${options["cScaleLabel" + i]};
    }
    .section-edge-${i - 1}{
      stroke: ${options["cScale" + i]};
    }
    .edge-depth-${i - 1}{
      stroke-width: ${sw};
    }
    .section-${i - 1} line {
      stroke: ${options["cScaleInv" + i]} ;
      stroke-width: 3;
    }

    .lineWrapper line{
      stroke: ${options["cScaleLabel" + i]} ;
    }

    .disabled, .disabled circle, .disabled text {
      fill: ${options.tertiaryColor ?? "lightgray"};
    }
    .disabled text {
      fill: ${options.clusterBorder ?? "#efefef"};
    }
    `;
  }
  return sections2;
}, "genSections");
var getStyles = /* @__PURE__ */ __name((options) => {
  const { theme } = getConfig();
  const isReduxTheme = theme?.includes("redux");
  const isNeutralTheme = theme === "neutral";
  const rawSvgId = options.svgId?.replace(/^#/, "") ?? "";
  let gradientSections = "";
  if (options.useGradient && rawSvgId && options.THEME_COLOR_LIMIT && !isNeutralTheme) {
    for (let i = 0; i < options.THEME_COLOR_LIMIT; i++) {
      gradientSections += `
      .section-${i - 1}[data-look="neo"] rect,
      .section-${i - 1}[data-look="neo"] path,
      .section-${i - 1}[data-look="neo"] circle {
        fill: ${options.mainBkg};
        stroke: url(#${rawSvgId}-gradient);
        stroke-width: 2;
      }
      .section-${i - 1}[data-look="neo"] line {
        stroke: url(#${rawSvgId}-gradient);
        stroke-width: 2;
      }`;
    }
  }
  return `
  .edge {
    stroke-width: 3;
  }
  ${isReduxTheme ? genReduxSections(options) : genSections(options)}
  ${gradientSections}
  .section-root rect, .section-root path, .section-root circle  {
    fill: ${options.git0};
  }
  .section-root text {
    fill: ${options.gitBranchLabel0};
  }
  .icon-container {
    height:100%;
    display: flex;
    justify-content: center;
    align-items: center;
  }
  .edge {
    fill: none;
  }
  .eventWrapper  {
   filter: brightness(120%);
  }
`;
}, "getStyles");
var styles_default = getStyles;

// src/diagrams/timeline/timeline-definition.ts
var rendererSelector = {
  setConf: /* @__PURE__ */ __name(() => {
  }, "setConf"),
  draw: /* @__PURE__ */ __name((text, id, version, diagObj) => {
    const direction2 = diagObj?.db?.getDirection?.() ?? "LR";
    if (direction2 === "TD") {
      return timelineRendererVertical_default.draw(text, id, version, diagObj);
    }
    return timelineRenderer_default.draw(text, id, version, diagObj);
  }, "draw")
};
var diagram = {
  db: timelineDb_exports,
  renderer: rendererSelector,
  parser: timeline_default,
  styles: styles_default
};
export {
  diagram
};
