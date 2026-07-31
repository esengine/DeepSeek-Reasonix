import {
  selectSvgElement
} from "./chunk-VAUOI2AC.mjs";
import {
  cleanAndMerge
} from "./chunk-ICXQ74PX.mjs";
import {
  clear,
  configureSvgSize,
  defaultConfig_default,
  getAccDescription,
  getAccTitle,
  getConfig,
  getDiagramTitle,
  setAccDescription,
  setAccTitle,
  setDiagramTitle
} from "./chunk-WYO6CB5R.mjs";
import "./chunk-X3CZISLH.mjs";
import {
  __name
} from "./chunk-Y2CYZVJY.mjs";

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

// src/diagrams/venn/vennRenderer.ts
import { select as d3select } from "d3";
import { isDark, lighten, darken, transparentize } from "khroma";
import * as venn from "@upsetjs/venn.js";
import rough from "roughjs";
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
  const scale = svgWidth / REFERENCE_WIDTH;
  const titleHeight = title ? 48 * scale : 0;
  const defaultTextColor = themeVariables.primaryTextColor ?? themeVariables.textColor;
  const svg = selectSvgElement(id);
  svg.attr("viewBox", `0 0 ${svgWidth} ${svgHeight}`);
  if (title) {
    svg.append("text").text(title).attr("class", "venn-title").attr("font-size", `${32 * scale}px`).attr("text-anchor", "middle").attr("dominant-baseline", "middle").attr("x", "50%").attr("y", 32 * scale).style("fill", themeVariables.vennTitleTextColor || themeVariables.titleColor);
  }
  const dummyD3root = d3select(document.createElement("div"));
  const vennDiagram = venn.VennDiagram().width(svgWidth).height(svgHeight - titleHeight);
  dummyD3root.datum(renderSets).call(vennDiagram);
  const roughSvg = isHandDrawn ? rough.svg(dummyD3root.select("svg").node()) : void 0;
  const layoutAreas = venn.layout(renderSets, {
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
    renderTextNodes(config, layoutByKey, dummyD3root, textNodes2, scale, styleByKey);
  }
  const themeDark = isDark(themeVariables.background || "#f4f4f4");
  dummyD3root.selectAll(".venn-circle").each(function(d, i) {
    const group = d3select(this);
    const data = d;
    const setsKey = stableSetsKey([...data.sets].sort());
    const customStyle = styleByKey.get(setsKey);
    const baseColor = customStyle?.fill || themeColors[i % themeColors.length] || themeVariables.primaryColor;
    group.classed(`venn-set-${i % 8}`, true);
    const fillOpacity = customStyle?.["fill-opacity"] ?? 0.1;
    const strokeColor = customStyle?.stroke || baseColor;
    const strokeWidthVal = customStyle?.["stroke-width"] || `${5 * scale}`;
    if (isHandDrawn && roughSvg) {
      const layoutArea = layoutByKey.get(setsKey);
      if (layoutArea && layoutArea.circles.length > 0) {
        const c = layoutArea.circles[0];
        const roughNode = roughSvg.circle(c.x, c.y, c.radius * 2, {
          roughness: 0.7,
          seed: handDrawnSeed,
          fill: transparentize(baseColor, 0.7),
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
    const textColor = customStyle?.color || (themeDark ? lighten(baseColor, 30) : darken(baseColor, 30));
    group.select("text").style("font-size", `${48 * scale}px`).style("fill", textColor);
  });
  if (isHandDrawn && roughSvg) {
    dummyD3root.selectAll(".venn-intersection").each(function(d) {
      const group = d3select(this);
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
            fill: transparentize(customFill, 0.3),
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
      group.select("text").style("font-size", `${48 * scale}px`).style("fill", customStyle?.color ?? themeVariables.vennSetTextColor ?? defaultTextColor);
    });
  } else {
    dummyD3root.selectAll(".venn-intersection text").style("font-size", `${48 * scale}px`).style("fill", (e) => {
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
function renderTextNodes(config, layoutByKey, dummyD3root, textNodes2, scale, styleByKey) {
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
    const areaGroup = textGroup.append("g").attr("class", "venn-text-area").attr("font-size", `${40 * scale}px`);
    if (useDebugLayout) {
      areaGroup.append("circle").attr("class", "venn-text-debug-circle").attr("cx", centerX).attr("cy", centerY).attr("r", innerRadius).attr("fill", "none").attr("stroke", "purple").attr("stroke-width", 1.5 * scale).attr("stroke-dasharray", `${6 * scale} ${4 * scale}`);
    }
    const innerWidth = Math.max(80 * scale, innerRadius * 2 * 0.95);
    const innerHeight = Math.max(60 * scale, innerRadius * 2 * 0.95);
    const hasLabel = area.data.label && area.data.label.length > 0;
    const labelOffsetBase = hasLabel ? Math.min(32 * scale, innerRadius * 0.25) : 0;
    const labelOffset = labelOffsetBase + (nodes.length <= 2 ? 30 * scale : 0);
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
        areaGroup.append("rect").attr("class", "venn-text-debug-cell").attr("x", startX + cellWidth * col).attr("y", startY + cellHeight * row).attr("width", cellWidth).attr("height", cellHeight).attr("fill", "none").attr("stroke", "teal").attr("stroke-width", 1 * scale).attr("stroke-dasharray", `${4 * scale} ${3 * scale}`);
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
