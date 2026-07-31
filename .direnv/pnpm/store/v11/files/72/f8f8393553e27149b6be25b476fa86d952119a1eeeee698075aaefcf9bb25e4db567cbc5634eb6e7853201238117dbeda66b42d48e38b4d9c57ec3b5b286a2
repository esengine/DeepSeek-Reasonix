import {
  selectSvgElement
} from "./chunk-PJCWW7M6.mjs";
import {
  at
} from "./chunk-PLCLPJVV.mjs";
import {
  cleanAndMerge
} from "./chunk-MMGVDTGO.mjs";
import "./chunk-IPM4HZQ6.mjs";
import {
  clear,
  configureSvgSize,
  darken_default,
  defaultConfig_default,
  getAccDescription,
  getAccTitle,
  getConfig,
  getDiagramTitle,
  is_dark_default,
  lighten_default,
  setAccDescription,
  setAccTitle,
  setDiagramTitle,
  transparentize_default
} from "./chunk-CHAKFXHA.mjs";
import {
  select_default
} from "./chunk-FO5PYUIK.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// src/diagrams/venn/parser/venn.jison
var parser = (function() {
  var o = /* @__PURE__ */ __name(function(k, v, o2, l) {
    for (o2 = o2 || {}, l = k.length; l--; o2[k[l]] = v) ;
    return o2;
  }, "o"), $V0 = [5, 8], $V1 = [7, 8, 11, 12, 17, 19, 22, 24], $V2 = [1, 17], $V3 = [1, 18], $V4 = [7, 8, 11, 12, 14, 15, 16, 17, 19, 20, 21, 22, 24, 27], $V5 = [1, 31], $V6 = [1, 39], $V7 = [7, 8, 11, 12, 17, 19, 22, 24, 27], $V8 = [1, 57], $V9 = [1, 56], $Va = [1, 58], $Vb = [1, 59], $Vc = [1, 60], $Vd = [7, 8, 11, 12, 16, 17, 19, 20, 22, 24, 27, 31, 32, 33];
  var parser2 = {
    trace: /* @__PURE__ */ __name(function trace() {
    }, "trace"),
    yy: {},
    symbols_: { "error": 2, "start": 3, "optNewlines": 4, "VENN": 5, "document": 6, "EOF": 7, "NEWLINE": 8, "line": 9, "statement": 10, "TITLE": 11, "SET": 12, "identifier": 13, "BRACKET_LABEL": 14, "COLON": 15, "NUMERIC": 16, "UNION": 17, "identifierList": 18, "TEXT": 19, "IDENTIFIER": 20, "STRING": 21, "INDENT_TEXT": 22, "indentedTextTail": 23, "STYLE": 24, "stylesOpt": 25, "styleField": 26, "COMMA": 27, "styleValue": 28, "valueTokens": 29, "valueToken": 30, "HEXCOLOR": 31, "RGBCOLOR": 32, "RGBACOLOR": 33, "$accept": 0, "$end": 1 },
    terminals_: { 2: "error", 5: "VENN", 7: "EOF", 8: "NEWLINE", 11: "TITLE", 12: "SET", 14: "BRACKET_LABEL", 15: "COLON", 16: "NUMERIC", 17: "UNION", 19: "TEXT", 20: "IDENTIFIER", 21: "STRING", 22: "INDENT_TEXT", 24: "STYLE", 27: "COMMA", 31: "HEXCOLOR", 32: "RGBCOLOR", 33: "RGBACOLOR" },
    productions_: [0, [3, 4], [4, 0], [4, 2], [6, 0], [6, 2], [9, 1], [9, 1], [10, 1], [10, 2], [10, 3], [10, 4], [10, 5], [10, 2], [10, 3], [10, 4], [10, 5], [10, 3], [10, 3], [10, 3], [10, 4], [10, 4], [10, 2], [10, 3], [23, 1], [23, 1], [23, 1], [23, 2], [23, 2], [25, 1], [25, 3], [26, 3], [28, 1], [28, 1], [29, 1], [29, 2], [30, 1], [30, 1], [30, 1], [30, 1], [30, 1], [18, 1], [18, 3], [13, 1], [13, 1]],
    performAction: /* @__PURE__ */ __name(function anonymous(yytext, yyleng, yylineno, yy, yystate, $$, _$) {
      var $0 = $$.length - 1;
      switch (yystate) {
        case 1:
          return $$[$0 - 1];
          break;
        case 2:
        case 3:
        case 4:
          this.$ = [];
          break;
        case 5:
          $$[$0 - 1].push($$[$0]);
          this.$ = $$[$0 - 1];
          break;
        case 6:
          this.$ = [];
          break;
        case 7:
        case 22:
        case 32:
        case 36:
        case 37:
        case 38:
        case 39:
        case 40:
          this.$ = $$[$0];
          break;
        case 8:
          yy.setDiagramTitle($$[$0].substr(6));
          this.$ = $$[$0].substr(6);
          break;
        case 9:
          yy.addSubsetData([$$[$0]], void 0, void 0);
          if (yy.setIndentMode) {
            yy.setIndentMode(true);
          }
          break;
        case 10:
          yy.addSubsetData([$$[$0 - 1]], $$[$0], void 0);
          if (yy.setIndentMode) {
            yy.setIndentMode(true);
          }
          break;
        case 11:
          yy.addSubsetData([$$[$0 - 2]], void 0, parseFloat($$[$0]));
          if (yy.setIndentMode) {
            yy.setIndentMode(true);
          }
          break;
        case 12:
          yy.addSubsetData([$$[$0 - 3]], $$[$0 - 2], parseFloat($$[$0]));
          if (yy.setIndentMode) {
            yy.setIndentMode(true);
          }
          break;
        case 13:
          if ($$[$0].length < 2) {
            throw new Error("union requires multiple identifiers");
          }
          if (yy.validateUnionIdentifiers) {
            yy.validateUnionIdentifiers($$[$0]);
          }
          yy.addSubsetData($$[$0], void 0, void 0);
          if (yy.setIndentMode) {
            yy.setIndentMode(true);
          }
          break;
        case 14:
          if ($$[$0 - 1].length < 2) {
            throw new Error("union requires multiple identifiers");
          }
          if (yy.validateUnionIdentifiers) {
            yy.validateUnionIdentifiers($$[$0 - 1]);
          }
          yy.addSubsetData($$[$0 - 1], $$[$0], void 0);
          if (yy.setIndentMode) {
            yy.setIndentMode(true);
          }
          break;
        case 15:
          if ($$[$0 - 2].length < 2) {
            throw new Error("union requires multiple identifiers");
          }
          if (yy.validateUnionIdentifiers) {
            yy.validateUnionIdentifiers($$[$0 - 2]);
          }
          yy.addSubsetData($$[$0 - 2], void 0, parseFloat($$[$0]));
          if (yy.setIndentMode) {
            yy.setIndentMode(true);
          }
          break;
        case 16:
          if ($$[$0 - 3].length < 2) {
            throw new Error("union requires multiple identifiers");
          }
          if (yy.validateUnionIdentifiers) {
            yy.validateUnionIdentifiers($$[$0 - 3]);
          }
          yy.addSubsetData($$[$0 - 3], $$[$0 - 2], parseFloat($$[$0]));
          if (yy.setIndentMode) {
            yy.setIndentMode(true);
          }
          break;
        case 17:
        case 18:
        case 19:
          yy.addTextData($$[$0 - 1], $$[$0], void 0);
          break;
        case 20:
        case 21:
          yy.addTextData($$[$0 - 2], $$[$0 - 1], $$[$0]);
          break;
        case 23:
          yy.addStyleData($$[$0 - 1], $$[$0]);
          break;
        case 24:
        case 25:
        case 26:
          var cs = yy.getCurrentSets();
          if (!cs) throw new Error("text requires set");
          yy.addTextData(cs, $$[$0], void 0);
          break;
        case 27:
        case 28:
          var cs = yy.getCurrentSets();
          if (!cs) throw new Error("text requires set");
          yy.addTextData(cs, $$[$0 - 1], $$[$0]);
          break;
        case 29:
        case 41:
          this.$ = [$$[$0]];
          break;
        case 30:
        case 42:
          this.$ = [...$$[$0 - 2], $$[$0]];
          break;
        case 31:
          this.$ = [$$[$0 - 2], $$[$0]];
          break;
        case 33:
          this.$ = $$[$0].join(" ");
          break;
        case 34:
          this.$ = [$$[$0]];
          break;
        case 35:
          $$[$0 - 1].push($$[$0]);
          this.$ = $$[$0 - 1];
          break;
        case 43:
        case 44:
          this.$ = $$[$0];
          break;
      }
    }, "anonymous"),
    table: [o($V0, [2, 2], { 3: 1, 4: 2 }), { 1: [3] }, { 5: [1, 3], 8: [1, 4] }, o($V1, [2, 4], { 6: 5 }), o($V0, [2, 3]), { 7: [1, 6], 8: [1, 8], 9: 7, 10: 9, 11: [1, 10], 12: [1, 11], 17: [1, 12], 19: [1, 13], 22: [1, 14], 24: [1, 15] }, { 1: [2, 1] }, o($V1, [2, 5]), o($V1, [2, 6]), o($V1, [2, 7]), o($V1, [2, 8]), { 13: 16, 20: $V2, 21: $V3 }, { 13: 20, 18: 19, 20: $V2, 21: $V3 }, { 13: 20, 18: 21, 20: $V2, 21: $V3 }, { 16: [1, 25], 20: [1, 23], 21: [1, 24], 23: 22 }, { 13: 20, 18: 26, 20: $V2, 21: $V3 }, o($V1, [2, 9], { 14: [1, 27], 15: [1, 28] }), o($V4, [2, 43]), o($V4, [2, 44]), o($V1, [2, 13], { 14: [1, 29], 15: [1, 30], 27: $V5 }), o($V4, [2, 41]), { 16: [1, 34], 20: [1, 32], 21: [1, 33], 27: $V5 }, o($V1, [2, 22]), o($V1, [2, 24], { 14: [1, 35] }), o($V1, [2, 25], { 14: [1, 36] }), o($V1, [2, 26]), { 20: $V6, 25: 37, 26: 38, 27: $V5 }, o($V1, [2, 10], { 15: [1, 40] }), { 16: [1, 41] }, o($V1, [2, 14], { 15: [1, 42] }), { 16: [1, 43] }, { 13: 44, 20: $V2, 21: $V3 }, o($V1, [2, 17], { 14: [1, 45] }), o($V1, [2, 18], { 14: [1, 46] }), o($V1, [2, 19]), o($V1, [2, 27]), o($V1, [2, 28]), o($V1, [2, 23], { 27: [1, 47] }), o($V7, [2, 29]), { 15: [1, 48] }, { 16: [1, 49] }, o($V1, [2, 11]), { 16: [1, 50] }, o($V1, [2, 15]), o($V4, [2, 42]), o($V1, [2, 20]), o($V1, [2, 21]), { 20: $V6, 26: 51 }, { 16: $V8, 20: $V9, 21: [1, 53], 28: 52, 29: 54, 30: 55, 31: $Va, 32: $Vb, 33: $Vc }, o($V1, [2, 12]), o($V1, [2, 16]), o($V7, [2, 30]), o($V7, [2, 31]), o($V7, [2, 32]), o($V7, [2, 33], { 30: 61, 16: $V8, 20: $V9, 31: $Va, 32: $Vb, 33: $Vc }), o($Vd, [2, 34]), o($Vd, [2, 36]), o($Vd, [2, 37]), o($Vd, [2, 38]), o($Vd, [2, 39]), o($Vd, [2, 40]), o($Vd, [2, 35])],
    defaultActions: { 6: [2, 1] },
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
            break;
          case 3:
            if (yy.getIndentMode && yy.getIndentMode()) {
              yy.consumeIndentText = true;
              this.begin("INITIAL");
              return 22;
            }
            break;
          case 4:
            break;
          case 5:
            if (yy.setIndentMode) {
              yy.setIndentMode(false);
            }
            this.begin("INITIAL");
            this.unput(yy_.yytext);
            break;
          case 6:
            this.begin("bol");
            return 8;
            break;
          case 7:
            break;
          case 8:
            break;
          case 9:
            return 7;
            break;
          case 10:
            return 11;
            break;
          case 11:
            return 5;
            break;
          case 12:
            return 12;
            break;
          case 13:
            return 17;
            break;
          case 14:
            if (yy.consumeIndentText) {
              yy.consumeIndentText = false;
            } else {
              return 19;
            }
            break;
          case 15:
            return 24;
            break;
          case 16:
            yy_.yytext = yy_.yytext.slice(2, -2);
            return 14;
            break;
          case 17:
            yy_.yytext = yy_.yytext.slice(1, -1).trim();
            return 14;
            break;
          case 18:
            return 16;
            break;
          case 19:
            return 31;
            break;
          case 20:
            return 33;
            break;
          case 21:
            return 32;
            break;
          case 22:
            return 20;
            break;
          case 23:
            return 21;
            break;
          case 24:
            return 27;
            break;
          case 25:
            return 15;
            break;
        }
      }, "anonymous"),
      rules: [/^(?:%%(?!\{)[^\n]*)/i, /^(?:[^\}]%%[^\n]*)/i, /^(?:[ \t]+(?=[\n\r]))/i, /^(?:[ \t]+(?=text\b))/i, /^(?:[ \t]+)/i, /^(?:[^ \t\n\r])/i, /^(?:[\n\r]+)/i, /^(?:%%[^\n]*)/i, /^(?:[ \t]+)/i, /^(?:$)/i, /^(?:title\s[^#\n;]+)/i, /^(?:venn-beta\b)/i, /^(?:set\b)/i, /^(?:union\b)/i, /^(?:text\b)/i, /^(?:style\b)/i, /^(?:\["[^\"]*"\])/i, /^(?:\[[^\]\"]+\])/i, /^(?:[+-]?(\d+(\.\d+)?|\.\d+))/i, /^(?:#[0-9a-fA-F]{3,8})/i, /^(?:rgba\(\s*[0-9.]+\s*[,]\s*[0-9.]+\s*[,]\s*[0-9.]+\s*[,]\s*[0-9.]+\s*\))/i, /^(?:rgb\(\s*[0-9.]+\s*[,]\s*[0-9.]+\s*[,]\s*[0-9.]+\s*\))/i, /^(?:[A-Za-z_][A-Za-z0-9\-_]*)/i, /^(?:"[^\"]*")/i, /^(?:,)/i, /^(?::)/i],
      conditions: { "bol": { "rules": [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25], "inclusive": true }, "INITIAL": { "rules": [0, 1, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25], "inclusive": true } }
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
var venn_default = parser;

// src/diagrams/venn/vennDB.ts
var subsets = [];
var textNodes = [];
var styleEntries = [];
var knownSets = /* @__PURE__ */ new Set();
var currentSets;
var indentMode = false;
var addSubsetData = /* @__PURE__ */ __name((identifierList, label, size) => {
  const sets = normalizeIdentifierList(identifierList).sort();
  const resolvedSize = size ?? 10 / Math.pow(identifierList.length, 2);
  currentSets = sets;
  if (sets.length === 1) {
    knownSets.add(sets[0]);
  }
  subsets.push({
    sets,
    size: resolvedSize,
    label: label ? normalizeText(label) : void 0
  });
}, "addSubsetData");
var getSubsetData = /* @__PURE__ */ __name(() => {
  return subsets;
}, "getSubsetData");
var normalizeText = /* @__PURE__ */ __name((text) => {
  const trimmed = text.trim();
  if (trimmed.length >= 2 && trimmed.startsWith('"') && trimmed.endsWith('"')) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}, "normalizeText");
var normalizeStyleValue = /* @__PURE__ */ __name((value) => {
  return value ? normalizeText(value) : value;
}, "normalizeStyleValue");
var addTextData = /* @__PURE__ */ __name((identifierList, id, label) => {
  const normalizedId = normalizeText(id);
  textNodes.push({
    sets: normalizeIdentifierList(identifierList).sort(),
    id: normalizedId,
    label: label ? normalizeText(label) : void 0
  });
}, "addTextData");
var addStyleData = /* @__PURE__ */ __name((identifierList, data) => {
  const targets = normalizeIdentifierList(identifierList).sort();
  const styles = {};
  for (const [key, value] of data) {
    styles[key] = normalizeStyleValue(value) ?? value;
  }
  styleEntries.push({ targets, styles });
}, "addStyleData");
var getStyleData = /* @__PURE__ */ __name(() => {
  return styleEntries;
}, "getStyleData");
var normalizeIdentifierList = /* @__PURE__ */ __name((identifierList) => {
  return identifierList.map((identifier) => normalizeText(identifier));
}, "normalizeIdentifierList");
var validateUnionIdentifiers = /* @__PURE__ */ __name((identifierList) => {
  const normalized = normalizeIdentifierList(identifierList);
  const unknown = normalized.filter((identifier) => !knownSets.has(identifier));
  if (unknown.length > 0) {
    throw new Error(`unknown set identifier: ${unknown.join(", ")}`);
  }
}, "validateUnionIdentifiers");
var getTextData = /* @__PURE__ */ __name(() => {
  return textNodes;
}, "getTextData");
var getCurrentSets = /* @__PURE__ */ __name(() => currentSets, "getCurrentSets");
var getIndentMode = /* @__PURE__ */ __name(() => indentMode, "getIndentMode");
var setIndentMode = /* @__PURE__ */ __name((enabled) => {
  indentMode = enabled;
}, "setIndentMode");
var DEFAULT_VENN_CONFIG = defaultConfig_default.venn;
function getConfig2() {
  return cleanAndMerge(DEFAULT_VENN_CONFIG, getConfig().venn);
}
__name(getConfig2, "getConfig");
var customClear = /* @__PURE__ */ __name(() => {
  clear();
  subsets.length = 0;
  textNodes.length = 0;
  styleEntries.length = 0;
  knownSets.clear();
  currentSets = void 0;
  indentMode = false;
}, "customClear");
var db = {
  getConfig: getConfig2,
  clear: customClear,
  setAccTitle,
  getAccTitle,
  setDiagramTitle,
  getDiagramTitle,
  getAccDescription,
  setAccDescription,
  addSubsetData,
  getSubsetData,
  addTextData,
  addStyleData,
  validateUnionIdentifiers,
  getTextData,
  getStyleData,
  getCurrentSets,
  getIndentMode,
  setIndentMode
};

// src/diagrams/venn/styles.ts
var getStyles = /* @__PURE__ */ __name((options) => `
  .venn-title {
    font-size: 32px;
    fill: ${options.vennTitleTextColor};
    font-family: ${options.fontFamily};
  }

  .venn-circle text {
    font-size: 48px;
    font-family: ${options.fontFamily};
  }

  .venn-intersection text {
    font-size: 48px;
    fill: ${options.vennSetTextColor};
    font-family: ${options.fontFamily};
  }

  .venn-text-node {
    font-family: ${options.fontFamily};
    color: ${options.vennSetTextColor};
  }
`, "getStyles");
var styles_default = getStyles;

// ../../node_modules/.pnpm/@upsetjs+venn.js@2.0.0/node_modules/@upsetjs/venn.js/build/venn.esm.js
var SMALL$1 = 1e-10;
function intersectionArea(circles, stats) {
  const intersectionPoints = getIntersectionPoints(circles);
  const innerPoints = intersectionPoints.filter((p) => containedInCircles(p, circles));
  let arcArea = 0;
  let polygonArea = 0;
  const arcs = [];
  if (innerPoints.length > 1) {
    const center = getCenter(innerPoints);
    for (let i = 0; i < innerPoints.length; ++i) {
      const p = innerPoints[i];
      p.angle = Math.atan2(p.x - center.x, p.y - center.y);
    }
    innerPoints.sort((a, b) => b.angle - a.angle);
    let p2 = innerPoints[innerPoints.length - 1];
    for (let i = 0; i < innerPoints.length; ++i) {
      const p1 = innerPoints[i];
      polygonArea += (p2.x + p1.x) * (p1.y - p2.y);
      const midPoint = { x: (p1.x + p2.x) / 2, y: (p1.y + p2.y) / 2 };
      let arc = null;
      for (let j = 0; j < p1.parentIndex.length; ++j) {
        if (p2.parentIndex.includes(p1.parentIndex[j])) {
          const circle = circles[p1.parentIndex[j]];
          const a1 = Math.atan2(p1.x - circle.x, p1.y - circle.y);
          const a2 = Math.atan2(p2.x - circle.x, p2.y - circle.y);
          let angleDiff = a2 - a1;
          if (angleDiff < 0) {
            angleDiff += 2 * Math.PI;
          }
          const a = a2 - angleDiff / 2;
          let width = distance(midPoint, {
            x: circle.x + circle.radius * Math.sin(a),
            y: circle.y + circle.radius * Math.cos(a)
          });
          if (width > circle.radius * 2) {
            width = circle.radius * 2;
          }
          if (arc == null || arc.width > width) {
            arc = { circle, width, p1, p2, large: width > circle.radius, sweep: true };
          }
        }
      }
      if (arc != null) {
        arcs.push(arc);
        arcArea += circleArea(arc.circle.radius, arc.width);
        p2 = p1;
      }
    }
  } else {
    let smallest = circles[0];
    for (let i = 1; i < circles.length; ++i) {
      if (circles[i].radius < smallest.radius) {
        smallest = circles[i];
      }
    }
    let disjoint = false;
    for (let i = 0; i < circles.length; ++i) {
      if (distance(circles[i], smallest) > Math.abs(smallest.radius - circles[i].radius)) {
        disjoint = true;
        break;
      }
    }
    if (disjoint) {
      arcArea = polygonArea = 0;
    } else {
      arcArea = smallest.radius * smallest.radius * Math.PI;
      arcs.push({
        circle: smallest,
        p1: { x: smallest.x, y: smallest.y + smallest.radius },
        p2: { x: smallest.x - SMALL$1, y: smallest.y + smallest.radius },
        width: smallest.radius * 2,
        large: true,
        sweep: true
      });
    }
  }
  polygonArea /= 2;
  if (stats) {
    stats.area = arcArea + polygonArea;
    stats.arcArea = arcArea;
    stats.polygonArea = polygonArea;
    stats.arcs = arcs;
    stats.innerPoints = innerPoints;
    stats.intersectionPoints = intersectionPoints;
  }
  return arcArea + polygonArea;
}
__name(intersectionArea, "intersectionArea");
function containedInCircles(point, circles) {
  return circles.every((circle) => distance(point, circle) < circle.radius + SMALL$1);
}
__name(containedInCircles, "containedInCircles");
function getIntersectionPoints(circles) {
  const ret = [];
  for (let i = 0; i < circles.length; ++i) {
    for (let j = i + 1; j < circles.length; ++j) {
      const intersect = circleCircleIntersection(circles[i], circles[j]);
      for (const p of intersect) {
        p.parentIndex = [i, j];
        ret.push(p);
      }
    }
  }
  return ret;
}
__name(getIntersectionPoints, "getIntersectionPoints");
function circleArea(r, width) {
  return r * r * Math.acos(1 - width / r) - (r - width) * Math.sqrt(width * (2 * r - width));
}
__name(circleArea, "circleArea");
function distance(p1, p2) {
  return Math.sqrt((p1.x - p2.x) * (p1.x - p2.x) + (p1.y - p2.y) * (p1.y - p2.y));
}
__name(distance, "distance");
function circleOverlap(r1, r2, d) {
  if (d >= r1 + r2) {
    return 0;
  }
  if (d <= Math.abs(r1 - r2)) {
    return Math.PI * Math.min(r1, r2) * Math.min(r1, r2);
  }
  const w1 = r1 - (d * d - r2 * r2 + r1 * r1) / (2 * d);
  const w2 = r2 - (d * d - r1 * r1 + r2 * r2) / (2 * d);
  return circleArea(r1, w1) + circleArea(r2, w2);
}
__name(circleOverlap, "circleOverlap");
function circleCircleIntersection(p1, p2) {
  const d = distance(p1, p2);
  const r1 = p1.radius;
  const r2 = p2.radius;
  if (d >= r1 + r2 || d <= Math.abs(r1 - r2)) {
    return [];
  }
  const a = (r1 * r1 - r2 * r2 + d * d) / (2 * d);
  const h = Math.sqrt(r1 * r1 - a * a);
  const x0 = p1.x + a * (p2.x - p1.x) / d;
  const y0 = p1.y + a * (p2.y - p1.y) / d;
  const rx = -(p2.y - p1.y) * (h / d);
  const ry = -(p2.x - p1.x) * (h / d);
  return [
    { x: x0 + rx, y: y0 - ry },
    { x: x0 - rx, y: y0 + ry }
  ];
}
__name(circleCircleIntersection, "circleCircleIntersection");
function getCenter(points) {
  const center = { x: 0, y: 0 };
  for (const point of points) {
    center.x += point.x;
    center.y += point.y;
  }
  center.x /= points.length;
  center.y /= points.length;
  return center;
}
__name(getCenter, "getCenter");
function bisect(f, a, b, parameters) {
  parameters = parameters || {};
  const maxIterations = parameters.maxIterations || 100;
  const tolerance = parameters.tolerance || 1e-10;
  const fA = f(a);
  const fB = f(b);
  let delta = b - a;
  if (fA * fB > 0) {
    throw "Initial bisect points must have opposite signs";
  }
  if (fA === 0) return a;
  if (fB === 0) return b;
  for (let i = 0; i < maxIterations; ++i) {
    delta /= 2;
    const mid = a + delta;
    const fMid = f(mid);
    if (fMid * fA >= 0) {
      a = mid;
    }
    if (Math.abs(delta) < tolerance || fMid === 0) {
      return mid;
    }
  }
  return a + delta;
}
__name(bisect, "bisect");
function zeros(x) {
  const r = new Array(x);
  for (let i = 0; i < x; ++i) {
    r[i] = 0;
  }
  return r;
}
__name(zeros, "zeros");
function zerosM(x, y) {
  return zeros(x).map(() => zeros(y));
}
__name(zerosM, "zerosM");
function dot(a, b) {
  let ret = 0;
  for (let i = 0; i < a.length; ++i) {
    ret += a[i] * b[i];
  }
  return ret;
}
__name(dot, "dot");
function norm2(a) {
  return Math.sqrt(dot(a, a));
}
__name(norm2, "norm2");
function scale(ret, value, c) {
  for (let i = 0; i < value.length; ++i) {
    ret[i] = value[i] * c;
  }
}
__name(scale, "scale");
function weightedSum(ret, w1, v1, w2, v2) {
  for (let j = 0; j < ret.length; ++j) {
    ret[j] = w1 * v1[j] + w2 * v2[j];
  }
}
__name(weightedSum, "weightedSum");
function nelderMead(f, x0, parameters) {
  parameters = parameters || {};
  const maxIterations = parameters.maxIterations || x0.length * 200;
  const nonZeroDelta = parameters.nonZeroDelta || 1.05;
  const zeroDelta = parameters.zeroDelta || 1e-3;
  const minErrorDelta = parameters.minErrorDelta || 1e-6;
  const minTolerance = parameters.minErrorDelta || 1e-5;
  const rho = parameters.rho !== void 0 ? parameters.rho : 1;
  const chi = parameters.chi !== void 0 ? parameters.chi : 2;
  const psi = parameters.psi !== void 0 ? parameters.psi : -0.5;
  const sigma = parameters.sigma !== void 0 ? parameters.sigma : 0.5;
  let maxDiff;
  const N = x0.length;
  const simplex = new Array(N + 1);
  simplex[0] = x0;
  simplex[0].fx = f(x0);
  simplex[0].id = 0;
  for (let i = 0; i < N; ++i) {
    const point = x0.slice();
    point[i] = point[i] ? point[i] * nonZeroDelta : zeroDelta;
    simplex[i + 1] = point;
    simplex[i + 1].fx = f(point);
    simplex[i + 1].id = i + 1;
  }
  function updateSimplex(value) {
    for (let i = 0; i < value.length; i++) {
      simplex[N][i] = value[i];
    }
    simplex[N].fx = value.fx;
  }
  __name(updateSimplex, "updateSimplex");
  const sortOrder = /* @__PURE__ */ __name((a, b) => a.fx - b.fx, "sortOrder");
  const centroid = x0.slice();
  const reflected = x0.slice();
  const contracted = x0.slice();
  const expanded = x0.slice();
  for (let iteration = 0; iteration < maxIterations; ++iteration) {
    simplex.sort(sortOrder);
    if (parameters.history) {
      const sortedSimplex = simplex.map((x) => {
        const state = x.slice();
        state.fx = x.fx;
        state.id = x.id;
        return state;
      });
      sortedSimplex.sort((a, b) => a.id - b.id);
      parameters.history.push({
        x: simplex[0].slice(),
        fx: simplex[0].fx,
        simplex: sortedSimplex
      });
    }
    maxDiff = 0;
    for (let i = 0; i < N; ++i) {
      maxDiff = Math.max(maxDiff, Math.abs(simplex[0][i] - simplex[1][i]));
    }
    if (Math.abs(simplex[0].fx - simplex[N].fx) < minErrorDelta && maxDiff < minTolerance) {
      break;
    }
    for (let i = 0; i < N; ++i) {
      centroid[i] = 0;
      for (let j = 0; j < N; ++j) {
        centroid[i] += simplex[j][i];
      }
      centroid[i] /= N;
    }
    const worst = simplex[N];
    weightedSum(reflected, 1 + rho, centroid, -rho, worst);
    reflected.fx = f(reflected);
    if (reflected.fx < simplex[0].fx) {
      weightedSum(expanded, 1 + chi, centroid, -chi, worst);
      expanded.fx = f(expanded);
      if (expanded.fx < reflected.fx) {
        updateSimplex(expanded);
      } else {
        updateSimplex(reflected);
      }
    } else if (reflected.fx >= simplex[N - 1].fx) {
      let shouldReduce = false;
      if (reflected.fx > worst.fx) {
        weightedSum(contracted, 1 + psi, centroid, -psi, worst);
        contracted.fx = f(contracted);
        if (contracted.fx < worst.fx) {
          updateSimplex(contracted);
        } else {
          shouldReduce = true;
        }
      } else {
        weightedSum(contracted, 1 - psi * rho, centroid, psi * rho, worst);
        contracted.fx = f(contracted);
        if (contracted.fx < reflected.fx) {
          updateSimplex(contracted);
        } else {
          shouldReduce = true;
        }
      }
      if (shouldReduce) {
        if (sigma >= 1) break;
        for (let i = 1; i < simplex.length; ++i) {
          weightedSum(simplex[i], 1 - sigma, simplex[0], sigma, simplex[i]);
          simplex[i].fx = f(simplex[i]);
        }
      }
    } else {
      updateSimplex(reflected);
    }
  }
  simplex.sort(sortOrder);
  return { fx: simplex[0].fx, x: simplex[0] };
}
__name(nelderMead, "nelderMead");
function wolfeLineSearch(f, pk, current, next, a, c1, c2) {
  const phi0 = current.fx;
  const phiPrime0 = dot(current.fxprime, pk);
  let phi = phi0;
  let phi_old = phi0;
  let phiPrime = phiPrime0;
  let a0 = 0;
  a = a || 1;
  c1 = c1 || 1e-6;
  c2 = c2 || 0.1;
  function zoom(a_lo, a_high, phi_lo) {
    for (let iteration = 0; iteration < 16; ++iteration) {
      a = (a_lo + a_high) / 2;
      weightedSum(next.x, 1, current.x, a, pk);
      phi = next.fx = f(next.x, next.fxprime);
      phiPrime = dot(next.fxprime, pk);
      if (phi > phi0 + c1 * a * phiPrime0 || phi >= phi_lo) {
        a_high = a;
      } else {
        if (Math.abs(phiPrime) <= -c2 * phiPrime0) {
          return a;
        }
        if (phiPrime * (a_high - a_lo) >= 0) {
          a_high = a_lo;
        }
        a_lo = a;
        phi_lo = phi;
      }
    }
    return 0;
  }
  __name(zoom, "zoom");
  for (let iteration = 0; iteration < 10; ++iteration) {
    weightedSum(next.x, 1, current.x, a, pk);
    phi = next.fx = f(next.x, next.fxprime);
    phiPrime = dot(next.fxprime, pk);
    if (phi > phi0 + c1 * a * phiPrime0 || iteration && phi >= phi_old) {
      return zoom(a0, a, phi_old);
    }
    if (Math.abs(phiPrime) <= -c2 * phiPrime0) {
      return a;
    }
    if (phiPrime >= 0) {
      return zoom(a, a0, phi);
    }
    phi_old = phi;
    a0 = a;
    a *= 2;
  }
  return a;
}
__name(wolfeLineSearch, "wolfeLineSearch");
function conjugateGradient(f, initial, params) {
  let current = { x: initial.slice(), fx: 0, fxprime: initial.slice() };
  let next = { x: initial.slice(), fx: 0, fxprime: initial.slice() };
  const yk = initial.slice();
  let pk;
  let temp;
  let a = 1;
  let maxIterations;
  params = params || {};
  maxIterations = params.maxIterations || initial.length * 20;
  current.fx = f(current.x, current.fxprime);
  pk = current.fxprime.slice();
  scale(pk, current.fxprime, -1);
  for (let i = 0; i < maxIterations; ++i) {
    a = wolfeLineSearch(f, pk, current, next, a);
    if (params.history) {
      params.history.push({
        x: current.x.slice(),
        fx: current.fx,
        fxprime: current.fxprime.slice(),
        alpha: a
      });
    }
    if (!a) {
      scale(pk, current.fxprime, -1);
    } else {
      weightedSum(yk, 1, next.fxprime, -1, current.fxprime);
      const delta_k = dot(current.fxprime, current.fxprime);
      const beta_k = Math.max(0, dot(yk, next.fxprime) / delta_k);
      weightedSum(pk, beta_k, pk, -1, next.fxprime);
      temp = current;
      current = next;
      next = temp;
    }
    if (norm2(current.fxprime) <= 1e-5) {
      break;
    }
  }
  if (params.history) {
    params.history.push({
      x: current.x.slice(),
      fx: current.fx,
      fxprime: current.fxprime.slice(),
      alpha: a
    });
  }
  return current;
}
__name(conjugateGradient, "conjugateGradient");
function venn(sets, parameters = {}) {
  parameters.maxIterations = parameters.maxIterations || 500;
  const initialLayout = parameters.initialLayout || bestInitialLayout;
  const loss = parameters.lossFunction || lossFunction;
  const areas = addMissingAreas(sets, parameters);
  const circles = initialLayout(areas, parameters);
  const setids = Object.keys(circles);
  const initial = [];
  for (const setid of setids) {
    initial.push(circles[setid].x);
    initial.push(circles[setid].y);
  }
  const solution = nelderMead(
    (values) => {
      const current = {};
      for (let i = 0; i < setids.length; ++i) {
        const setid = setids[i];
        current[setid] = {
          x: values[2 * i],
          y: values[2 * i + 1],
          radius: circles[setid].radius
          // size : circles[setid].size
        };
      }
      return loss(current, areas);
    },
    initial,
    parameters
  );
  const positions = solution.x;
  for (let i = 0; i < setids.length; ++i) {
    const setid = setids[i];
    circles[setid].x = positions[2 * i];
    circles[setid].y = positions[2 * i + 1];
  }
  return circles;
}
__name(venn, "venn");
var SMALL = 1e-10;
function distanceFromIntersectArea(r1, r2, overlap) {
  if (Math.min(r1, r2) * Math.min(r1, r2) * Math.PI <= overlap + SMALL) {
    return Math.abs(r1 - r2);
  }
  return bisect((distance2) => circleOverlap(r1, r2, distance2) - overlap, 0, r1 + r2);
}
__name(distanceFromIntersectArea, "distanceFromIntersectArea");
function addMissingAreas(areas, parameters = {}) {
  const distinct = parameters.distinct;
  const r = areas.map((s) => Object.assign({}, s));
  function toKey(arr) {
    return arr.join(";");
  }
  __name(toKey, "toKey");
  if (distinct) {
    const count = /* @__PURE__ */ new Map();
    for (const area of r) {
      for (let i = 0; i < area.sets.length; i++) {
        const si = String(area.sets[i]);
        count.set(si, area.size + (count.get(si) || 0));
        for (let j = i + 1; j < area.sets.length; j++) {
          const sj = String(area.sets[j]);
          const k1 = `${si};${sj}`;
          const k2 = `${sj};${si}`;
          count.set(k1, area.size + (count.get(k1) || 0));
          count.set(k2, area.size + (count.get(k2) || 0));
        }
      }
    }
    for (const area of r) {
      if (area.sets.length < 3) {
        area.size = count.get(toKey(area.sets));
      }
    }
  }
  const ids = [];
  const pairs = /* @__PURE__ */ new Set();
  for (const area of r) {
    if (area.sets.length === 1) {
      ids.push(area.sets[0]);
    } else if (area.sets.length === 2) {
      const a = area.sets[0];
      const b = area.sets[1];
      pairs.add(toKey(area.sets));
      pairs.add(toKey([b, a]));
    }
  }
  ids.sort((a, b) => a === b ? 0 : a < b ? -1 : 1);
  for (let i = 0; i < ids.length; ++i) {
    const a = ids[i];
    for (let j = i + 1; j < ids.length; ++j) {
      const b = ids[j];
      if (!pairs.has(toKey([a, b]))) {
        r.push({ sets: [a, b], size: 0 });
      }
    }
  }
  return r;
}
__name(addMissingAreas, "addMissingAreas");
function getDistanceMatrices(areas, sets, setids) {
  const distances = zerosM(sets.length, sets.length);
  const constraints = zerosM(sets.length, sets.length);
  areas.filter((x) => x.sets.length === 2).forEach((current) => {
    const left = setids[current.sets[0]];
    const right = setids[current.sets[1]];
    const r1 = Math.sqrt(sets[left].size / Math.PI);
    const r2 = Math.sqrt(sets[right].size / Math.PI);
    const distance2 = distanceFromIntersectArea(r1, r2, current.size);
    distances[left][right] = distances[right][left] = distance2;
    let c = 0;
    if (current.size + 1e-10 >= Math.min(sets[left].size, sets[right].size)) {
      c = 1;
    } else if (current.size <= 1e-10) {
      c = -1;
    }
    constraints[left][right] = constraints[right][left] = c;
  });
  return { distances, constraints };
}
__name(getDistanceMatrices, "getDistanceMatrices");
function constrainedMDSGradient(x, fxprime, distances, constraints) {
  for (let i = 0; i < fxprime.length; ++i) {
    fxprime[i] = 0;
  }
  let loss = 0;
  for (let i = 0; i < distances.length; ++i) {
    const xi = x[2 * i];
    const yi = x[2 * i + 1];
    for (let j = i + 1; j < distances.length; ++j) {
      const xj = x[2 * j];
      const yj = x[2 * j + 1];
      const dij = distances[i][j];
      const constraint = constraints[i][j];
      const squaredDistance = (xj - xi) * (xj - xi) + (yj - yi) * (yj - yi);
      const distance2 = Math.sqrt(squaredDistance);
      const delta = squaredDistance - dij * dij;
      if (constraint > 0 && distance2 <= dij || constraint < 0 && distance2 >= dij) {
        continue;
      }
      loss += 2 * delta * delta;
      fxprime[2 * i] += 4 * delta * (xi - xj);
      fxprime[2 * i + 1] += 4 * delta * (yi - yj);
      fxprime[2 * j] += 4 * delta * (xj - xi);
      fxprime[2 * j + 1] += 4 * delta * (yj - yi);
    }
  }
  return loss;
}
__name(constrainedMDSGradient, "constrainedMDSGradient");
function bestInitialLayout(areas, params = {}) {
  let initial = greedyLayout(areas, params);
  const loss = params.lossFunction || lossFunction;
  if (areas.length >= 8) {
    const constrained = constrainedMDSLayout(areas, params);
    const constrainedLoss = loss(constrained, areas);
    const greedyLoss = loss(initial, areas);
    if (constrainedLoss + 1e-8 < greedyLoss) {
      initial = constrained;
    }
  }
  return initial;
}
__name(bestInitialLayout, "bestInitialLayout");
function constrainedMDSLayout(areas, params = {}) {
  const restarts = params.restarts || 10;
  const sets = [];
  const setids = {};
  for (const area of areas) {
    if (area.sets.length === 1) {
      setids[area.sets[0]] = sets.length;
      sets.push(area);
    }
  }
  let { distances, constraints } = getDistanceMatrices(areas, sets, setids);
  const norm = norm2(distances.map(norm2)) / distances.length;
  distances = distances.map((row) => row.map((value) => value / norm));
  const obj = /* @__PURE__ */ __name((x, fxprime) => constrainedMDSGradient(x, fxprime, distances, constraints), "obj");
  let best = null;
  for (let i = 0; i < restarts; ++i) {
    const initial = zeros(distances.length * 2).map(Math.random);
    const current = conjugateGradient(obj, initial, params);
    if (!best || current.fx < best.fx) {
      best = current;
    }
  }
  const positions = best.x;
  const circles = {};
  for (let i = 0; i < sets.length; ++i) {
    const set = sets[i];
    circles[set.sets[0]] = {
      x: positions[2 * i] * norm,
      y: positions[2 * i + 1] * norm,
      radius: Math.sqrt(set.size / Math.PI)
    };
  }
  if (params.history) {
    for (const h of params.history) {
      scale(h.x, norm);
    }
  }
  return circles;
}
__name(constrainedMDSLayout, "constrainedMDSLayout");
function greedyLayout(areas, params) {
  const loss = params && params.lossFunction ? params.lossFunction : lossFunction;
  const circles = {};
  const setOverlaps = {};
  for (const area of areas) {
    if (area.sets.length === 1) {
      const set = area.sets[0];
      circles[set] = {
        x: 1e10,
        y: 1e10,
        rowid: circles.length,
        size: area.size,
        radius: Math.sqrt(area.size / Math.PI)
      };
      setOverlaps[set] = [];
    }
  }
  areas = areas.filter((a) => a.sets.length === 2);
  for (const current of areas) {
    let weight = current.weight != null ? current.weight : 1;
    const left = current.sets[0];
    const right = current.sets[1];
    if (current.size + SMALL >= Math.min(circles[left].size, circles[right].size)) {
      weight = 0;
    }
    setOverlaps[left].push({ set: right, size: current.size, weight });
    setOverlaps[right].push({ set: left, size: current.size, weight });
  }
  const mostOverlapped = [];
  Object.keys(setOverlaps).forEach((set) => {
    let size = 0;
    for (let i = 0; i < setOverlaps[set].length; ++i) {
      size += setOverlaps[set][i].size * setOverlaps[set][i].weight;
    }
    mostOverlapped.push({ set, size });
  });
  function sortOrder(a, b) {
    return b.size - a.size;
  }
  __name(sortOrder, "sortOrder");
  mostOverlapped.sort(sortOrder);
  const positioned = {};
  function isPositioned(element) {
    return element.set in positioned;
  }
  __name(isPositioned, "isPositioned");
  function positionSet(point, index) {
    circles[index].x = point.x;
    circles[index].y = point.y;
    positioned[index] = true;
  }
  __name(positionSet, "positionSet");
  positionSet({ x: 0, y: 0 }, mostOverlapped[0].set);
  for (let i = 1; i < mostOverlapped.length; ++i) {
    const setIndex = mostOverlapped[i].set;
    const overlap = setOverlaps[setIndex].filter(isPositioned);
    const set = circles[setIndex];
    overlap.sort(sortOrder);
    if (overlap.length === 0) {
      throw "ERROR: missing pairwise overlap information";
    }
    const points = [];
    for (var j = 0; j < overlap.length; ++j) {
      const p1 = circles[overlap[j].set];
      const d1 = distanceFromIntersectArea(set.radius, p1.radius, overlap[j].size);
      points.push({ x: p1.x + d1, y: p1.y });
      points.push({ x: p1.x - d1, y: p1.y });
      points.push({ y: p1.y + d1, x: p1.x });
      points.push({ y: p1.y - d1, x: p1.x });
      for (let k = j + 1; k < overlap.length; ++k) {
        const p2 = circles[overlap[k].set];
        const d2 = distanceFromIntersectArea(set.radius, p2.radius, overlap[k].size);
        const extraPoints = circleCircleIntersection(
          { x: p1.x, y: p1.y, radius: d1 },
          { x: p2.x, y: p2.y, radius: d2 }
        );
        points.push(...extraPoints);
      }
    }
    let bestLoss = 1e50;
    let bestPoint = points[0];
    for (const point of points) {
      circles[setIndex].x = point.x;
      circles[setIndex].y = point.y;
      const localLoss = loss(circles, areas);
      if (localLoss < bestLoss) {
        bestLoss = localLoss;
        bestPoint = point;
      }
    }
    positionSet(bestPoint, setIndex);
  }
  return circles;
}
__name(greedyLayout, "greedyLayout");
function lossFunction(circles, overlaps) {
  let output = 0;
  for (const area of overlaps) {
    if (area.sets.length === 1) {
      continue;
    }
    let overlap;
    if (area.sets.length === 2) {
      const left = circles[area.sets[0]];
      const right = circles[area.sets[1]];
      overlap = circleOverlap(left.radius, right.radius, distance(left, right));
    } else {
      overlap = intersectionArea(area.sets.map((d) => circles[d]));
    }
    const weight = area.weight != null ? area.weight : 1;
    output += weight * (overlap - area.size) * (overlap - area.size);
  }
  return output;
}
__name(lossFunction, "lossFunction");
function logRatioLossFunction(circles, overlaps) {
  let output = 0;
  for (const area of overlaps) {
    if (area.sets.length === 1) {
      continue;
    }
    let overlap;
    if (area.sets.length === 2) {
      const left = circles[area.sets[0]];
      const right = circles[area.sets[1]];
      overlap = circleOverlap(left.radius, right.radius, distance(left, right));
    } else {
      overlap = intersectionArea(area.sets.map((d) => circles[d]));
    }
    const weight = area.weight != null ? area.weight : 1;
    const differenceFromIdeal = Math.log((overlap + 1) / (area.size + 1));
    output += weight * differenceFromIdeal * differenceFromIdeal;
  }
  return output;
}
__name(logRatioLossFunction, "logRatioLossFunction");
function orientateCircles(circles, orientation, orientationOrder) {
  if (orientationOrder == null) {
    circles.sort((a, b) => b.radius - a.radius);
  } else {
    circles.sort(orientationOrder);
  }
  if (circles.length > 0) {
    const largestX = circles[0].x;
    const largestY = circles[0].y;
    for (const circle of circles) {
      circle.x -= largestX;
      circle.y -= largestY;
    }
  }
  if (circles.length === 2) {
    const dist = distance(circles[0], circles[1]);
    if (dist < Math.abs(circles[1].radius - circles[0].radius)) {
      circles[1].x = circles[0].x + circles[0].radius - circles[1].radius - 1e-10;
      circles[1].y = circles[0].y;
    }
  }
  if (circles.length > 1) {
    const rotation = Math.atan2(circles[1].x, circles[1].y) - orientation;
    const c = Math.cos(rotation);
    const s = Math.sin(rotation);
    for (const circle of circles) {
      const x = circle.x;
      const y = circle.y;
      circle.x = c * x - s * y;
      circle.y = s * x + c * y;
    }
  }
  if (circles.length > 2) {
    let angle = Math.atan2(circles[2].x, circles[2].y) - orientation;
    while (angle < 0) {
      angle += 2 * Math.PI;
    }
    while (angle > 2 * Math.PI) {
      angle -= 2 * Math.PI;
    }
    if (angle > Math.PI) {
      const slope = circles[1].y / (1e-10 + circles[1].x);
      for (const circle of circles) {
        var d = (circle.x + slope * circle.y) / (1 + slope * slope);
        circle.x = 2 * d - circle.x;
        circle.y = 2 * d * slope - circle.y;
      }
    }
  }
}
__name(orientateCircles, "orientateCircles");
function disjointCluster(circles) {
  circles.forEach((circle) => {
    circle.parent = circle;
  });
  function find(circle) {
    if (circle.parent !== circle) {
      circle.parent = find(circle.parent);
    }
    return circle.parent;
  }
  __name(find, "find");
  function union(x, y) {
    const xRoot = find(x);
    const yRoot = find(y);
    xRoot.parent = yRoot;
  }
  __name(union, "union");
  for (let i = 0; i < circles.length; ++i) {
    for (let j = i + 1; j < circles.length; ++j) {
      const maxDistance = circles[i].radius + circles[j].radius;
      if (distance(circles[i], circles[j]) + 1e-10 < maxDistance) {
        union(circles[j], circles[i]);
      }
    }
  }
  const disjointClusters = /* @__PURE__ */ new Map();
  for (let i = 0; i < circles.length; ++i) {
    const setid = find(circles[i]).parent.setid;
    if (!disjointClusters.has(setid)) {
      disjointClusters.set(setid, []);
    }
    disjointClusters.get(setid).push(circles[i]);
  }
  circles.forEach((circle) => {
    delete circle.parent;
  });
  return Array.from(disjointClusters.values());
}
__name(disjointCluster, "disjointCluster");
function getBoundingBox(circles) {
  const minMax = /* @__PURE__ */ __name((d) => {
    const hi = circles.reduce((acc, c) => Math.max(acc, c[d] + c.radius), Number.NEGATIVE_INFINITY);
    const lo = circles.reduce((acc, c) => Math.min(acc, c[d] - c.radius), Number.POSITIVE_INFINITY);
    return { max: hi, min: lo };
  }, "minMax");
  return { xRange: minMax("x"), yRange: minMax("y") };
}
__name(getBoundingBox, "getBoundingBox");
function normalizeSolution(solution, orientation, orientationOrder) {
  if (orientation == null) {
    orientation = Math.PI / 2;
  }
  let circles = fromObjectNotation(solution).map((d) => Object.assign({}, d));
  const clusters = disjointCluster(circles);
  for (const cluster of clusters) {
    orientateCircles(cluster, orientation, orientationOrder);
    const bounds = getBoundingBox(cluster);
    cluster.size = (bounds.xRange.max - bounds.xRange.min) * (bounds.yRange.max - bounds.yRange.min);
    cluster.bounds = bounds;
  }
  clusters.sort((a, b) => b.size - a.size);
  circles = clusters[0];
  let returnBounds = circles.bounds;
  const spacing = (returnBounds.xRange.max - returnBounds.xRange.min) / 50;
  function addCluster(cluster, right, bottom) {
    if (!cluster) {
      return;
    }
    const bounds = cluster.bounds;
    let xOffset;
    let yOffset;
    if (right) {
      xOffset = returnBounds.xRange.max - bounds.xRange.min + spacing;
    } else {
      xOffset = returnBounds.xRange.max - bounds.xRange.max;
      const centreing = (bounds.xRange.max - bounds.xRange.min) / 2 - (returnBounds.xRange.max - returnBounds.xRange.min) / 2;
      if (centreing < 0) {
        xOffset += centreing;
      }
    }
    if (bottom) {
      yOffset = returnBounds.yRange.max - bounds.yRange.min + spacing;
    } else {
      yOffset = returnBounds.yRange.max - bounds.yRange.max;
      const centreing = (bounds.yRange.max - bounds.yRange.min) / 2 - (returnBounds.yRange.max - returnBounds.yRange.min) / 2;
      if (centreing < 0) {
        yOffset += centreing;
      }
    }
    for (const c of cluster) {
      c.x += xOffset;
      c.y += yOffset;
      circles.push(c);
    }
  }
  __name(addCluster, "addCluster");
  let index = 1;
  while (index < clusters.length) {
    addCluster(clusters[index], true, false);
    addCluster(clusters[index + 1], false, true);
    addCluster(clusters[index + 2], true, true);
    index += 3;
    returnBounds = getBoundingBox(circles);
  }
  return toObjectNotation(circles);
}
__name(normalizeSolution, "normalizeSolution");
function scaleSolution(solution, width, height, padding, scaleToFit) {
  const circles = fromObjectNotation(solution);
  width -= 2 * padding;
  height -= 2 * padding;
  const { xRange, yRange } = getBoundingBox(circles);
  if (xRange.max === xRange.min || yRange.max === yRange.min) {
    console.log("not scaling solution: zero size detected");
    return solution;
  }
  let xScaling;
  let yScaling;
  if (scaleToFit) {
    const toScaleDiameter = Math.sqrt(scaleToFit / Math.PI) * 2;
    xScaling = width / toScaleDiameter;
    yScaling = height / toScaleDiameter;
  } else {
    xScaling = width / (xRange.max - xRange.min);
    yScaling = height / (yRange.max - yRange.min);
  }
  const scaling = Math.min(yScaling, xScaling);
  const xOffset = (width - (xRange.max - xRange.min) * scaling) / 2;
  const yOffset = (height - (yRange.max - yRange.min) * scaling) / 2;
  return toObjectNotation(
    circles.map((circle) => ({
      radius: scaling * circle.radius,
      x: padding + xOffset + (circle.x - xRange.min) * scaling,
      y: padding + yOffset + (circle.y - yRange.min) * scaling,
      setid: circle.setid
    }))
  );
}
__name(scaleSolution, "scaleSolution");
function toObjectNotation(circles) {
  const r = {};
  for (const circle of circles) {
    r[circle.setid] = circle;
  }
  return r;
}
__name(toObjectNotation, "toObjectNotation");
function fromObjectNotation(solution) {
  const setids = Object.keys(solution);
  return setids.map((id) => Object.assign(solution[id], { setid: id }));
}
__name(fromObjectNotation, "fromObjectNotation");
function VennDiagram(options = {}) {
  let useViewBox = false, width = 600, height = 350, padding = 15, duration = 1e3, orientation = Math.PI / 2, normalize = true, scaleToFit = null, wrap = true, styled = true, fontSize = null, orientationOrder = null, distinct = false, round = null, symmetricalTextCentre = options && options.symmetricalTextCentre ? options.symmetricalTextCentre : false, colourMap = {}, colourScheme = options && options.colourScheme ? options.colourScheme : options && options.colorScheme ? options.colorScheme : [
    "#1f77b4",
    "#ff7f0e",
    "#2ca02c",
    "#d62728",
    "#9467bd",
    "#8c564b",
    "#e377c2",
    "#7f7f7f",
    "#bcbd22",
    "#17becf"
  ], colourIndex = 0, colours = /* @__PURE__ */ __name(function(key) {
    if (key in colourMap) {
      return colourMap[key];
    }
    var ret = colourMap[key] = colourScheme[colourIndex];
    colourIndex += 1;
    if (colourIndex >= colourScheme.length) {
      colourIndex = 0;
    }
    return ret;
  }, "colours"), layoutFunction = venn, loss = lossFunction;
  function chart(selection) {
    let data = selection.datum();
    const toRemove = /* @__PURE__ */ new Set();
    data.forEach((datum) => {
      if (datum.size == 0 && datum.sets.length == 1) {
        toRemove.add(datum.sets[0]);
      }
    });
    data = data.filter((datum) => !datum.sets.some((set) => toRemove.has(set)));
    let circles = {};
    let textCentres = {};
    if (data.length > 0) {
      let solution = layoutFunction(data, { lossFunction: loss, distinct });
      if (normalize) {
        solution = normalizeSolution(solution, orientation, orientationOrder);
      }
      circles = scaleSolution(solution, width, height, padding, scaleToFit);
      textCentres = computeTextCentres(circles, data, symmetricalTextCentre);
    }
    const labels = {};
    data.forEach((datum) => {
      if (datum.label) {
        labels[datum.sets] = datum.label;
      }
    });
    function label(d) {
      if (d.sets in labels) {
        return labels[d.sets];
      }
      if (d.sets.length == 1) {
        return "" + d.sets[0];
      }
    }
    __name(label, "label");
    selection.selectAll("svg").data([circles]).enter().append("svg");
    const svg = selection.select("svg");
    if (useViewBox) {
      svg.attr("viewBox", `0 0 ${width} ${height}`);
    } else {
      svg.attr("width", width).attr("height", height);
    }
    const previous = {};
    let hasPrevious = false;
    svg.selectAll(".venn-area path").each(function(d) {
      const path = this.getAttribute("d");
      if (d.sets.length == 1 && path && !distinct) {
        hasPrevious = true;
        previous[d.sets[0]] = circleFromPath(path);
      }
    });
    function pathTween(d) {
      return (t) => {
        const c = d.sets.map((set) => {
          let start = previous[set];
          let end = circles[set];
          if (!start) {
            start = { x: width / 2, y: height / 2, radius: 1 };
          }
          if (!end) {
            end = { x: width / 2, y: height / 2, radius: 1 };
          }
          return {
            x: start.x * (1 - t) + end.x * t,
            y: start.y * (1 - t) + end.y * t,
            radius: start.radius * (1 - t) + end.radius * t
          };
        });
        return intersectionAreaPath(c, round);
      };
    }
    __name(pathTween, "pathTween");
    const nodes = svg.selectAll(".venn-area").data(data, (d) => d.sets);
    const enter = nodes.enter().append("g").attr(
      "class",
      (d) => `venn-area venn-${d.sets.length == 1 ? "circle" : "intersection"}${d.colour || d.color ? " venn-coloured" : ""}`
    ).attr("data-venn-sets", (d) => d.sets.join("_"));
    const enterPath = enter.append("path");
    const enterText = enter.append("text").attr("class", "label").text((d) => label(d)).attr("text-anchor", "middle").attr("dy", ".35em").attr("x", width / 2).attr("y", height / 2);
    if (styled) {
      enterPath.style("fill-opacity", "0").filter((d) => d.sets.length == 1).style("fill", (d) => d.colour ? d.colour : d.color ? d.color : colours(d.sets)).style("fill-opacity", ".25");
      enterText.style("fill", (d) => {
        if (d.colour || d.color) {
          return "#FFF";
        }
        if (options.textFill) {
          return options.textFill;
        }
        return d.sets.length == 1 ? colours(d.sets) : "#444";
      });
    }
    function asTransition(s) {
      if (typeof s.transition === "function") {
        return s.transition("venn").duration(duration);
      }
      return s;
    }
    __name(asTransition, "asTransition");
    let update = selection;
    if (hasPrevious && typeof update.transition === "function") {
      update = asTransition(selection);
      update.selectAll("path").attrTween("d", pathTween);
    } else {
      update.selectAll("path").attr("d", (d) => intersectionAreaPath(d.sets.map((set) => circles[set])), round);
    }
    const updateText = update.selectAll("text").filter((d) => d.sets in textCentres).text((d) => label(d)).attr("x", (d) => Math.floor(textCentres[d.sets].x)).attr("y", (d) => Math.floor(textCentres[d.sets].y));
    if (wrap) {
      if (hasPrevious) {
        if ("on" in updateText) {
          updateText.on("end", wrapText(circles, label));
        } else {
          updateText.each("end", wrapText(circles, label));
        }
      } else {
        updateText.each(wrapText(circles, label));
      }
    }
    const exit = asTransition(nodes.exit()).remove();
    if (typeof nodes.transition === "function") {
      exit.selectAll("path").attrTween("d", pathTween);
    }
    const exitText = exit.selectAll("text").attr("x", width / 2).attr("y", height / 2);
    if (fontSize !== null) {
      enterText.style("font-size", "0px");
      updateText.style("font-size", fontSize);
      exitText.style("font-size", "0px");
    }
    return { circles, textCentres, nodes, enter, update, exit };
  }
  __name(chart, "chart");
  chart.wrap = function(_) {
    if (!arguments.length) return wrap;
    wrap = _;
    return chart;
  };
  chart.useViewBox = function() {
    useViewBox = true;
    return chart;
  };
  chart.width = function(_) {
    if (!arguments.length) return width;
    width = _;
    return chart;
  };
  chart.height = function(_) {
    if (!arguments.length) return height;
    height = _;
    return chart;
  };
  chart.padding = function(_) {
    if (!arguments.length) return padding;
    padding = _;
    return chart;
  };
  chart.distinct = function(_) {
    if (!arguments.length) return distinct;
    distinct = _;
    return chart;
  };
  chart.colours = function(_) {
    if (!arguments.length) return colours;
    colours = _;
    return chart;
  };
  chart.colors = function(_) {
    if (!arguments.length) return colours;
    colours = _;
    return chart;
  };
  chart.fontSize = function(_) {
    if (!arguments.length) return fontSize;
    fontSize = _;
    return chart;
  };
  chart.round = function(_) {
    if (!arguments.length) return round;
    round = _;
    return chart;
  };
  chart.duration = function(_) {
    if (!arguments.length) return duration;
    duration = _;
    return chart;
  };
  chart.layoutFunction = function(_) {
    if (!arguments.length) return layoutFunction;
    layoutFunction = _;
    return chart;
  };
  chart.normalize = function(_) {
    if (!arguments.length) return normalize;
    normalize = _;
    return chart;
  };
  chart.scaleToFit = function(_) {
    if (!arguments.length) return scaleToFit;
    scaleToFit = _;
    return chart;
  };
  chart.styled = function(_) {
    if (!arguments.length) return styled;
    styled = _;
    return chart;
  };
  chart.orientation = function(_) {
    if (!arguments.length) return orientation;
    orientation = _;
    return chart;
  };
  chart.orientationOrder = function(_) {
    if (!arguments.length) return orientationOrder;
    orientationOrder = _;
    return chart;
  };
  chart.lossFunction = function(_) {
    if (!arguments.length) return loss;
    loss = _ === "default" ? lossFunction : _ === "logRatio" ? logRatioLossFunction : _;
    return chart;
  };
  return chart;
}
__name(VennDiagram, "VennDiagram");
function wrapText(circles, labeller) {
  return function(data) {
    const text = this;
    const width = circles[data.sets[0]].radius || 50;
    const label = labeller(data) || "";
    const words = label.split(/\s+/).reverse();
    const maxLines = 3;
    const minChars = (label.length + words.length) / maxLines;
    let word = words.pop();
    let line = [word];
    let lineNumber = 0;
    const lineHeight = 1.1;
    text.textContent = null;
    const tspans = [];
    function append(word2) {
      const tspan2 = text.ownerDocument.createElementNS(text.namespaceURI, "tspan");
      tspan2.textContent = word2;
      tspans.push(tspan2);
      text.append(tspan2);
      return tspan2;
    }
    __name(append, "append");
    let tspan = append(word);
    while (true) {
      word = words.pop();
      if (!word) {
        break;
      }
      line.push(word);
      const joined = line.join(" ");
      tspan.textContent = joined;
      if (joined.length > minChars && tspan.getComputedTextLength() > width) {
        line.pop();
        tspan.textContent = line.join(" ");
        line = [word];
        tspan = append(word);
        lineNumber++;
      }
    }
    const initial = 0.35 - lineNumber * lineHeight / 2;
    const x = text.getAttribute("x");
    const y = text.getAttribute("y");
    tspans.forEach((t, i) => {
      t.setAttribute("x", x);
      t.setAttribute("y", y);
      t.setAttribute("dy", `${initial + i * lineHeight}em`);
    });
  };
}
__name(wrapText, "wrapText");
function circleMargin(current, interior, exterior) {
  let margin = interior[0].radius - distance(interior[0], current);
  for (let i = 1; i < interior.length; ++i) {
    const m = interior[i].radius - distance(interior[i], current);
    if (m <= margin) {
      margin = m;
    }
  }
  for (let i = 0; i < exterior.length; ++i) {
    const m = distance(exterior[i], current) - exterior[i].radius;
    if (m <= margin) {
      margin = m;
    }
  }
  return margin;
}
__name(circleMargin, "circleMargin");
function computeTextCentre(interior, exterior, symmetricalTextCentre) {
  const points = [];
  for (const c of interior) {
    points.push({ x: c.x, y: c.y });
    points.push({ x: c.x + c.radius / 2, y: c.y });
    points.push({ x: c.x - c.radius / 2, y: c.y });
    points.push({ x: c.x, y: c.y + c.radius / 2 });
    points.push({ x: c.x, y: c.y - c.radius / 2 });
  }
  let initial = points[0];
  let margin = circleMargin(points[0], interior, exterior);
  for (let i = 1; i < points.length; ++i) {
    const m = circleMargin(points[i], interior, exterior);
    if (m >= margin) {
      initial = points[i];
      margin = m;
    }
  }
  const solution = nelderMead(
    (p) => -1 * circleMargin({ x: p[0], y: p[1] }, interior, exterior),
    [initial.x, initial.y],
    { maxIterations: 500, minErrorDelta: 1e-10 }
  ).x;
  const ret = { x: symmetricalTextCentre ? 0 : solution[0], y: solution[1] };
  let valid = true;
  for (const i of interior) {
    if (distance(ret, i) > i.radius) {
      valid = false;
      break;
    }
  }
  for (const e of exterior) {
    if (distance(ret, e) < e.radius) {
      valid = false;
      break;
    }
  }
  if (valid) {
    return ret;
  }
  if (interior.length == 1) {
    return { x: interior[0].x, y: interior[0].y };
  }
  const areaStats = {};
  intersectionArea(interior, areaStats);
  if (areaStats.arcs.length === 0) {
    return { x: 0, y: -1e3, disjoint: true };
  }
  if (areaStats.arcs.length == 1) {
    return { x: areaStats.arcs[0].circle.x, y: areaStats.arcs[0].circle.y };
  }
  if (exterior.length) {
    return computeTextCentre(interior, []);
  }
  return getCenter(areaStats.arcs.map((a) => a.p1));
}
__name(computeTextCentre, "computeTextCentre");
function getOverlappingCircles(circles) {
  const ret = {};
  const circleids = Object.keys(circles);
  for (const circleid of circleids) {
    ret[circleid] = [];
  }
  for (let i = 0; i < circleids.length; i++) {
    const ci = circleids[i];
    const a = circles[ci];
    for (let j = i + 1; j < circleids.length; ++j) {
      const cj = circleids[j];
      const b = circles[cj];
      const d = distance(a, b);
      if (d + b.radius <= a.radius + 1e-10) {
        ret[cj].push(ci);
      } else if (d + a.radius <= b.radius + 1e-10) {
        ret[ci].push(cj);
      }
    }
  }
  return ret;
}
__name(getOverlappingCircles, "getOverlappingCircles");
function computeTextCentres(circles, areas, symmetricalTextCentre) {
  const ret = {};
  const overlapped = getOverlappingCircles(circles);
  for (let i = 0; i < areas.length; ++i) {
    const area = areas[i].sets;
    const areaids = {};
    const exclude = {};
    for (let j = 0; j < area.length; ++j) {
      areaids[area[j]] = true;
      const overlaps = overlapped[area[j]];
      for (let k = 0; k < overlaps.length; ++k) {
        exclude[overlaps[k]] = true;
      }
    }
    const interior = [];
    const exterior = [];
    for (let setid in circles) {
      if (setid in areaids) {
        interior.push(circles[setid]);
      } else if (!(setid in exclude)) {
        exterior.push(circles[setid]);
      }
    }
    const centre = computeTextCentre(interior, exterior, symmetricalTextCentre);
    ret[area] = centre;
    if (centre.disjoint && areas[i].size > 0) {
      console.log("WARNING: area " + area + " not represented on screen");
    }
  }
  return ret;
}
__name(computeTextCentres, "computeTextCentres");
function circlePath(x, y, r) {
  const ret = [];
  ret.push("\nM", x, y);
  ret.push("\nm", -r, 0);
  ret.push("\na", r, r, 0, 1, 0, r * 2, 0);
  ret.push("\na", r, r, 0, 1, 0, -r * 2, 0);
  return ret.join(" ");
}
__name(circlePath, "circlePath");
function circleFromPath(path) {
  const tokens = path.split(" ");
  return { x: Number.parseFloat(tokens[1]), y: Number.parseFloat(tokens[2]), radius: -Number.parseFloat(tokens[4]) };
}
__name(circleFromPath, "circleFromPath");
function intersectionAreaArcs(circles) {
  if (circles.length === 0) {
    return [];
  }
  const stats = {};
  intersectionArea(circles, stats);
  return stats.arcs;
}
__name(intersectionAreaArcs, "intersectionAreaArcs");
function arcsToPath(arcs, round) {
  if (arcs.length === 0) {
    return "M 0 0";
  }
  const rFactor = Math.pow(10, round || 0);
  const r = round != null ? (v) => Math.round(v * rFactor) / rFactor : (v) => v;
  if (arcs.length == 1) {
    const circle = arcs[0].circle;
    return circlePath(r(circle.x), r(circle.y), r(circle.radius));
  }
  const ret = ["\nM", r(arcs[0].p2.x), r(arcs[0].p2.y)];
  for (const arc of arcs) {
    const radius = r(arc.circle.radius);
    ret.push("\nA", radius, radius, 0, arc.large ? 1 : 0, arc.sweep ? 1 : 0, r(arc.p1.x), r(arc.p1.y));
  }
  return ret.join(" ");
}
__name(arcsToPath, "arcsToPath");
function intersectionAreaPath(circles, round) {
  return arcsToPath(intersectionAreaArcs(circles), round);
}
__name(intersectionAreaPath, "intersectionAreaPath");
function layout(data, options = {}) {
  const {
    lossFunction: loss,
    layoutFunction: layout2 = venn,
    normalize = true,
    orientation = Math.PI / 2,
    orientationOrder,
    width = 600,
    height = 350,
    padding = 15,
    scaleToFit = false,
    symmetricalTextCentre = false,
    distinct,
    round = 2
  } = options;
  let solution = layout2(data, {
    lossFunction: loss === "default" || !loss ? lossFunction : loss === "logRatio" ? logRatioLossFunction : loss,
    distinct
  });
  if (normalize) {
    solution = normalizeSolution(solution, orientation, orientationOrder);
  }
  const circles = scaleSolution(solution, width, height, padding, scaleToFit);
  const textCentres = computeTextCentres(circles, data, symmetricalTextCentre);
  const circleLookup = new Map(
    Object.keys(circles).map((set) => [
      set,
      {
        set,
        x: circles[set].x,
        y: circles[set].y,
        radius: circles[set].radius
      }
    ])
  );
  const helpers = data.map((area) => {
    const circles2 = area.sets.map((s) => circleLookup.get(s));
    const arcs = intersectionAreaArcs(circles2);
    const path = arcsToPath(arcs, round);
    return { circles: circles2, arcs, path, area, has: new Set(area.sets) };
  });
  function genDistinctPath(sets) {
    let r = "";
    for (const e of helpers) {
      if (e.has.size > sets.length && sets.every((s) => e.has.has(s))) {
        r += " " + e.path;
      }
    }
    return r;
  }
  __name(genDistinctPath, "genDistinctPath");
  return helpers.map(({ circles: circles2, arcs, path, area }) => {
    return {
      data: area,
      text: textCentres[area.sets],
      circles: circles2,
      arcs,
      path,
      distinctPath: path + genDistinctPath(area.sets)
    };
  });
}
__name(layout, "layout");

// src/diagrams/venn/vennRenderer.ts
function buildStyleByKey(styleData) {
  const map = /* @__PURE__ */ new Map();
  for (const entry of styleData) {
    const key = entry.targets.join("|");
    const existing = map.get(key);
    if (existing) {
      Object.assign(existing, entry.styles);
    } else {
      map.set(key, { ...entry.styles });
    }
  }
  return map;
}
__name(buildStyleByKey, "buildStyleByKey");
var draw = /* @__PURE__ */ __name((_text, id, _version, diagObj) => {
  const db2 = diagObj.db;
  const config = db2.getConfig?.();
  const { themeVariables, look, handDrawnSeed } = getConfig();
  const isHandDrawn = look === "handDrawn";
  const themeColors = [
    themeVariables.venn1,
    themeVariables.venn2,
    themeVariables.venn3,
    themeVariables.venn4,
    themeVariables.venn5,
    themeVariables.venn6,
    themeVariables.venn7,
    themeVariables.venn8
  ].filter(Boolean);
  const title = db2.getDiagramTitle?.();
  const sets = db2.getSubsetData();
  const textNodes2 = db2.getTextData();
  const styleByKey = buildStyleByKey(db2.getStyleData());
  const renderSets = ensurePairwiseSubsets(sets);
  const svgWidth = config?.width ?? 800;
  const svgHeight = config?.height ?? 450;
  const REFERENCE_WIDTH = 1600;
  const scale2 = svgWidth / REFERENCE_WIDTH;
  const titleHeight = title ? 48 * scale2 : 0;
  const defaultTextColor = themeVariables.primaryTextColor ?? themeVariables.textColor;
  const svg = selectSvgElement(id);
  svg.attr("viewBox", `0 0 ${svgWidth} ${svgHeight}`);
  if (title) {
    svg.append("text").text(title).attr("class", "venn-title").attr("font-size", `${32 * scale2}px`).attr("text-anchor", "middle").attr("dominant-baseline", "middle").attr("x", "50%").attr("y", 32 * scale2).style("fill", themeVariables.vennTitleTextColor || themeVariables.titleColor);
  }
  const dummyD3root = select_default(document.createElement("div"));
  const vennDiagram = VennDiagram().width(svgWidth).height(svgHeight - titleHeight);
  dummyD3root.datum(renderSets).call(vennDiagram);
  const roughSvg = isHandDrawn ? at.svg(dummyD3root.select("svg").node()) : void 0;
  const layoutAreas = layout(renderSets, {
    width: svgWidth,
    height: svgHeight - titleHeight,
    padding: config?.padding ?? 15
  });
  const layoutByKey = /* @__PURE__ */ new Map();
  for (const area of layoutAreas) {
    const key = stableSetsKey([...area.data.sets].sort());
    layoutByKey.set(key, area);
  }
  if (textNodes2.length > 0) {
    renderTextNodes(config, layoutByKey, dummyD3root, textNodes2, scale2, styleByKey);
  }
  const themeDark = is_dark_default(themeVariables.background || "#f4f4f4");
  dummyD3root.selectAll(".venn-circle").each(function(d, i) {
    const group = select_default(this);
    const data = d;
    const setsKey = stableSetsKey([...data.sets].sort());
    const customStyle = styleByKey.get(setsKey);
    const baseColor = customStyle?.fill || themeColors[i % themeColors.length] || themeVariables.primaryColor;
    group.classed(`venn-set-${i % 8}`, true);
    const fillOpacity = customStyle?.["fill-opacity"] ?? 0.1;
    const strokeColor = customStyle?.stroke || baseColor;
    const strokeWidthVal = customStyle?.["stroke-width"] || `${5 * scale2}`;
    if (isHandDrawn && roughSvg) {
      const layoutArea = layoutByKey.get(setsKey);
      if (layoutArea && layoutArea.circles.length > 0) {
        const c = layoutArea.circles[0];
        const roughNode = roughSvg.circle(c.x, c.y, c.radius * 2, {
          roughness: 0.7,
          seed: handDrawnSeed,
          fill: transparentize_default(baseColor, 0.7),
          fillStyle: "hachure",
          fillWeight: 2,
          hachureGap: 8,
          hachureAngle: -41 + i * 60,
          stroke: strokeColor,
          strokeWidth: parseFloat(String(strokeWidthVal))
        });
        group.select("path").remove();
        group.node()?.insertBefore(roughNode, group.select("text").node());
      }
    } else {
      group.select("path").style("fill", baseColor).style("fill-opacity", fillOpacity).style("stroke", strokeColor).style("stroke-width", strokeWidthVal).style("stroke-opacity", 0.95);
    }
    const textColor = customStyle?.color || (themeDark ? lighten_default(baseColor, 30) : darken_default(baseColor, 30));
    group.select("text").style("font-size", `${48 * scale2}px`).style("fill", textColor);
  });
  if (isHandDrawn && roughSvg) {
    dummyD3root.selectAll(".venn-intersection").each(function(d) {
      const group = select_default(this);
      const data = d;
      const setsKey = stableSetsKey([...data.sets].sort());
      const customStyle = styleByKey.get(setsKey);
      const customFill = customStyle?.fill;
      if (customFill) {
        const pathEl = group.select("path");
        const pathD = pathEl.attr("d");
        if (pathD) {
          const roughNode = roughSvg.path(pathD, {
            roughness: 0.7,
            seed: handDrawnSeed,
            fill: transparentize_default(customFill, 0.3),
            fillStyle: "cross-hatch",
            fillWeight: 2,
            hachureGap: 6,
            hachureAngle: 60,
            stroke: "none"
          });
          const existingPath = pathEl.node();
          existingPath?.parentNode?.insertBefore(roughNode, existingPath);
          pathEl.remove();
        }
      } else {
        group.select("path").style("fill-opacity", 0);
      }
      group.select("text").style("font-size", `${48 * scale2}px`).style("fill", customStyle?.color ?? themeVariables.vennSetTextColor ?? defaultTextColor);
    });
  } else {
    dummyD3root.selectAll(".venn-intersection text").style("font-size", `${48 * scale2}px`).style("fill", (e) => {
      const data = e;
      const setsKey = stableSetsKey([...data.sets].sort());
      return styleByKey.get(setsKey)?.color ?? themeVariables.vennSetTextColor ?? defaultTextColor;
    });
    dummyD3root.selectAll(".venn-intersection path").style("fill-opacity", (e) => {
      const data = e;
      const setsKey = stableSetsKey([...data.sets].sort());
      return styleByKey.get(setsKey)?.fill ? 1 : 0;
    }).style("fill", (e) => {
      const data = e;
      const setsKey = stableSetsKey([...data.sets].sort());
      return styleByKey.get(setsKey)?.fill ?? "transparent";
    });
  }
  const vennGroup = svg.append("g").attr("transform", `translate(0, ${titleHeight})`);
  const dummySvg = dummyD3root.select("svg").node();
  if (dummySvg && "childNodes" in dummySvg) {
    for (const child of [...dummySvg.childNodes]) {
      vennGroup.node()?.appendChild(child);
    }
  }
  configureSvgSize(svg, svgHeight, svgWidth, config?.useMaxWidth ?? true);
}, "draw");
function stableSetsKey(setIds) {
  return setIds.join("|");
}
__name(stableSetsKey, "stableSetsKey");
function renderTextNodes(config, layoutByKey, dummyD3root, textNodes2, scale2, styleByKey) {
  const useDebugLayout = config?.useDebugLayout ?? false;
  const vennSvg = dummyD3root.select("svg");
  const textGroup = vennSvg.append("g").attr("class", "venn-text-nodes");
  const nodesByArea = /* @__PURE__ */ new Map();
  for (const node of textNodes2) {
    const key = stableSetsKey(node.sets);
    const existing = nodesByArea.get(key);
    if (existing) {
      existing.push(node);
    } else {
      nodesByArea.set(key, [node]);
    }
  }
  for (const [key, nodes] of nodesByArea.entries()) {
    const area = layoutByKey.get(key);
    if (!area?.text) {
      continue;
    }
    const centerX = area.text.x;
    const centerY = area.text.y;
    const minCircleRadius = Math.min(...area.circles.map((c) => c.radius));
    const innerRadiusRaw = Math.min(
      ...area.circles.map((c) => c.radius - Math.hypot(centerX - c.x, centerY - c.y))
    );
    let innerRadius = Number.isFinite(innerRadiusRaw) ? Math.max(0, innerRadiusRaw) : 0;
    if (innerRadius === 0 && Number.isFinite(minCircleRadius)) {
      innerRadius = minCircleRadius * 0.6;
    }
    const areaGroup = textGroup.append("g").attr("class", "venn-text-area").attr("font-size", `${40 * scale2}px`);
    if (useDebugLayout) {
      areaGroup.append("circle").attr("class", "venn-text-debug-circle").attr("cx", centerX).attr("cy", centerY).attr("r", innerRadius).attr("fill", "none").attr("stroke", "purple").attr("stroke-width", 1.5 * scale2).attr("stroke-dasharray", `${6 * scale2} ${4 * scale2}`);
    }
    const innerWidth = Math.max(80 * scale2, innerRadius * 2 * 0.95);
    const innerHeight = Math.max(60 * scale2, innerRadius * 2 * 0.95);
    const hasLabel = area.data.label && area.data.label.length > 0;
    const labelOffsetBase = hasLabel ? Math.min(32 * scale2, innerRadius * 0.25) : 0;
    const labelOffset = labelOffsetBase + (nodes.length <= 2 ? 30 * scale2 : 0);
    const startX = centerX - innerWidth / 2;
    const startY = centerY - innerHeight / 2 + labelOffset;
    const cols = Math.max(1, Math.ceil(Math.sqrt(nodes.length)));
    const rows = Math.max(1, Math.ceil(nodes.length / cols));
    const cellWidth = innerWidth / cols;
    const cellHeight = innerHeight / rows;
    for (const [i, node] of nodes.entries()) {
      const col = i % cols;
      const row = Math.floor(i / cols);
      const x = startX + cellWidth * (col + 0.5);
      const y = startY + cellHeight * (row + 0.5);
      if (useDebugLayout) {
        areaGroup.append("rect").attr("class", "venn-text-debug-cell").attr("x", startX + cellWidth * col).attr("y", startY + cellHeight * row).attr("width", cellWidth).attr("height", cellHeight).attr("fill", "none").attr("stroke", "teal").attr("stroke-width", 1 * scale2).attr("stroke-dasharray", `${4 * scale2} ${3 * scale2}`);
      }
      const boxWidth = cellWidth * 0.9;
      const boxHeight = cellHeight * 0.9;
      const container = areaGroup.append("foreignObject").attr("class", "venn-text-node-fo").attr("width", boxWidth).attr("height", boxHeight).attr("x", x - boxWidth / 2).attr("y", y - boxHeight / 2).attr("overflow", "visible");
      const textColor = styleByKey.get(node.id)?.color;
      const text = container.append("xhtml:span").attr("class", "venn-text-node").style("display", "flex").style("width", "100%").style("height", "100%").style("white-space", "normal").style("align-items", "center").style("justify-content", "center").style("text-align", "center").style("overflow-wrap", "normal").style("word-break", "normal").text(node.label ?? node.id);
      if (textColor) {
        text.style("color", textColor);
      }
    }
  }
}
__name(renderTextNodes, "renderTextNodes");
function ensurePairwiseSubsets(subsets2) {
  const existingKeys = new Set(subsets2.map((s) => [...s.sets].sort().join("|")));
  const individualSetSizes = new Map(
    subsets2.filter((s) => s.sets.length === 1 && s.size !== void 0).map((s) => [s.sets[0], s.size])
  );
  const synthetic = [];
  for (const subset of subsets2) {
    if (subset.sets.length < 3) {
      continue;
    }
    const members = [...subset.sets].sort();
    for (let i = 0; i < members.length - 1; i++) {
      for (let j = i + 1; j < members.length; j++) {
        const pair = [members[i], members[j]];
        const key = pair.join("|");
        if (!existingKeys.has(key)) {
          existingKeys.add(key);
          const sizeA = individualSetSizes.get(pair[0]);
          const sizeB = individualSetSizes.get(pair[1]);
          const pairSize = sizeA !== void 0 && sizeB !== void 0 ? Math.min(sizeA, sizeB) / 4 : 2.5;
          synthetic.push({ sets: pair, size: pairSize, label: "" });
        }
      }
    }
  }
  return synthetic.length > 0 ? [...subsets2, ...synthetic] : subsets2;
}
__name(ensurePairwiseSubsets, "ensurePairwiseSubsets");
var renderer = { draw };

// src/diagrams/venn/vennDiagram.ts
var diagram = {
  parser: venn_default,
  db,
  renderer,
  styles: styles_default
};
export {
  diagram
};
