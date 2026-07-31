import defineFunction, {ordargument} from "../defineFunction";
import {makeSpan} from "../buildCommon";
import {isCharacterBox} from "../utils";
import {MathNode} from "../mathMLTree";
import type {AnyParseNode, ParseNode} from "../types/nodes";

import * as html from "../buildHTML";
import * as mml from "../buildMathML";

import type Options from "../Options";
import type {MathClass, Slice5} from "../types";

function htmlBuilder(group: ParseNode<"mclass">, options: Options) {
    const elements = html.buildExpression(group.body, options, true);
    return makeSpan([group.mclass], elements, options);
}

function mathmlBuilder(group: ParseNode<"mclass">, options: Options) {
    let node: MathNode;
    const inner = mml.buildExpression(group.body, options);

    if (group.mclass === "minner") {
        node = new MathNode("mpadded", inner);
    } else if (group.mclass === "mord") {
        if (group.isCharacterBox) {
            node = inner[0];
            node.type = "mi";
        } else {
            node = new MathNode("mi", inner);
        }
    } else {
        if (group.isCharacterBox) {
            node = inner[0];
            node.type = "mo";
        } else {
            node = new MathNode("mo", inner);
        }

        // Set spacing based on what is the most likely adjacent atom type.
        // See TeXbook p170.
        if (group.mclass === "mbin") {
            node.attributes.lspace = "0.22em"; // medium space
            node.attributes.rspace = "0.22em";
        } else if (group.mclass === "mpunct") {
            node.attributes.lspace = "0em";
            node.attributes.rspace = "0.17em"; // thinspace
        } else if (group.mclass === "mopen" || group.mclass === "mclose") {
            node.attributes.lspace = "0em";
            node.attributes.rspace = "0em";
        }
        // MathML <mo> default space is 5/18 em, so <mrel> needs no action.
        // Ref: https://developer.mozilla.org/en-US/docs/Web/MathML/Element/mo
    }
    return node;
}

// Math class commands except \mathop
defineFunction({
    type: "mclass",
    names: [
        "\\mathord", "\\mathbin", "\\mathrel", "\\mathopen",
        "\\mathclose", "\\mathpunct", "\\mathinner",
    ],
    numArgs: 1,
    primitive: true,
    handler({parser, funcName}, args) {
        const body = args[0];
        return {
            type: "mclass",
            mode: parser.mode,
            mclass: `m${funcName.slice(5) as Slice5<typeof funcName>}`,
            body: ordargument(body),
            isCharacterBox: isCharacterBox(body),
        };
    },

    htmlBuilder,
    mathmlBuilder,
});

export const binrelClass = (arg: AnyParseNode): MathClass => {
    // \binrel@ spacing varies with (bin|rel|ord) of the atom in the argument.
    // (by rendering separately and with {}s before and after, and measuring
    // the change in spacing).  We'll do roughly the same by detecting the
    // atom type directly.
    const atom = (arg.type === "ordgroup" && arg.body.length ? arg.body[0] : arg);
    if (atom.type === "atom" && (atom.family === "bin" || atom.family === "rel")) {
        return `m${atom.family}`;
    } else {
        return "mord";
    }
};

// \@binrel{x}{y} renders like y but as mbin/mrel/mord if x is mbin/mrel/mord.
// This is equivalent to \binrel@{x}\binrel@@{y} in AMSTeX.
defineFunction({
    type: "mclass",
    names: ["\\@binrel"],
    numArgs: 2,

    handler({parser}, args) {
        return {
            type: "mclass",
            mode: parser.mode,
            mclass: binrelClass(args[0]),
            body: ordargument(args[1]),
            isCharacterBox: isCharacterBox(args[1]),
        };
    },
});

// Build a relation or stacked op by placing one symbol on top of another
defineFunction({
    type: "mclass",
    names: ["\\stackrel", "\\overset", "\\underset"],
    numArgs: 2,

    handler({parser, funcName}, args) {
        const baseArg = args[1];
        const shiftedArg = args[0];

        let mclass: MathClass;
        if (funcName !== "\\stackrel") {
            // LaTeX applies \binrel spacing to \overset and \underset.
            mclass = binrelClass(baseArg);
        } else {
            mclass = "mrel";  // for \stackrel
        }

        const baseOp: ParseNode<"op"> = {
            type: "op",
            mode: baseArg.mode,
            limits: true,
            alwaysHandleSupSub: true,
            parentIsSupSub: false,
            symbol: false,
            suppressBaseShift: funcName !== "\\stackrel",
            body: ordargument(baseArg),
        };

        const supsub: ParseNode<"supsub"> = funcName === "\\underset"
            ? {type: "supsub", mode: shiftedArg.mode, base: baseOp, sub: shiftedArg}
            : {type: "supsub", mode: shiftedArg.mode, base: baseOp, sup: shiftedArg};

        return {
            type: "mclass",
            mode: parser.mode,
            mclass,
            body: [supsub],
            isCharacterBox: isCharacterBox(supsub),
        };
    },
});
