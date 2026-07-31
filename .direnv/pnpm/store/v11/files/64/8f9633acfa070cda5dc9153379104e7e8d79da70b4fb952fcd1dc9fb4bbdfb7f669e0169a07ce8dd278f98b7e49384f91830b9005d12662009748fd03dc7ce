// TODO(kevinb): implement \\sl and \\sc

import {binrelClass} from "./mclass";
import defineFunction, {normalizeArgument} from "../defineFunction";
import {isCharacterBox} from "../utils";

import * as html from "../buildHTML";
import * as mml from "../buildMathML";

import type Options from "../Options";
import type {ParseNode} from "../types/nodes";
import type {Slice1} from "../types";

const htmlBuilder = (group: ParseNode<"font">, options: Options) => {
    const font = group.font;
    const newOptions = options.withFont(font);
    return html.buildGroup(group.body, newOptions);
};

const mathmlBuilder = (group: ParseNode<"font">, options: Options) => {
    const font = group.font;
    const newOptions = options.withFont(font);
    return mml.buildGroup(group.body, newOptions);
};

const fontAliases = {
    "\\Bbb": "\\mathbb",
    "\\bold": "\\mathbf",
    "\\frak": "\\mathfrak",
} as const;

defineFunction({
    type: "font",
    names: [
        // styles, except \boldsymbol defined below
        "\\mathrm", "\\mathit", "\\mathbf", "\\mathnormal", "\\mathsfit",

        // families
        "\\mathbb", "\\mathcal", "\\mathfrak", "\\mathscr", "\\mathsf",
        "\\mathtt",

        // aliases, except \bm defined below
        "\\Bbb", "\\bold", "\\frak",
    ],
    numArgs: 1,
    allowedInArgument: true,

    handler: ({parser, funcName}, args) => {
        const body = normalizeArgument(args[0]);
        const func = funcName in fontAliases
            ? fontAliases[funcName as keyof typeof fontAliases]
            : funcName as Exclude<typeof funcName, keyof typeof fontAliases>;
        return {
            type: "font",
            mode: parser.mode,
            font: func.slice(1) as Slice1<typeof func>,
            body,
        };
    },

    htmlBuilder,
    mathmlBuilder,
});

defineFunction({
    type: "mclass",
    names: ["\\boldsymbol", "\\bm"],
    numArgs: 1,

    handler: ({parser}, args) => {
        const body = args[0];
        // amsbsy.sty's \boldsymbol uses \binrel spacing to inherit the
        // argument's bin|rel|ord status
        return {
            type: "mclass",
            mode: parser.mode,
            mclass: binrelClass(body),
            body: [
                {
                    type: "font",
                    mode: parser.mode,
                    font: "boldsymbol",
                    body,
                },
            ],
            isCharacterBox: isCharacterBox(body),
        };
    },
});

// Old font changing functions
defineFunction({
    type: "font",
    names: ["\\rm", "\\sf", "\\tt", "\\bf", "\\it", "\\cal"],
    numArgs: 0,
    allowedInText: true,
    handler: ({parser, funcName, breakOnTokenText}, args) => {
        const {mode} = parser;
        const body = parser.parseExpression(true, breakOnTokenText);

        return {
            type: "font",
            mode: mode,
            font: `math${funcName.slice(1) as Slice1<typeof funcName>}` as const,
            body: {
                type: "ordgroup",
                mode: parser.mode,
                body,
            },
        };
    },
});
