import {
  getIconStyles
} from "./chunk-RTBOCTTP.mjs";
import {
  getDiagramElement
} from "./chunk-HPLX5OYV.mjs";
import {
  setupViewPortForSVG
} from "./chunk-DRARJEGU.mjs";
import {
  getRegisteredLayoutAlgorithm,
  render
} from "./chunk-FW7PRHBN.mjs";
import {
  createTooltip
} from "./chunk-QVUG6NDL.mjs";
import {
  getEdgeId,
  utils_default
} from "./chunk-MMGVDTGO.mjs";
import {
  clear,
  common_default,
  getAccDescription,
  getAccTitle,
  getConfig2 as getConfig,
  getDiagramTitle,
  parseGenericTypes,
  purify,
  sanitizeText,
  setAccDescription,
  setAccTitle,
  setDiagramTitle
} from "./chunk-CHAKFXHA.mjs";
import {
  log,
  select_default
} from "./chunk-FO5PYUIK.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// src/diagrams/class/parser/classDiagram.jison
var parser = (function() {
  var o = /* @__PURE__ */ __name(function(k, v, o2, l) {
    for (o2 = o2 || {}, l = k.length; l--; o2[k[l]] = v) ;
    return o2;
  }, "o"), $V0 = [1, 18], $V1 = [1, 19], $V2 = [1, 20], $V3 = [1, 41], $V4 = [1, 26], $V5 = [1, 42], $V6 = [1, 24], $V7 = [1, 25], $V8 = [1, 32], $V9 = [1, 33], $Va = [1, 34], $Vb = [1, 45], $Vc = [1, 35], $Vd = [1, 36], $Ve = [1, 37], $Vf = [1, 38], $Vg = [1, 27], $Vh = [1, 28], $Vi = [1, 29], $Vj = [1, 30], $Vk = [1, 31], $Vl = [1, 44], $Vm = [1, 46], $Vn = [1, 43], $Vo = [1, 47], $Vp = [1, 9], $Vq = [1, 8, 9], $Vr = [1, 58], $Vs = [1, 59], $Vt = [1, 60], $Vu = [1, 61], $Vv = [1, 62], $Vw = [1, 63], $Vx = [1, 64], $Vy = [1, 8, 9, 41], $Vz = [1, 77], $VA = [1, 8, 9, 12, 13, 22, 39, 41, 44, 46, 68, 69, 70, 71, 72, 73, 74, 79, 81], $VB = [1, 8, 9, 12, 13, 18, 20, 22, 39, 41, 44, 46, 47, 60, 68, 69, 70, 71, 72, 73, 74, 79, 81, 86, 100, 102, 103], $VC = [13, 60, 86, 100, 102, 103], $VD = [13, 60, 73, 74, 86, 100, 102, 103], $VE = [13, 60, 68, 69, 70, 71, 72, 86, 100, 102, 103], $VF = [1, 103], $VG = [1, 121], $VH = [1, 117], $VI = [1, 113], $VJ = [1, 119], $VK = [1, 114], $VL = [1, 115], $VM = [1, 116], $VN = [1, 118], $VO = [1, 120], $VP = [22, 50, 60, 61, 82, 86, 87, 88, 89, 90], $VQ = [1, 128], $VR = [12, 39], $VS = [1, 8, 9, 39, 41, 44, 46], $VT = [1, 8, 9, 22], $VU = [1, 153], $VV = [1, 8, 9, 61], $VW = [1, 8, 9, 22, 50, 60, 61, 82, 86, 87, 88, 89, 90];
  var parser2 = {
    trace: /* @__PURE__ */ __name(function trace() {
    }, "trace"),
    yy: {},
    symbols_: { "error": 2, "start": 3, "mermaidDoc": 4, "statements": 5, "graphConfig": 6, "CLASS_DIAGRAM": 7, "NEWLINE": 8, "EOF": 9, "statement": 10, "classLabel": 11, "SQS": 12, "STR": 13, "SQE": 14, "namespaceName": 15, "alphaNumToken": 16, "classLiteralName": 17, "DOT": 18, "className": 19, "GENERICTYPE": 20, "relationStatement": 21, "LABEL": 22, "namespaceStatement": 23, "classStatement": 24, "memberStatement": 25, "annotationStatement": 26, "clickStatement": 27, "styleStatement": 28, "cssClassStatement": 29, "noteStatement": 30, "classDefStatement": 31, "direction": 32, "acc_title": 33, "acc_title_value": 34, "acc_descr": 35, "acc_descr_value": 36, "acc_descr_multiline_value": 37, "namespaceIdentifier": 38, "STRUCT_START": 39, "classStatements": 40, "STRUCT_STOP": 41, "NAMESPACE": 42, "classIdentifier": 43, "STYLE_SEPARATOR": 44, "members": 45, "ANNOTATION_START": 46, "ANNOTATION_END": 47, "CLASS": 48, "emptyBody": 49, "SPACE": 50, "MEMBER": 51, "SEPARATOR": 52, "relation": 53, "NOTE_FOR": 54, "noteText": 55, "NOTE": 56, "CLASSDEF": 57, "classList": 58, "stylesOpt": 59, "ALPHA": 60, "COMMA": 61, "direction_tb": 62, "direction_bt": 63, "direction_rl": 64, "direction_lr": 65, "relationType": 66, "lineType": 67, "AGGREGATION": 68, "EXTENSION": 69, "COMPOSITION": 70, "DEPENDENCY": 71, "LOLLIPOP": 72, "LINE": 73, "DOTTED_LINE": 74, "CALLBACK": 75, "LINK": 76, "LINK_TARGET": 77, "CLICK": 78, "CALLBACK_NAME": 79, "CALLBACK_ARGS": 80, "HREF": 81, "STYLE": 82, "CSSCLASS": 83, "style": 84, "styleComponent": 85, "NUM": 86, "COLON": 87, "UNIT": 88, "BRKT": 89, "PCT": 90, "commentToken": 91, "textToken": 92, "graphCodeTokens": 93, "textNoTagsToken": 94, "TAGSTART": 95, "TAGEND": 96, "==": 97, "--": 98, "DEFAULT": 99, "MINUS": 100, "keywords": 101, "UNICODE_TEXT": 102, "BQUOTE_STR": 103, "$accept": 0, "$end": 1 },
    terminals_: { 2: "error", 7: "CLASS_DIAGRAM", 8: "NEWLINE", 9: "EOF", 12: "SQS", 13: "STR", 14: "SQE", 18: "DOT", 20: "GENERICTYPE", 22: "LABEL", 33: "acc_title", 34: "acc_title_value", 35: "acc_descr", 36: "acc_descr_value", 37: "acc_descr_multiline_value", 39: "STRUCT_START", 41: "STRUCT_STOP", 42: "NAMESPACE", 44: "STYLE_SEPARATOR", 46: "ANNOTATION_START", 47: "ANNOTATION_END", 48: "CLASS", 50: "SPACE", 51: "MEMBER", 52: "SEPARATOR", 54: "NOTE_FOR", 56: "NOTE", 57: "CLASSDEF", 60: "ALPHA", 61: "COMMA", 62: "direction_tb", 63: "direction_bt", 64: "direction_rl", 65: "direction_lr", 68: "AGGREGATION", 69: "EXTENSION", 70: "COMPOSITION", 71: "DEPENDENCY", 72: "LOLLIPOP", 73: "LINE", 74: "DOTTED_LINE", 75: "CALLBACK", 76: "LINK", 77: "LINK_TARGET", 78: "CLICK", 79: "CALLBACK_NAME", 80: "CALLBACK_ARGS", 81: "HREF", 82: "STYLE", 83: "CSSCLASS", 86: "NUM", 87: "COLON", 88: "UNIT", 89: "BRKT", 90: "PCT", 93: "graphCodeTokens", 95: "TAGSTART", 96: "TAGEND", 97: "==", 98: "--", 99: "DEFAULT", 100: "MINUS", 101: "keywords", 102: "UNICODE_TEXT", 103: "BQUOTE_STR" },
    productions_: [0, [3, 1], [3, 1], [4, 1], [6, 4], [5, 1], [5, 2], [5, 3], [11, 3], [15, 1], [15, 1], [15, 3], [15, 2], [19, 1], [19, 3], [19, 1], [19, 2], [19, 2], [19, 2], [10, 1], [10, 2], [10, 1], [10, 1], [10, 1], [10, 1], [10, 1], [10, 1], [10, 1], [10, 1], [10, 1], [10, 1], [10, 2], [10, 2], [10, 1], [23, 4], [23, 5], [38, 2], [38, 3], [40, 1], [40, 2], [40, 3], [40, 1], [40, 2], [40, 3], [40, 1], [40, 2], [40, 3], [24, 1], [24, 3], [24, 4], [24, 3], [24, 6], [24, 4], [24, 7], [24, 6], [43, 2], [43, 3], [49, 0], [49, 2], [49, 2], [26, 4], [45, 1], [45, 2], [25, 1], [25, 2], [25, 1], [25, 1], [21, 3], [21, 4], [21, 4], [21, 5], [30, 3], [30, 2], [31, 3], [58, 1], [58, 3], [32, 1], [32, 1], [32, 1], [32, 1], [53, 3], [53, 2], [53, 2], [53, 1], [66, 1], [66, 1], [66, 1], [66, 1], [66, 1], [67, 1], [67, 1], [27, 3], [27, 4], [27, 3], [27, 4], [27, 4], [27, 5], [27, 3], [27, 4], [27, 4], [27, 5], [27, 4], [27, 5], [27, 5], [27, 6], [28, 3], [29, 3], [59, 1], [59, 3], [84, 1], [84, 2], [85, 1], [85, 1], [85, 1], [85, 1], [85, 1], [85, 1], [85, 1], [85, 1], [85, 1], [91, 1], [91, 1], [92, 1], [92, 1], [92, 1], [92, 1], [92, 1], [92, 1], [92, 1], [94, 1], [94, 1], [94, 1], [94, 1], [16, 1], [16, 1], [16, 1], [16, 1], [17, 1], [55, 1]],
    performAction: /* @__PURE__ */ __name(function anonymous(yytext, yyleng, yylineno, yy, yystate, $$, _$) {
      var $0 = $$.length - 1;
      switch (yystate) {
        case 8:
          this.$ = $$[$0 - 1];
          break;
        case 9:
        case 10:
        case 13:
        case 15:
          this.$ = $$[$0];
          break;
        case 11:
        case 14:
          this.$ = $$[$0 - 2] + "." + $$[$0];
          break;
        case 12:
        case 16:
          this.$ = $$[$0 - 1] + $$[$0];
          break;
        case 17:
        case 18:
          this.$ = $$[$0 - 1] + "~" + $$[$0] + "~";
          break;
        case 19:
          yy.addRelation($$[$0]);
          break;
        case 20:
          $$[$0 - 1].title = yy.cleanupLabel($$[$0]);
          yy.addRelation($$[$0 - 1]);
          break;
        case 31:
          this.$ = $$[$0].trim();
          yy.setAccTitle(this.$);
          break;
        case 32:
        case 33:
          this.$ = $$[$0].trim();
          yy.setAccDescription(this.$);
          break;
        case 34:
          yy.addClassesToNamespace($$[$0 - 3], $$[$0 - 1][0], $$[$0 - 1][1]);
          yy.popNamespace();
          break;
        case 35:
          yy.addClassesToNamespace($$[$0 - 4], $$[$0 - 1][0], $$[$0 - 1][1]);
          yy.popNamespace();
          break;
        case 36:
          this.$ = yy.addNamespace($$[$0]);
          break;
        case 37:
          this.$ = yy.addNamespace($$[$0 - 1], $$[$0]);
          break;
        case 38:
          this.$ = [[$$[$0]], []];
          break;
        case 39:
          this.$ = [[$$[$0 - 1]], []];
          break;
        case 40:
          $$[$0][0].unshift($$[$0 - 2]);
          this.$ = $$[$0];
          break;
        case 41:
          this.$ = [[], [$$[$0]]];
          break;
        case 42:
          this.$ = [[], [$$[$0 - 1]]];
          break;
        case 43:
          $$[$0][1].unshift($$[$0 - 2]);
          this.$ = $$[$0];
          break;
        case 44:
        case 45:
          this.$ = [[], []];
          break;
        case 46:
          this.$ = $$[$0];
          break;
        case 48:
          yy.setCssClass($$[$0 - 2], $$[$0]);
          break;
        case 49:
          yy.addMembers($$[$0 - 3], $$[$0 - 1]);
          break;
        case 51:
          yy.setCssClass($$[$0 - 5], $$[$0 - 3]);
          yy.addMembers($$[$0 - 5], $$[$0 - 1]);
          break;
        case 52:
          yy.addAnnotation($$[$0 - 3], $$[$0 - 1]);
          break;
        case 53:
          yy.addAnnotation($$[$0 - 6], $$[$0 - 4]);
          yy.addMembers($$[$0 - 6], $$[$0 - 1]);
          break;
        case 54:
          yy.addAnnotation($$[$0 - 5], $$[$0 - 3]);
          break;
        case 55:
          this.$ = $$[$0];
          yy.addClass($$[$0]);
          break;
        case 56:
          this.$ = $$[$0 - 1];
          yy.addClass($$[$0 - 1]);
          yy.setClassLabel($$[$0 - 1], $$[$0]);
          break;
        case 60:
          yy.addAnnotation($$[$0], $$[$0 - 2]);
          break;
        case 61:
        case 74:
          this.$ = [$$[$0]];
          break;
        case 62:
          $$[$0].push($$[$0 - 1]);
          this.$ = $$[$0];
          break;
        case 63:
          break;
        case 64:
          yy.addMember($$[$0 - 1], yy.cleanupLabel($$[$0]));
          break;
        case 65:
          break;
        case 66:
          break;
        case 67:
          this.$ = { "id1": $$[$0 - 2], "id2": $$[$0], relation: $$[$0 - 1], relationTitle1: "none", relationTitle2: "none" };
          break;
        case 68:
          this.$ = { id1: $$[$0 - 3], id2: $$[$0], relation: $$[$0 - 1], relationTitle1: $$[$0 - 2], relationTitle2: "none" };
          break;
        case 69:
          this.$ = { id1: $$[$0 - 3], id2: $$[$0], relation: $$[$0 - 2], relationTitle1: "none", relationTitle2: $$[$0 - 1] };
          break;
        case 70:
          this.$ = { id1: $$[$0 - 4], id2: $$[$0], relation: $$[$0 - 2], relationTitle1: $$[$0 - 3], relationTitle2: $$[$0 - 1] };
          break;
        case 71:
          this.$ = yy.addNote($$[$0], $$[$0 - 1]);
          break;
        case 72:
          this.$ = yy.addNote($$[$0]);
          break;
        case 73:
          this.$ = $$[$0 - 2];
          yy.defineClass($$[$0 - 1], $$[$0]);
          break;
        case 75:
          this.$ = $$[$0 - 2].concat([$$[$0]]);
          break;
        case 76:
          yy.setDirection("TB");
          break;
        case 77:
          yy.setDirection("BT");
          break;
        case 78:
          yy.setDirection("RL");
          break;
        case 79:
          yy.setDirection("LR");
          break;
        case 80:
          this.$ = { type1: $$[$0 - 2], type2: $$[$0], lineType: $$[$0 - 1] };
          break;
        case 81:
          this.$ = { type1: "none", type2: $$[$0], lineType: $$[$0 - 1] };
          break;
        case 82:
          this.$ = { type1: $$[$0 - 1], type2: "none", lineType: $$[$0] };
          break;
        case 83:
          this.$ = { type1: "none", type2: "none", lineType: $$[$0] };
          break;
        case 84:
          this.$ = yy.relationType.AGGREGATION;
          break;
        case 85:
          this.$ = yy.relationType.EXTENSION;
          break;
        case 86:
          this.$ = yy.relationType.COMPOSITION;
          break;
        case 87:
          this.$ = yy.relationType.DEPENDENCY;
          break;
        case 88:
          this.$ = yy.relationType.LOLLIPOP;
          break;
        case 89:
          this.$ = yy.lineType.LINE;
          break;
        case 90:
          this.$ = yy.lineType.DOTTED_LINE;
          break;
        case 91:
        case 97:
          this.$ = $$[$0 - 2];
          yy.setClickEvent($$[$0 - 1], $$[$0]);
          break;
        case 92:
        case 98:
          this.$ = $$[$0 - 3];
          yy.setClickEvent($$[$0 - 2], $$[$0 - 1]);
          yy.setTooltip($$[$0 - 2], $$[$0]);
          break;
        case 93:
          this.$ = $$[$0 - 2];
          yy.setLink($$[$0 - 1], $$[$0]);
          break;
        case 94:
          this.$ = $$[$0 - 3];
          yy.setLink($$[$0 - 2], $$[$0 - 1], $$[$0]);
          break;
        case 95:
          this.$ = $$[$0 - 3];
          yy.setLink($$[$0 - 2], $$[$0 - 1]);
          yy.setTooltip($$[$0 - 2], $$[$0]);
          break;
        case 96:
          this.$ = $$[$0 - 4];
          yy.setLink($$[$0 - 3], $$[$0 - 2], $$[$0]);
          yy.setTooltip($$[$0 - 3], $$[$0 - 1]);
          break;
        case 99:
          this.$ = $$[$0 - 3];
          yy.setClickEvent($$[$0 - 2], $$[$0 - 1], $$[$0]);
          break;
        case 100:
          this.$ = $$[$0 - 4];
          yy.setClickEvent($$[$0 - 3], $$[$0 - 2], $$[$0 - 1]);
          yy.setTooltip($$[$0 - 3], $$[$0]);
          break;
        case 101:
          this.$ = $$[$0 - 3];
          yy.setLink($$[$0 - 2], $$[$0]);
          break;
        case 102:
          this.$ = $$[$0 - 4];
          yy.setLink($$[$0 - 3], $$[$0 - 1], $$[$0]);
          break;
        case 103:
          this.$ = $$[$0 - 4];
          yy.setLink($$[$0 - 3], $$[$0 - 1]);
          yy.setTooltip($$[$0 - 3], $$[$0]);
          break;
        case 104:
          this.$ = $$[$0 - 5];
          yy.setLink($$[$0 - 4], $$[$0 - 2], $$[$0]);
          yy.setTooltip($$[$0 - 4], $$[$0 - 1]);
          break;
        case 105:
          this.$ = $$[$0 - 2];
          yy.setCssStyle($$[$0 - 1], $$[$0]);
          break;
        case 106:
          yy.setCssClass($$[$0 - 1], $$[$0]);
          break;
        case 107:
          this.$ = [$$[$0]];
          break;
        case 108:
          $$[$0 - 2].push($$[$0]);
          this.$ = $$[$0 - 2];
          break;
        case 110:
          this.$ = $$[$0 - 1] + $$[$0];
          break;
      }
    }, "anonymous"),
    table: [{ 3: 1, 4: 2, 5: 3, 6: 4, 7: [1, 6], 10: 5, 16: 39, 17: 40, 19: 21, 21: 7, 23: 8, 24: 9, 25: 10, 26: 11, 27: 12, 28: 13, 29: 14, 30: 15, 31: 16, 32: 17, 33: $V0, 35: $V1, 37: $V2, 38: 22, 42: $V3, 43: 23, 46: $V4, 48: $V5, 51: $V6, 52: $V7, 54: $V8, 56: $V9, 57: $Va, 60: $Vb, 62: $Vc, 63: $Vd, 64: $Ve, 65: $Vf, 75: $Vg, 76: $Vh, 78: $Vi, 82: $Vj, 83: $Vk, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, { 1: [3] }, { 1: [2, 1] }, { 1: [2, 2] }, { 1: [2, 3] }, o($Vp, [2, 5], { 8: [1, 48] }), { 8: [1, 49] }, o($Vq, [2, 19], { 22: [1, 50] }), o($Vq, [2, 21]), o($Vq, [2, 22]), o($Vq, [2, 23]), o($Vq, [2, 24]), o($Vq, [2, 25]), o($Vq, [2, 26]), o($Vq, [2, 27]), o($Vq, [2, 28]), o($Vq, [2, 29]), o($Vq, [2, 30]), { 34: [1, 51] }, { 36: [1, 52] }, o($Vq, [2, 33]), o($Vq, [2, 63], { 53: 53, 66: 56, 67: 57, 13: [1, 54], 22: [1, 55], 68: $Vr, 69: $Vs, 70: $Vt, 71: $Vu, 72: $Vv, 73: $Vw, 74: $Vx }), { 39: [1, 65] }, o($Vy, [2, 47], { 39: [1, 67], 44: [1, 66], 46: [1, 68] }), o($Vq, [2, 65]), o($Vq, [2, 66]), { 16: 69, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn }, { 16: 39, 17: 40, 19: 70, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, { 16: 39, 17: 40, 19: 71, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, { 16: 39, 17: 40, 19: 72, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, { 60: [1, 73] }, { 13: [1, 74] }, { 16: 39, 17: 40, 19: 75, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, { 13: $Vz, 55: 76 }, { 58: 78, 60: [1, 79] }, o($Vq, [2, 76]), o($Vq, [2, 77]), o($Vq, [2, 78]), o($Vq, [2, 79]), o($VA, [2, 13], { 16: 39, 17: 40, 19: 81, 18: [1, 80], 20: [1, 82], 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }), o($VA, [2, 15], { 20: [1, 83] }), { 15: 84, 16: 85, 17: 86, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, { 16: 39, 17: 40, 19: 87, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, o($VB, [2, 133]), o($VB, [2, 134]), o($VB, [2, 135]), o($VB, [2, 136]), o([1, 8, 9, 12, 13, 20, 22, 39, 41, 44, 46, 68, 69, 70, 71, 72, 73, 74, 79, 81], [2, 137]), o($Vp, [2, 6], { 10: 5, 21: 7, 23: 8, 24: 9, 25: 10, 26: 11, 27: 12, 28: 13, 29: 14, 30: 15, 31: 16, 32: 17, 19: 21, 38: 22, 43: 23, 16: 39, 17: 40, 5: 88, 33: $V0, 35: $V1, 37: $V2, 42: $V3, 46: $V4, 48: $V5, 51: $V6, 52: $V7, 54: $V8, 56: $V9, 57: $Va, 60: $Vb, 62: $Vc, 63: $Vd, 64: $Ve, 65: $Vf, 75: $Vg, 76: $Vh, 78: $Vi, 82: $Vj, 83: $Vk, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }), { 5: 89, 10: 5, 16: 39, 17: 40, 19: 21, 21: 7, 23: 8, 24: 9, 25: 10, 26: 11, 27: 12, 28: 13, 29: 14, 30: 15, 31: 16, 32: 17, 33: $V0, 35: $V1, 37: $V2, 38: 22, 42: $V3, 43: 23, 46: $V4, 48: $V5, 51: $V6, 52: $V7, 54: $V8, 56: $V9, 57: $Va, 60: $Vb, 62: $Vc, 63: $Vd, 64: $Ve, 65: $Vf, 75: $Vg, 76: $Vh, 78: $Vi, 82: $Vj, 83: $Vk, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, o($Vq, [2, 20]), o($Vq, [2, 31]), o($Vq, [2, 32]), { 13: [1, 91], 16: 39, 17: 40, 19: 90, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, { 53: 92, 66: 56, 67: 57, 68: $Vr, 69: $Vs, 70: $Vt, 71: $Vu, 72: $Vv, 73: $Vw, 74: $Vx }, o($Vq, [2, 64]), { 67: 93, 73: $Vw, 74: $Vx }, o($VC, [2, 83], { 66: 94, 68: $Vr, 69: $Vs, 70: $Vt, 71: $Vu, 72: $Vv }), o($VD, [2, 84]), o($VD, [2, 85]), o($VD, [2, 86]), o($VD, [2, 87]), o($VD, [2, 88]), o($VE, [2, 89]), o($VE, [2, 90]), { 8: [1, 96], 23: 99, 24: 97, 30: 98, 38: 22, 40: 95, 42: $V3, 43: 23, 48: $V5, 54: $V8, 56: $V9 }, { 16: 100, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn }, { 41: [1, 102], 45: 101, 51: $VF }, { 16: 104, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn }, { 47: [1, 105] }, { 13: [1, 106] }, { 13: [1, 107] }, { 79: [1, 108], 81: [1, 109] }, { 22: $VG, 50: $VH, 59: 110, 60: $VI, 82: $VJ, 84: 111, 85: 112, 86: $VK, 87: $VL, 88: $VM, 89: $VN, 90: $VO }, { 60: [1, 122] }, { 13: $Vz, 55: 123 }, o($Vy, [2, 72]), o($Vy, [2, 138]), { 22: $VG, 50: $VH, 59: 124, 60: $VI, 61: [1, 125], 82: $VJ, 84: 111, 85: 112, 86: $VK, 87: $VL, 88: $VM, 89: $VN, 90: $VO }, o($VP, [2, 74]), { 16: 39, 17: 40, 19: 126, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, o($VA, [2, 16]), o($VA, [2, 17]), o($VA, [2, 18]), { 11: 127, 12: $VQ, 39: [2, 36] }, o($VR, [2, 9], { 16: 85, 17: 86, 15: 130, 18: [1, 129], 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }), o($VR, [2, 10]), o($VS, [2, 55], { 11: 131, 12: $VQ }), o($Vp, [2, 7]), { 9: [1, 132] }, o($VT, [2, 67]), { 16: 39, 17: 40, 19: 133, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, { 13: [1, 135], 16: 39, 17: 40, 19: 134, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, o($VC, [2, 82], { 66: 136, 68: $Vr, 69: $Vs, 70: $Vt, 71: $Vu, 72: $Vv }), o($VC, [2, 81]), { 41: [1, 137] }, { 23: 99, 24: 97, 30: 98, 38: 22, 40: 138, 42: $V3, 43: 23, 48: $V5, 54: $V8, 56: $V9 }, { 8: [1, 139], 41: [2, 38] }, { 8: [1, 140], 41: [2, 41] }, { 8: [1, 141], 41: [2, 44] }, o($Vy, [2, 48], { 39: [1, 142] }), { 41: [1, 143] }, o($Vy, [2, 50]), { 41: [2, 61], 45: 144, 51: $VF }, { 47: [1, 145] }, { 16: 39, 17: 40, 19: 146, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, o($Vq, [2, 91], { 13: [1, 147] }), o($Vq, [2, 93], { 13: [1, 149], 77: [1, 148] }), o($Vq, [2, 97], { 13: [1, 150], 80: [1, 151] }), { 13: [1, 152] }, o($Vq, [2, 105], { 61: $VU }), o($VV, [2, 107], { 85: 154, 22: $VG, 50: $VH, 60: $VI, 82: $VJ, 86: $VK, 87: $VL, 88: $VM, 89: $VN, 90: $VO }), o($VW, [2, 109]), o($VW, [2, 111]), o($VW, [2, 112]), o($VW, [2, 113]), o($VW, [2, 114]), o($VW, [2, 115]), o($VW, [2, 116]), o($VW, [2, 117]), o($VW, [2, 118]), o($VW, [2, 119]), o($Vq, [2, 106]), o($Vy, [2, 71]), o($Vq, [2, 73], { 61: $VU }), { 60: [1, 155] }, o($VA, [2, 14]), { 39: [2, 37] }, { 13: [1, 156] }, { 15: 157, 16: 85, 17: 86, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, o($VR, [2, 12]), o($VS, [2, 56]), { 1: [2, 4] }, o($VT, [2, 69]), o($VT, [2, 68]), { 16: 39, 17: 40, 19: 158, 60: $Vb, 86: $Vl, 100: $Vm, 102: $Vn, 103: $Vo }, o($VC, [2, 80]), o($Vy, [2, 34]), { 41: [1, 159] }, { 23: 99, 24: 97, 30: 98, 38: 22, 40: 160, 41: [2, 39], 42: $V3, 43: 23, 48: $V5, 54: $V8, 56: $V9 }, { 23: 99, 24: 97, 30: 98, 38: 22, 40: 161, 41: [2, 42], 42: $V3, 43: 23, 48: $V5, 54: $V8, 56: $V9 }, { 23: 99, 24: 97, 30: 98, 38: 22, 40: 162, 41: [2, 45], 42: $V3, 43: 23, 48: $V5, 54: $V8, 56: $V9 }, { 45: 163, 51: $VF }, o($Vy, [2, 49]), { 41: [2, 62] }, o($Vy, [2, 52], { 39: [1, 164] }), o($Vq, [2, 60]), o($Vq, [2, 92]), o($Vq, [2, 94]), o($Vq, [2, 95], { 77: [1, 165] }), o($Vq, [2, 98]), o($Vq, [2, 99], { 13: [1, 166] }), o($Vq, [2, 101], { 13: [1, 168], 77: [1, 167] }), { 22: $VG, 50: $VH, 60: $VI, 82: $VJ, 84: 169, 85: 112, 86: $VK, 87: $VL, 88: $VM, 89: $VN, 90: $VO }, o($VW, [2, 110]), o($VP, [2, 75]), { 14: [1, 170] }, o($VR, [2, 11]), o($VT, [2, 70]), o($Vy, [2, 35]), { 41: [2, 40] }, { 41: [2, 43] }, { 41: [2, 46] }, { 41: [1, 171] }, { 41: [1, 173], 45: 172, 51: $VF }, o($Vq, [2, 96]), o($Vq, [2, 100]), o($Vq, [2, 102]), o($Vq, [2, 103], { 77: [1, 174] }), o($VV, [2, 108], { 85: 154, 22: $VG, 50: $VH, 60: $VI, 82: $VJ, 86: $VK, 87: $VL, 88: $VM, 89: $VN, 90: $VO }), o($VS, [2, 8]), o($Vy, [2, 51]), { 41: [1, 175] }, o($Vy, [2, 54]), o($Vq, [2, 104]), o($Vy, [2, 53])],
    defaultActions: { 2: [2, 1], 3: [2, 2], 4: [2, 3], 127: [2, 37], 132: [2, 4], 144: [2, 62], 160: [2, 40], 161: [2, 43], 162: [2, 46] },
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
      options: {},
      performAction: /* @__PURE__ */ __name(function anonymous(yy, yy_, $avoiding_name_collisions, YY_START) {
        var YYSTATE = YY_START;
        switch ($avoiding_name_collisions) {
          case 0:
            return 62;
            break;
          case 1:
            return 63;
            break;
          case 2:
            return 64;
            break;
          case 3:
            return 65;
            break;
          case 4:
            break;
          case 5:
            break;
          case 6:
            this.begin("acc_title");
            return 33;
            break;
          case 7:
            this.popState();
            return "acc_title_value";
            break;
          case 8:
            this.begin("acc_descr");
            return 35;
            break;
          case 9:
            this.popState();
            return "acc_descr_value";
            break;
          case 10:
            this.begin("acc_descr_multiline");
            break;
          case 11:
            this.popState();
            break;
          case 12:
            return "acc_descr_multiline_value";
            break;
          case 13:
            return 8;
            break;
          case 14:
            break;
          case 15:
            return 7;
            break;
          case 16:
            return 7;
            break;
          case 17:
            return "EDGE_STATE";
            break;
          case 18:
            this.begin("callback_name");
            break;
          case 19:
            this.popState();
            break;
          case 20:
            this.popState();
            this.begin("callback_args");
            break;
          case 21:
            return 79;
            break;
          case 22:
            this.popState();
            break;
          case 23:
            return 80;
            break;
          case 24:
            this.popState();
            break;
          case 25:
            return "STR";
            break;
          case 26:
            this.begin("string");
            break;
          case 27:
            return 82;
            break;
          case 28:
            return 57;
            break;
          case 29:
            this.begin("namespace");
            return 42;
            break;
          case 30:
            this.popState();
            return 8;
            break;
          case 31:
            break;
          case 32:
            this.begin("namespace-body");
            return 39;
            break;
          case 33:
            this.popState();
            this.less(0);
            break;
          case 34:
            this.popState();
            return 41;
            break;
          case 35:
            return "EOF_IN_STRUCT";
            break;
          case 36:
            return 8;
            break;
          case 37:
            break;
          case 38:
            return "EDGE_STATE";
            break;
          case 39:
            this.begin("class");
            return 48;
            break;
          case 40:
            this.popState();
            return 8;
            break;
          case 41:
            break;
          case 42:
            this.popState();
            this.popState();
            return 41;
            break;
          case 43:
            this.begin("class-body");
            return 39;
            break;
          case 44:
            this.popState();
            return 41;
            break;
          case 45:
            return "EOF_IN_STRUCT";
            break;
          case 46:
            return "EDGE_STATE";
            break;
          case 47:
            return "OPEN_IN_STRUCT";
            break;
          case 48:
            break;
          case 49:
            return "MEMBER";
            break;
          case 50:
            return 83;
            break;
          case 51:
            return 75;
            break;
          case 52:
            return 76;
            break;
          case 53:
            return 78;
            break;
          case 54:
            return 54;
            break;
          case 55:
            return 56;
            break;
          case 56:
            return 46;
            break;
          case 57:
            return 47;
            break;
          case 58:
            return 81;
            break;
          case 59:
            this.popState();
            break;
          case 60:
            return "GENERICTYPE";
            break;
          case 61:
            this.begin("generic");
            break;
          case 62:
            this.popState();
            break;
          case 63:
            return "BQUOTE_STR";
            break;
          case 64:
            this.begin("bqstring");
            break;
          case 65:
            return 77;
            break;
          case 66:
            return 77;
            break;
          case 67:
            return 77;
            break;
          case 68:
            return 77;
            break;
          case 69:
            return 69;
            break;
          case 70:
            return 69;
            break;
          case 71:
            return 71;
            break;
          case 72:
            return 71;
            break;
          case 73:
            return 70;
            break;
          case 74:
            return 68;
            break;
          case 75:
            return 72;
            break;
          case 76:
            return 73;
            break;
          case 77:
            return 74;
            break;
          case 78:
            return 22;
            break;
          case 79:
            return 44;
            break;
          case 80:
            return 100;
            break;
          case 81:
            return 18;
            break;
          case 82:
            return "PLUS";
            break;
          case 83:
            return 87;
            break;
          case 84:
            return 61;
            break;
          case 85:
            return 89;
            break;
          case 86:
            return 89;
            break;
          case 87:
            return 90;
            break;
          case 88:
            return "EQUALS";
            break;
          case 89:
            return "EQUALS";
            break;
          case 90:
            return 60;
            break;
          case 91:
            return 12;
            break;
          case 92:
            return 14;
            break;
          case 93:
            return "PUNCTUATION";
            break;
          case 94:
            return 86;
            break;
          case 95:
            return 102;
            break;
          case 96:
            return 50;
            break;
          case 97:
            return 50;
            break;
          case 98:
            return 9;
            break;
        }
      }, "anonymous"),
      rules: [/^(?:.*direction\s+TB[^\n]*)/, /^(?:.*direction\s+BT[^\n]*)/, /^(?:.*direction\s+RL[^\n]*)/, /^(?:.*direction\s+LR[^\n]*)/, /^(?:%%(?!\{)*[^\n]*(\r?\n?)+)/, /^(?:%%[^\n]*(\r?\n)*)/, /^(?:accTitle\s*:\s*)/, /^(?:(?!\n||)*[^\n]*)/, /^(?:accDescr\s*:\s*)/, /^(?:(?!\n||)*[^\n]*)/, /^(?:accDescr\s*\{\s*)/, /^(?:[\}])/, /^(?:[^\}]*)/, /^(?:\s*(\r?\n)+)/, /^(?:\s+)/, /^(?:classDiagram-v2\b)/, /^(?:classDiagram\b)/, /^(?:\[\*\])/, /^(?:call[\s]+)/, /^(?:\([\s]*\))/, /^(?:\()/, /^(?:[^(]*)/, /^(?:\))/, /^(?:[^)]*)/, /^(?:["])/, /^(?:[^"]*)/, /^(?:["])/, /^(?:style\b)/, /^(?:classDef\b)/, /^(?:namespace\b)/, /^(?:\s*(\r?\n)+)/, /^(?:\s+)/, /^(?:[{])/, /^(?:[}])/, /^(?:[}])/, /^(?:$)/, /^(?:\s*(\r?\n)+)/, /^(?:\s+)/, /^(?:\[\*\])/, /^(?:class\b)/, /^(?:\s*(\r?\n)+)/, /^(?:\s+)/, /^(?:[}])/, /^(?:[{])/, /^(?:[}])/, /^(?:$)/, /^(?:\[\*\])/, /^(?:[{])/, /^(?:[\n])/, /^(?:[^{}\n]*)/, /^(?:cssClass\b)/, /^(?:callback\b)/, /^(?:link\b)/, /^(?:click\b)/, /^(?:note for\b)/, /^(?:note\b)/, /^(?:<<)/, /^(?:>>)/, /^(?:href\b)/, /^(?:[~])/, /^(?:[^~]*)/, /^(?:~)/, /^(?:[`])/, /^(?:[^`]+)/, /^(?:[`])/, /^(?:_self\b)/, /^(?:_blank\b)/, /^(?:_parent\b)/, /^(?:_top\b)/, /^(?:\s*<\|)/, /^(?:\s*\|>)/, /^(?:\s*>)/, /^(?:\s*<)/, /^(?:\s*\*)/, /^(?:\s*o\b)/, /^(?:\s*\(\))/, /^(?:--)/, /^(?:\.\.)/, /^(?::{1}[^:\n;]+)/, /^(?::{3})/, /^(?:-)/, /^(?:\.)/, /^(?:\+)/, /^(?::)/, /^(?:,)/, /^(?:#)/, /^(?:#)/, /^(?:%)/, /^(?:=)/, /^(?:=)/, /^(?:\w+)/, /^(?:\[)/, /^(?:\])/, /^(?:[!"#$%&'*+,-.`?\\/])/, /^(?:[0-9]+)/, /^(?:[\u00AA\u00B5\u00BA\u00C0-\u00D6\u00D8-\u00F6]|[\u00F8-\u02C1\u02C6-\u02D1\u02E0-\u02E4\u02EC\u02EE\u0370-\u0374\u0376\u0377]|[\u037A-\u037D\u0386\u0388-\u038A\u038C\u038E-\u03A1\u03A3-\u03F5]|[\u03F7-\u0481\u048A-\u0527\u0531-\u0556\u0559\u0561-\u0587\u05D0-\u05EA]|[\u05F0-\u05F2\u0620-\u064A\u066E\u066F\u0671-\u06D3\u06D5\u06E5\u06E6\u06EE]|[\u06EF\u06FA-\u06FC\u06FF\u0710\u0712-\u072F\u074D-\u07A5\u07B1\u07CA-\u07EA]|[\u07F4\u07F5\u07FA\u0800-\u0815\u081A\u0824\u0828\u0840-\u0858\u08A0]|[\u08A2-\u08AC\u0904-\u0939\u093D\u0950\u0958-\u0961\u0971-\u0977]|[\u0979-\u097F\u0985-\u098C\u098F\u0990\u0993-\u09A8\u09AA-\u09B0\u09B2]|[\u09B6-\u09B9\u09BD\u09CE\u09DC\u09DD\u09DF-\u09E1\u09F0\u09F1\u0A05-\u0A0A]|[\u0A0F\u0A10\u0A13-\u0A28\u0A2A-\u0A30\u0A32\u0A33\u0A35\u0A36\u0A38\u0A39]|[\u0A59-\u0A5C\u0A5E\u0A72-\u0A74\u0A85-\u0A8D\u0A8F-\u0A91\u0A93-\u0AA8]|[\u0AAA-\u0AB0\u0AB2\u0AB3\u0AB5-\u0AB9\u0ABD\u0AD0\u0AE0\u0AE1\u0B05-\u0B0C]|[\u0B0F\u0B10\u0B13-\u0B28\u0B2A-\u0B30\u0B32\u0B33\u0B35-\u0B39\u0B3D\u0B5C]|[\u0B5D\u0B5F-\u0B61\u0B71\u0B83\u0B85-\u0B8A\u0B8E-\u0B90\u0B92-\u0B95\u0B99]|[\u0B9A\u0B9C\u0B9E\u0B9F\u0BA3\u0BA4\u0BA8-\u0BAA\u0BAE-\u0BB9\u0BD0]|[\u0C05-\u0C0C\u0C0E-\u0C10\u0C12-\u0C28\u0C2A-\u0C33\u0C35-\u0C39\u0C3D]|[\u0C58\u0C59\u0C60\u0C61\u0C85-\u0C8C\u0C8E-\u0C90\u0C92-\u0CA8\u0CAA-\u0CB3]|[\u0CB5-\u0CB9\u0CBD\u0CDE\u0CE0\u0CE1\u0CF1\u0CF2\u0D05-\u0D0C\u0D0E-\u0D10]|[\u0D12-\u0D3A\u0D3D\u0D4E\u0D60\u0D61\u0D7A-\u0D7F\u0D85-\u0D96\u0D9A-\u0DB1]|[\u0DB3-\u0DBB\u0DBD\u0DC0-\u0DC6\u0E01-\u0E30\u0E32\u0E33\u0E40-\u0E46\u0E81]|[\u0E82\u0E84\u0E87\u0E88\u0E8A\u0E8D\u0E94-\u0E97\u0E99-\u0E9F\u0EA1-\u0EA3]|[\u0EA5\u0EA7\u0EAA\u0EAB\u0EAD-\u0EB0\u0EB2\u0EB3\u0EBD\u0EC0-\u0EC4\u0EC6]|[\u0EDC-\u0EDF\u0F00\u0F40-\u0F47\u0F49-\u0F6C\u0F88-\u0F8C\u1000-\u102A]|[\u103F\u1050-\u1055\u105A-\u105D\u1061\u1065\u1066\u106E-\u1070\u1075-\u1081]|[\u108E\u10A0-\u10C5\u10C7\u10CD\u10D0-\u10FA\u10FC-\u1248\u124A-\u124D]|[\u1250-\u1256\u1258\u125A-\u125D\u1260-\u1288\u128A-\u128D\u1290-\u12B0]|[\u12B2-\u12B5\u12B8-\u12BE\u12C0\u12C2-\u12C5\u12C8-\u12D6\u12D8-\u1310]|[\u1312-\u1315\u1318-\u135A\u1380-\u138F\u13A0-\u13F4\u1401-\u166C]|[\u166F-\u167F\u1681-\u169A\u16A0-\u16EA\u1700-\u170C\u170E-\u1711]|[\u1720-\u1731\u1740-\u1751\u1760-\u176C\u176E-\u1770\u1780-\u17B3\u17D7]|[\u17DC\u1820-\u1877\u1880-\u18A8\u18AA\u18B0-\u18F5\u1900-\u191C]|[\u1950-\u196D\u1970-\u1974\u1980-\u19AB\u19C1-\u19C7\u1A00-\u1A16]|[\u1A20-\u1A54\u1AA7\u1B05-\u1B33\u1B45-\u1B4B\u1B83-\u1BA0\u1BAE\u1BAF]|[\u1BBA-\u1BE5\u1C00-\u1C23\u1C4D-\u1C4F\u1C5A-\u1C7D\u1CE9-\u1CEC]|[\u1CEE-\u1CF1\u1CF5\u1CF6\u1D00-\u1DBF\u1E00-\u1F15\u1F18-\u1F1D]|[\u1F20-\u1F45\u1F48-\u1F4D\u1F50-\u1F57\u1F59\u1F5B\u1F5D\u1F5F-\u1F7D]|[\u1F80-\u1FB4\u1FB6-\u1FBC\u1FBE\u1FC2-\u1FC4\u1FC6-\u1FCC\u1FD0-\u1FD3]|[\u1FD6-\u1FDB\u1FE0-\u1FEC\u1FF2-\u1FF4\u1FF6-\u1FFC\u2071\u207F]|[\u2090-\u209C\u2102\u2107\u210A-\u2113\u2115\u2119-\u211D\u2124\u2126\u2128]|[\u212A-\u212D\u212F-\u2139\u213C-\u213F\u2145-\u2149\u214E\u2183\u2184]|[\u2C00-\u2C2E\u2C30-\u2C5E\u2C60-\u2CE4\u2CEB-\u2CEE\u2CF2\u2CF3]|[\u2D00-\u2D25\u2D27\u2D2D\u2D30-\u2D67\u2D6F\u2D80-\u2D96\u2DA0-\u2DA6]|[\u2DA8-\u2DAE\u2DB0-\u2DB6\u2DB8-\u2DBE\u2DC0-\u2DC6\u2DC8-\u2DCE]|[\u2DD0-\u2DD6\u2DD8-\u2DDE\u2E2F\u3005\u3006\u3031-\u3035\u303B\u303C]|[\u3041-\u3096\u309D-\u309F\u30A1-\u30FA\u30FC-\u30FF\u3105-\u312D]|[\u3131-\u318E\u31A0-\u31BA\u31F0-\u31FF\u3400-\u4DB5\u4E00-\u9FCC]|[\uA000-\uA48C\uA4D0-\uA4FD\uA500-\uA60C\uA610-\uA61F\uA62A\uA62B]|[\uA640-\uA66E\uA67F-\uA697\uA6A0-\uA6E5\uA717-\uA71F\uA722-\uA788]|[\uA78B-\uA78E\uA790-\uA793\uA7A0-\uA7AA\uA7F8-\uA801\uA803-\uA805]|[\uA807-\uA80A\uA80C-\uA822\uA840-\uA873\uA882-\uA8B3\uA8F2-\uA8F7\uA8FB]|[\uA90A-\uA925\uA930-\uA946\uA960-\uA97C\uA984-\uA9B2\uA9CF\uAA00-\uAA28]|[\uAA40-\uAA42\uAA44-\uAA4B\uAA60-\uAA76\uAA7A\uAA80-\uAAAF\uAAB1\uAAB5]|[\uAAB6\uAAB9-\uAABD\uAAC0\uAAC2\uAADB-\uAADD\uAAE0-\uAAEA\uAAF2-\uAAF4]|[\uAB01-\uAB06\uAB09-\uAB0E\uAB11-\uAB16\uAB20-\uAB26\uAB28-\uAB2E]|[\uABC0-\uABE2\uAC00-\uD7A3\uD7B0-\uD7C6\uD7CB-\uD7FB\uF900-\uFA6D]|[\uFA70-\uFAD9\uFB00-\uFB06\uFB13-\uFB17\uFB1D\uFB1F-\uFB28\uFB2A-\uFB36]|[\uFB38-\uFB3C\uFB3E\uFB40\uFB41\uFB43\uFB44\uFB46-\uFBB1\uFBD3-\uFD3D]|[\uFD50-\uFD8F\uFD92-\uFDC7\uFDF0-\uFDFB\uFE70-\uFE74\uFE76-\uFEFC]|[\uFF21-\uFF3A\uFF41-\uFF5A\uFF66-\uFFBE\uFFC2-\uFFC7\uFFCA-\uFFCF]|[\uFFD2-\uFFD7\uFFDA-\uFFDC])/, /^(?:\s)/, /^(?:\s)/, /^(?:$)/],
      conditions: { "namespace-body": { "rules": [26, 29, 34, 35, 36, 37, 38, 39, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "namespace": { "rules": [26, 29, 30, 31, 32, 33, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "class-body": { "rules": [26, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "class": { "rules": [26, 40, 41, 42, 43, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "acc_descr_multiline": { "rules": [11, 12, 26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "acc_descr": { "rules": [9, 26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "acc_title": { "rules": [7, 26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "callback_args": { "rules": [22, 23, 26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "callback_name": { "rules": [19, 20, 21, 26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "href": { "rules": [26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "struct": { "rules": [26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "generic": { "rules": [26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "bqstring": { "rules": [26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "string": { "rules": [24, 25, 26, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 98], "inclusive": false }, "INITIAL": { "rules": [0, 1, 2, 3, 4, 5, 6, 8, 10, 13, 14, 15, 16, 17, 18, 26, 27, 28, 29, 39, 50, 51, 52, 53, 54, 55, 56, 57, 58, 61, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 97, 98], "inclusive": true } }
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
var classDiagram_default = parser;

// src/diagrams/class/classTypes.ts
var visibilityValues = ["#", "+", "~", "-", ""];
var ClassMember = class {
  static {
    __name(this, "ClassMember");
  }
  constructor(input, memberType) {
    this.memberType = memberType;
    this.visibility = "";
    this.classifier = "";
    this.text = "";
    const sanitizedInput = sanitizeText(input, getConfig());
    this.parseMember(sanitizedInput);
  }
  getDisplayDetails() {
    let displayText = this.visibility + parseGenericTypes(this.id);
    if (this.memberType === "method") {
      displayText += `(${parseGenericTypes(this.parameters.trim())})`;
      if (this.returnType) {
        displayText += " : " + parseGenericTypes(this.returnType);
      }
    }
    displayText = displayText.trim();
    const cssStyle = this.parseClassifier();
    return {
      displayText,
      cssStyle
    };
  }
  parseMember(input) {
    let potentialClassifier = "";
    if (this.memberType === "method") {
      const methodRegEx = /([#+~-])?(.+)\((.*)\)([\s$*])?(.*)([$*])?/;
      const match = methodRegEx.exec(input);
      if (match) {
        const detectedVisibility = match[1] ? match[1].trim() : "";
        if (visibilityValues.includes(detectedVisibility)) {
          this.visibility = detectedVisibility;
        }
        this.id = match[2];
        this.parameters = match[3] ? match[3].trim() : "";
        potentialClassifier = match[4] ? match[4].trim() : "";
        this.returnType = match[5] ? match[5].trim() : "";
        if (potentialClassifier === "") {
          const lastChar = this.returnType.substring(this.returnType.length - 1);
          if (/[$*]/.exec(lastChar)) {
            potentialClassifier = lastChar;
            this.returnType = this.returnType.substring(0, this.returnType.length - 1);
          }
        }
      }
    } else {
      const length = input.length;
      const firstChar = input.substring(0, 1);
      const lastChar = input.substring(length - 1);
      if (visibilityValues.includes(firstChar)) {
        this.visibility = firstChar;
      }
      if (/[$*]/.exec(lastChar)) {
        potentialClassifier = lastChar;
      }
      this.id = input.substring(
        this.visibility === "" ? 0 : 1,
        potentialClassifier === "" ? length : length - 1
      );
    }
    this.classifier = potentialClassifier;
    this.id = this.id.startsWith(" ") ? " " + this.id.trim() : this.id.trim();
    const combinedText = `${this.visibility ? "\\" + this.visibility : ""}${parseGenericTypes(this.id)}${this.memberType === "method" ? `(${parseGenericTypes(this.parameters)})${this.returnType ? " : " + parseGenericTypes(this.returnType) : ""}` : ""}`;
    this.text = combinedText.replaceAll("<", "&lt;").replaceAll(">", "&gt;");
    if (this.text.startsWith("\\&lt;")) {
      this.text = this.text.replace("\\&lt;", "~");
    }
  }
  parseClassifier() {
    switch (this.classifier) {
      case "*":
        return "font-style:italic;";
      case "$":
        return "text-decoration:underline;";
      default:
        return "";
    }
  }
};

// src/diagrams/class/classDb.ts
var MERMAID_DOM_ID_PREFIX = "classId-";
var classCounter = 0;
var sanitizeText2 = /* @__PURE__ */ __name((txt) => common_default.sanitizeText(txt, getConfig()), "sanitizeText");
var ClassDB = class _ClassDB {
  constructor() {
    this.relations = [];
    this.classes = /* @__PURE__ */ new Map();
    this.styleClasses = /* @__PURE__ */ new Map();
    this.notes = /* @__PURE__ */ new Map();
    this.interfaces = [];
    // private static classCounter = 0;
    this.namespaces = /* @__PURE__ */ new Map();
    this.namespaceCounter = 0;
    this.namespaceStack = [];
    this.diagramId = "";
    // eslint-disable-next-line @typescript-eslint/no-unsafe-function-type
    this.functions = [];
    this.lineType = {
      LINE: 0,
      DOTTED_LINE: 1
    };
    this.relationType = {
      AGGREGATION: 0,
      EXTENSION: 1,
      COMPOSITION: 2,
      DEPENDENCY: 3,
      LOLLIPOP: 4
    };
    this.setupToolTips = /* @__PURE__ */ __name((element) => {
      const tooltipElem = createTooltip();
      const svg = select_default(element).select("svg");
      const nodes = svg.selectAll("g").filter(function() {
        return select_default(this).attr("title") !== null;
      });
      nodes.on("mouseover", (event) => {
        const el = select_default(event.currentTarget);
        const title = el.attr("title");
        if (!title) {
          return;
        }
        const rect = event.currentTarget.getBoundingClientRect();
        tooltipElem.transition().duration(200).style("opacity", ".9");
        tooltipElem.html(purify.sanitize(title)).style("left", `${window.scrollX + rect.left + rect.width / 2}px`).style("top", `${window.scrollY + rect.bottom + 4}px`);
        el.classed("hover", true);
      }).on("mouseout", (event) => {
        tooltipElem.transition().duration(500).style("opacity", 0);
        select_default(event.currentTarget).classed("hover", false);
      });
    }, "setupToolTips");
    this.direction = "TB";
    this.setAccTitle = setAccTitle;
    this.getAccTitle = getAccTitle;
    this.setAccDescription = setAccDescription;
    this.getAccDescription = getAccDescription;
    this.setDiagramTitle = setDiagramTitle;
    this.getDiagramTitle = getDiagramTitle;
    this.getConfig = /* @__PURE__ */ __name(() => getConfig().class, "getConfig");
    this.functions.push(this.setupToolTips.bind(this));
    this.clear();
    this.addRelation = this.addRelation.bind(this);
    this.addClassesToNamespace = this.addClassesToNamespace.bind(this);
    this.addNamespace = this.addNamespace.bind(this);
    this.popNamespace = this.popNamespace.bind(this);
    this.setCssClass = this.setCssClass.bind(this);
    this.addMembers = this.addMembers.bind(this);
    this.addClass = this.addClass.bind(this);
    this.setClassLabel = this.setClassLabel.bind(this);
    this.addAnnotation = this.addAnnotation.bind(this);
    this.addMember = this.addMember.bind(this);
    this.cleanupLabel = this.cleanupLabel.bind(this);
    this.addNote = this.addNote.bind(this);
    this.defineClass = this.defineClass.bind(this);
    this.setDirection = this.setDirection.bind(this);
    this.setLink = this.setLink.bind(this);
    this.bindFunctions = this.bindFunctions.bind(this);
    this.clear = this.clear.bind(this);
    this.setTooltip = this.setTooltip.bind(this);
    this.setClickEvent = this.setClickEvent.bind(this);
    this.setCssStyle = this.setCssStyle.bind(this);
  }
  static {
    __name(this, "ClassDB");
  }
  splitClassNameAndType(_id) {
    const id = common_default.sanitizeText(_id, getConfig());
    let genericType = "";
    let className = id;
    if (id.indexOf("~") > 0) {
      const split = id.split("~");
      className = sanitizeText2(split[0]);
      genericType = sanitizeText2(split[1]);
    }
    return { className, type: genericType };
  }
  setClassLabel(_id, label) {
    const id = common_default.sanitizeText(_id, getConfig());
    if (label) {
      label = sanitizeText2(label);
    }
    const { className } = this.splitClassNameAndType(id);
    this.classes.get(className).label = label;
    this.classes.get(className).text = `${label}${this.classes.get(className).type ? `<${this.classes.get(className).type}>` : ""}`;
  }
  /**
   * Function called by parser when a node definition has been found.
   *
   * @param id - ID of the class to add
   * @public
   */
  addClass(_id) {
    const id = common_default.sanitizeText(_id, getConfig());
    const { className, type } = this.splitClassNameAndType(id);
    if (this.classes.has(className)) {
      return;
    }
    const name = common_default.sanitizeText(className, getConfig());
    this.classes.set(name, {
      id: name,
      type,
      label: name,
      text: `${name}${type ? `&lt;${type}&gt;` : ""}`,
      shape: "classBox",
      cssClasses: "default",
      methods: [],
      members: [],
      annotations: [],
      styles: [],
      domId: MERMAID_DOM_ID_PREFIX + name + "-" + classCounter
    });
    classCounter++;
  }
  addInterface(label, classId) {
    const classInterface = {
      id: `interface${this.interfaces.length}`,
      label,
      classId
    };
    this.interfaces.push(classInterface);
  }
  /**
   * Sets the diagram's SVG element ID, used to prefix domIds for uniqueness
   * across multiple diagrams on the same page.
   */
  setDiagramId(svgElementId) {
    this.diagramId = svgElementId;
  }
  /**
   * Function to lookup domId from id in the graph definition.
   * When diagramId is set, returns the prefixed version for DOM uniqueness.
   *
   * @param id - class ID to lookup
   * @public
   */
  lookUpDomId(_id) {
    const id = common_default.sanitizeText(_id, getConfig());
    if (this.classes.has(id)) {
      const domId = this.classes.get(id).domId;
      return this.diagramId ? `${this.diagramId}-${domId}` : domId;
    }
    throw new Error("Class not found: " + id);
  }
  clear() {
    this.relations = [];
    this.classes = /* @__PURE__ */ new Map();
    this.notes = /* @__PURE__ */ new Map();
    this.interfaces = [];
    this.functions = [];
    this.functions.push(this.setupToolTips.bind(this));
    this.namespaces = /* @__PURE__ */ new Map();
    this.namespaceCounter = 0;
    this.namespaceStack = [];
    this.diagramId = "";
    this.direction = "TB";
    clear();
  }
  getClass(id) {
    return this.classes.get(id);
  }
  getClasses() {
    return this.classes;
  }
  getRelations() {
    return this.relations;
  }
  getNote(id) {
    const key = typeof id === "number" ? `note${id}` : id;
    return this.notes.get(key);
  }
  getNotes() {
    return this.notes;
  }
  addRelation(classRelation) {
    log.debug("Adding relation: " + JSON.stringify(classRelation));
    const invalidTypes = [
      this.relationType.LOLLIPOP,
      this.relationType.AGGREGATION,
      this.relationType.COMPOSITION,
      this.relationType.DEPENDENCY,
      this.relationType.EXTENSION
    ];
    if (classRelation.relation.type1 === this.relationType.LOLLIPOP && !invalidTypes.includes(classRelation.relation.type2)) {
      this.addClass(classRelation.id2);
      this.addInterface(classRelation.id1, classRelation.id2);
      classRelation.id1 = `interface${this.interfaces.length - 1}`;
    } else if (classRelation.relation.type2 === this.relationType.LOLLIPOP && !invalidTypes.includes(classRelation.relation.type1)) {
      this.addClass(classRelation.id1);
      this.addInterface(classRelation.id2, classRelation.id1);
      classRelation.id2 = `interface${this.interfaces.length - 1}`;
    } else {
      this.addClass(classRelation.id1);
      this.addClass(classRelation.id2);
    }
    classRelation.id1 = this.splitClassNameAndType(classRelation.id1).className;
    classRelation.id2 = this.splitClassNameAndType(classRelation.id2).className;
    classRelation.relationTitle1 = common_default.sanitizeText(
      classRelation.relationTitle1.trim(),
      getConfig()
    );
    classRelation.relationTitle2 = common_default.sanitizeText(
      classRelation.relationTitle2.trim(),
      getConfig()
    );
    this.relations.push(classRelation);
  }
  /**
   * Adds an annotation to the specified class Annotations mark special properties of the given type
   * (like 'interface' or 'service')
   *
   * @param className - The class name
   * @param annotation - The name of the annotation without any brackets
   * @public
   */
  addAnnotation(className, annotation) {
    const validatedClassName = this.splitClassNameAndType(className).className;
    this.classes.get(validatedClassName).annotations.push(annotation);
  }
  /**
   * Adds a member to the specified class
   *
   * @param className - The class name
   * @param member - The full name of the member. If the member is enclosed in `<<brackets>>` it is
   *   treated as an annotation If the member is ending with a closing bracket ) it is treated as a
   *   method Otherwise the member will be treated as a normal property
   * @public
   */
  addMember(className, member) {
    this.addClass(className);
    const validatedClassName = this.splitClassNameAndType(className).className;
    const theClass = this.classes.get(validatedClassName);
    if (typeof member === "string") {
      const memberString = member.trim();
      if (memberString.startsWith("<<") && memberString.endsWith(">>")) {
        theClass.annotations.push(sanitizeText2(memberString.substring(2, memberString.length - 2)));
      } else if (memberString.indexOf(")") > 0) {
        theClass.methods.push(new ClassMember(memberString, "method"));
      } else if (memberString) {
        theClass.members.push(new ClassMember(memberString, "attribute"));
      }
    }
  }
  addMembers(className, members) {
    if (Array.isArray(members)) {
      members.reverse();
      members.forEach((member) => this.addMember(className, member));
    }
  }
  addNote(text, className) {
    const index = this.notes.size;
    const note = {
      id: `note${index}`,
      class: className,
      text,
      index
    };
    this.notes.set(note.id, note);
    return note.id;
  }
  cleanupLabel(label) {
    if (label.startsWith(":")) {
      label = label.substring(1);
    }
    return sanitizeText2(label.trim());
  }
  /**
   * Called by parser when assigning cssClass to a class
   *
   * @param ids - Comma separated list of ids
   * @param className - Class to add
   */
  setCssClass(ids, className) {
    ids.split(",").forEach((_id) => {
      let id = _id;
      if (/\d/.exec(_id[0])) {
        id = MERMAID_DOM_ID_PREFIX + id;
      }
      id = this.splitClassNameAndType(id).className;
      const classNode = this.classes.get(id);
      if (classNode) {
        classNode.cssClasses += " " + className;
      }
    });
  }
  defineClass(ids, style) {
    for (const id of ids) {
      let styleClass = this.styleClasses.get(id);
      if (styleClass === void 0) {
        styleClass = { id, styles: [], textStyles: [] };
        this.styleClasses.set(id, styleClass);
      }
      if (style) {
        style.forEach((s) => {
          if (/color/.exec(s)) {
            const newStyle = s.replace("fill", "bgFill");
            styleClass.textStyles.push(newStyle);
          }
          styleClass.styles.push(s);
        });
      }
      this.classes.forEach((value) => {
        if (value.cssClasses.includes(id)) {
          value.styles.push(...style.flatMap((s) => s.split(",")));
        }
      });
    }
  }
  /**
   * Called by parser when a tooltip is found, e.g. a clickable element.
   *
   * @param ids - Comma separated list of ids
   * @param tooltip - Tooltip to add
   */
  setTooltip(ids, tooltip) {
    ids.split(",").forEach((id) => {
      if (tooltip !== void 0) {
        const className = this.splitClassNameAndType(id).className;
        const classNode = this.classes.get(className);
        if (classNode) {
          classNode.tooltip = sanitizeText2(tooltip);
        }
      }
    });
  }
  getTooltip(id, namespace) {
    if (namespace && this.namespaces.has(namespace)) {
      return this.namespaces.get(namespace).classes.get(id).tooltip;
    }
    return this.classes.get(id).tooltip;
  }
  /**
   * Called by parser when a link is found. Adds the URL to the vertex data.
   *
   * @param ids - Comma separated list of ids
   * @param linkStr - URL to create a link for
   * @param target - Target of the link, _blank by default as originally defined in the svgDraw.js file
   */
  setLink(ids, linkStr, target) {
    const config = getConfig();
    ids.split(",").forEach((_id) => {
      let id = _id;
      if (/\d/.exec(_id[0])) {
        id = MERMAID_DOM_ID_PREFIX + id;
      }
      id = this.splitClassNameAndType(id).className;
      const theClass = this.classes.get(id);
      if (theClass) {
        theClass.link = utils_default.formatUrl(linkStr, config);
        if (config.securityLevel === "sandbox") {
          theClass.linkTarget = "_top";
        } else if (typeof target === "string") {
          theClass.linkTarget = sanitizeText2(target);
        } else {
          theClass.linkTarget = "_blank";
        }
      }
    });
    this.setCssClass(ids, "clickable");
  }
  /**
   * Called by parser when a click definition is found. Registers an event handler.
   *
   * @param ids - Comma separated list of ids
   * @param functionName - Function to be called on click
   * @param functionArgs - Function args the function should be called with
   */
  setClickEvent(ids, functionName, functionArgs) {
    ids.split(",").forEach((id) => {
      this.setClickFunc(id, functionName, functionArgs);
      const className = this.splitClassNameAndType(id).className;
      const classNode = this.classes.get(className);
      if (classNode) {
        classNode.haveCallback = true;
      }
    });
    this.setCssClass(ids, "clickable");
  }
  setClickFunc(_domId, functionName, functionArgs) {
    const domId = common_default.sanitizeText(_domId, getConfig());
    const config = getConfig();
    if (config.securityLevel !== "loose") {
      return;
    }
    if (functionName === void 0) {
      return;
    }
    const id = this.splitClassNameAndType(domId).className;
    if (this.classes.has(id)) {
      let argList = [];
      if (typeof functionArgs === "string") {
        argList = functionArgs.split(/,(?=(?:(?:[^"]*"){2})*[^"]*$)/);
        for (let i = 0; i < argList.length; i++) {
          let item = argList[i].trim();
          if (item.startsWith('"') && item.endsWith('"')) {
            item = item.substr(1, item.length - 2);
          }
          argList[i] = item;
        }
      }
      if (argList.length === 0) {
        argList.push(id);
      }
      this.functions.push(() => {
        const elemId = this.lookUpDomId(id);
        const elem = document.querySelector(`[id="${elemId}"]`);
        if (elem !== null) {
          elem.addEventListener(
            "click",
            () => {
              utils_default.runFunc(functionName, ...argList);
            },
            false
          );
        }
      });
    }
  }
  bindFunctions(element) {
    this.functions.forEach((fun) => {
      fun(element);
    });
  }
  // Utility function to escape HTML meta-characters
  escapeHtml(str) {
    return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }
  getDirection() {
    return this.direction;
  }
  setDirection(dir) {
    this.direction = dir;
  }
  static resolveQualifiedId(id, stack) {
    const prefix = stack.at(-1);
    return prefix ? `${prefix}.${id}` : id;
  }
  static getAncestorIds(qualifiedId) {
    const parts = qualifiedId.split(".");
    const ids = new Array(parts.length);
    ids[0] = parts[0];
    for (let i = 1; i < parts.length; i++) {
      ids[i] = `${ids[i - 1]}.${parts[i]}`;
    }
    return ids;
  }
  createNamespaceNode(id, label, parentId, explicit = false) {
    return {
      id,
      label,
      classes: /* @__PURE__ */ new Map(),
      notes: /* @__PURE__ */ new Map(),
      children: /* @__PURE__ */ new Map(),
      domId: MERMAID_DOM_ID_PREFIX + id + "-" + this.namespaceCounter++,
      parent: parentId,
      explicit
    };
  }
  linkParentChild(parentId, childId) {
    const parent = this.namespaces.get(parentId);
    const child = this.namespaces.get(childId);
    if (!parent || !child) {
      return;
    }
    if (!parent.children.has(childId)) {
      parent.children.set(childId, child);
    }
    child.parent ??= parentId;
  }
  addNamespace(id, label) {
    const qualifiedId = _ClassDB.resolveQualifiedId(id, this.namespaceStack);
    this.namespaceStack.push(qualifiedId);
    if (this.namespaces.has(qualifiedId)) {
      const existing = this.namespaces.get(qualifiedId);
      existing.explicit = true;
      if (label) {
        existing.label = label;
      }
      return qualifiedId;
    }
    const parts = qualifiedId.split(".");
    const ancestorIds = _ClassDB.getAncestorIds(qualifiedId);
    for (let i = 0; i < ancestorIds.length; i++) {
      const currentId = ancestorIds[i];
      const parentId = i > 0 ? ancestorIds[i - 1] : void 0;
      const isLeaf = i === ancestorIds.length - 1;
      const nodeLabel = isLeaf && label ? label : parts[i];
      if (!this.namespaces.has(currentId)) {
        this.namespaces.set(
          currentId,
          this.createNamespaceNode(currentId, nodeLabel, parentId, isLeaf)
        );
      } else if (isLeaf) {
        this.namespaces.get(currentId).explicit = true;
      }
      if (parentId) {
        this.linkParentChild(parentId, currentId);
      }
    }
    return qualifiedId;
  }
  popNamespace() {
    this.namespaceStack.pop();
  }
  getNamespace(name) {
    return this.namespaces.get(name);
  }
  getNamespaces() {
    return this.namespaces;
  }
  /**
   * Function called by parser when a namespace definition has been found.
   *
   * @param id - ID of the namespace to add
   * @param classNames - IDs of the class to add
   * @param noteNames - IDs of the notes to add
   * @public
   */
  addClassesToNamespace(id, classNames, noteNames) {
    if (!this.namespaces.has(id)) {
      return;
    }
    for (const name of classNames) {
      const { className } = this.splitClassNameAndType(name);
      const classNode = this.getClass(className);
      classNode.parent = id;
      this.namespaces.get(id).classes.set(className, classNode);
    }
    for (const noteName of noteNames) {
      const noteNode = this.getNote(noteName);
      noteNode.parent = id;
      this.namespaces.get(id).notes.set(noteName, noteNode);
    }
  }
  setCssStyle(id, styles) {
    const thisClass = this.classes.get(id);
    if (!styles || !thisClass) {
      return;
    }
    for (const s of styles) {
      if (s.includes(",")) {
        thisClass.styles.push(...s.split(","));
      } else {
        thisClass.styles.push(s);
      }
    }
  }
  /**
   * Gets the arrow marker for a type index
   *
   * @param type - The type to look for
   * @returns The arrow marker
   */
  getArrowMarker(type) {
    let marker;
    switch (type) {
      case 0:
        marker = "aggregation";
        break;
      case 1:
        marker = "extension";
        break;
      case 2:
        marker = "composition";
        break;
      case 3:
        marker = "dependency";
        break;
      case 4:
        marker = "lollipop";
        break;
      default:
        marker = "none";
    }
    return marker;
  }
  /**
   * Walks up the namespace tree from the given id and returns the nearest ancestor
   * (or the id itself) that is marked as explicit. Used by compact rendering mode
   * to reassign children to the nearest user-declared namespace.
   */
  resolveExplicitAncestor(id) {
    let current = id;
    while (current) {
      const ns = this.namespaces.get(current);
      if (!ns) {
        return void 0;
      }
      if (ns.explicit) {
        return current;
      }
      current = ns.parent;
    }
    return void 0;
  }
  getData() {
    const nodes = [];
    const edges = [];
    const config = getConfig();
    const hierarchical = config.class?.hierarchicalNamespaces ?? true;
    for (const namespace of this.namespaces.values()) {
      if (!hierarchical && !namespace.explicit) {
        continue;
      }
      const node = {
        id: namespace.id,
        label: hierarchical ? namespace.label : namespace.id,
        isGroup: true,
        padding: config.class.padding ?? 16,
        // parent node must be one of [rect, roundedWithTitle, noteGroup, divider]
        shape: "rect",
        cssStyles: [],
        look: config.look,
        parentId: hierarchical ? namespace.parent : void 0
      };
      nodes.push(node);
    }
    for (const classNode of this.classes.values()) {
      const parentId = hierarchical ? classNode.parent : this.resolveExplicitAncestor(classNode.parent);
      const node = {
        ...classNode,
        type: void 0,
        isGroup: false,
        parentId,
        look: config.look
      };
      nodes.push(node);
    }
    for (const note of this.notes.values()) {
      const noteParentId = hierarchical ? note.parent : this.resolveExplicitAncestor(note.parent);
      const noteNode = {
        id: note.id,
        label: note.text,
        isGroup: false,
        shape: "note",
        padding: config.class.padding ?? 6,
        cssStyles: [
          "text-align: left",
          "white-space: nowrap",
          `fill: ${config.themeVariables.noteBkgColor}`,
          `stroke: ${config.themeVariables.noteBorderColor}`
        ],
        look: config.look,
        parentId: noteParentId,
        labelType: "markdown"
      };
      nodes.push(noteNode);
      const noteClassId = this.classes.get(note.class)?.id;
      if (noteClassId) {
        const edge = {
          id: `edgeNote${note.index}`,
          start: note.id,
          end: noteClassId,
          type: "normal",
          thickness: "normal",
          classes: "relation",
          arrowTypeStart: "none",
          arrowTypeEnd: "none",
          arrowheadStyle: "",
          labelStyle: [""],
          style: ["fill: none"],
          pattern: "dotted",
          look: config.look
        };
        edges.push(edge);
      }
    }
    for (const _interface of this.interfaces) {
      const interfaceNode = {
        id: _interface.id,
        label: _interface.label,
        isGroup: false,
        shape: "rect",
        cssStyles: ["opacity: 0;"],
        look: config.look
      };
      nodes.push(interfaceNode);
    }
    let cnt = 0;
    for (const classRelation of this.relations) {
      cnt++;
      const edge = {
        id: getEdgeId(classRelation.id1, classRelation.id2, {
          prefix: "id",
          counter: cnt
        }),
        start: classRelation.id1,
        end: classRelation.id2,
        type: "normal",
        label: classRelation.title,
        labelpos: "c",
        thickness: "normal",
        classes: "relation",
        arrowTypeStart: this.getArrowMarker(classRelation.relation.type1),
        arrowTypeEnd: this.getArrowMarker(classRelation.relation.type2),
        startLabelRight: classRelation.relationTitle1 === "none" ? "" : classRelation.relationTitle1,
        endLabelLeft: classRelation.relationTitle2 === "none" ? "" : classRelation.relationTitle2,
        arrowheadStyle: "",
        labelStyle: ["display: inline-block"],
        style: classRelation.style || "",
        pattern: classRelation.relation.lineType == 1 ? "dashed" : "solid",
        look: config.look,
        labelType: "markdown"
      };
      edges.push(edge);
    }
    return { nodes, edges, other: {}, config, direction: this.getDirection() };
  }
};

// src/diagrams/class/styles.js
var getStyles = /* @__PURE__ */ __name((options) => `g.classGroup text {
  fill: ${options.nodeBorder || options.classText};
  stroke: none;
  font-family: ${options.fontFamily};
  font-size: 10px;

  .title {
    font-weight: bolder;
  }

}

  .cluster-label text {
    fill: ${options.titleColor};
  }
  .cluster-label span {
    color: ${options.titleColor};
  }
  .cluster-label span p {
    background-color: transparent;
  }

  .cluster rect {
    fill: ${options.clusterBkg};
    stroke: ${options.clusterBorder};
    stroke-width: 1px;
  }

  .cluster text {
    fill: ${options.titleColor};
  }

  .cluster span {
    color: ${options.titleColor};
  }

.nodeLabel, .edgeLabel {
  color: ${options.classText};
}

.noteLabel .nodeLabel, .noteLabel .edgeLabel {
  color: ${options.noteTextColor};
}
.edgeLabel .label rect {
  fill: ${options.mainBkg};
}
.label text {
  fill: ${options.classText};
}

.labelBkg {
  background: ${options.mainBkg};
}
.edgeLabel .label span {
  background: ${options.mainBkg};
}

.classTitle {
  font-weight: bolder;
}
.node rect,
  .node circle,
  .node ellipse,
  .node polygon,
  .node path {
    fill: ${options.mainBkg};
    stroke: ${options.nodeBorder};
    stroke-width: ${options.strokeWidth};
  }


.divider {
  stroke: ${options.nodeBorder};
  stroke-width: 1;
}

g.clickable {
  cursor: pointer;
}

g.classGroup rect {
  fill: ${options.mainBkg};
  stroke: ${options.nodeBorder};
}

g.classGroup line {
  stroke: ${options.nodeBorder};
  stroke-width: 1;
}

.classLabel .box {
  stroke: none;
  stroke-width: 0;
  fill: ${options.mainBkg};
  opacity: 0.5;
}

.classLabel .label {
  fill: ${options.nodeBorder};
  font-size: 10px;
}

.relation {
  stroke: ${options.lineColor};
  stroke-width: ${options.strokeWidth};
  fill: none;
}

.dashed-line{
  stroke-dasharray: 3;
}

.dotted-line{
  stroke-dasharray: 1 2;
}

[id$="-compositionStart"], .composition {
  fill: ${options.lineColor} !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-compositionEnd"], .composition {
  fill: ${options.lineColor} !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-dependencyStart"], .dependency {
  fill: ${options.lineColor} !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-dependencyEnd"], .dependency {
  fill: ${options.lineColor} !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-extensionStart"], .extension {
  fill: transparent !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-extensionEnd"], .extension {
  fill: transparent !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-aggregationStart"], .aggregation {
  fill: transparent !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-aggregationEnd"], .aggregation {
  fill: transparent !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-lollipopStart"], .lollipop {
  fill: ${options.mainBkg} !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

[id$="-lollipopEnd"], .lollipop {
  fill: ${options.mainBkg} !important;
  stroke: ${options.lineColor} !important;
  stroke-width: 1;
}

.edgeTerminals {
  font-size: 11px;
  line-height: initial;
}

.classTitleText {
  text-anchor: middle;
  font-size: 18px;
  fill: ${options.textColor};
}

.edgeLabel[data-look="neo"] {
  background-color: ${options.edgeLabelBackground};
  p {
    background-color: ${options.edgeLabelBackground};
  }
  rect {
    opacity: 0.5;
    background-color: ${options.edgeLabelBackground};
    fill: ${options.edgeLabelBackground};
  }
  text-align: center;
}
  ${getIconStyles()}
`, "getStyles");
var styles_default = getStyles;

// src/diagrams/class/classRenderer-v3-unified.ts
var getDir = /* @__PURE__ */ __name((parsedItem, defaultDir = "TB") => {
  if (!parsedItem.doc) {
    return defaultDir;
  }
  let dir = defaultDir;
  for (const parsedItemDoc of parsedItem.doc) {
    if (parsedItemDoc.stmt === "dir") {
      dir = parsedItemDoc.value;
    }
  }
  return dir;
}, "getDir");
var getClasses = /* @__PURE__ */ __name(function(text, diagramObj) {
  return diagramObj.db.getClasses();
}, "getClasses");
var draw = /* @__PURE__ */ __name(async function(text, id, _version, diag) {
  log.info("REF0:");
  log.info("Drawing class diagram (v3)", id);
  const { securityLevel, state: conf, layout } = getConfig();
  diag.db.setDiagramId(id);
  const data4Layout = diag.db.getData();
  const svg = getDiagramElement(id, securityLevel);
  data4Layout.type = diag.type;
  data4Layout.layoutAlgorithm = getRegisteredLayoutAlgorithm(layout);
  data4Layout.nodeSpacing = conf?.nodeSpacing || 50;
  data4Layout.rankSpacing = conf?.rankSpacing || 50;
  data4Layout.markers = ["aggregation", "extension", "composition", "dependency", "lollipop"];
  data4Layout.diagramId = id;
  await render(data4Layout, svg);
  const padding = 8;
  utils_default.insertTitle(
    svg,
    "classDiagramTitleText",
    conf?.titleTopMargin ?? 25,
    diag.db.getDiagramTitle()
  );
  setupViewPortForSVG(svg, padding, "classDiagram", conf?.useMaxWidth ?? true);
}, "draw");
var classRenderer_v3_unified_default = {
  getClasses,
  draw,
  getDir
};

export {
  classDiagram_default,
  ClassDB,
  styles_default,
  classRenderer_v3_unified_default
};
