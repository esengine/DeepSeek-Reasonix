import defineFunction from "../defineFunction";
import {MathNode} from "../mathMLTree";
import Style from "../Style";
import {sizingGroup} from "./sizing";

import * as mml from "../buildMathML";
import type {StyleStr} from "../types";

const styleMap: Record<StyleStr, typeof Style.DISPLAY> = {
    "display": Style.DISPLAY,
    "text": Style.TEXT,
    "script": Style.SCRIPT,
    "scriptscript": Style.SCRIPTSCRIPT,
};

function isStyleStr(s: string): s is StyleStr {
    return s in styleMap;
}

defineFunction({
    type: "styling",
    names: [
        "\\displaystyle", "\\textstyle", "\\scriptstyle",
        "\\scriptscriptstyle",
    ],
    props: {
        numArgs: 0,
        allowedInText: true,
        primitive: true,
    },
    handler({breakOnTokenText, funcName, parser}, args) {
        // parse out the implicit body
        const body = parser.parseExpression(true, breakOnTokenText);

        // TODO: Refactor to avoid duplicating styleMap in multiple places (e.g.
        // here and in buildHTML and de-dupe the enumeration of all the styles).
        const style = funcName.slice(1, funcName.length - 5);
        if (!isStyleStr(style)) {
            throw new Error(`Unknown style: ${style}`);
        }
        return {
            type: "styling",
            mode: parser.mode,
            // Figure out what style to use by pulling out the style from
            // the function name
            style,
            body,
        };
    },
    htmlBuilder(group, options) {
        // Style changes are handled in the TeXbook on pg. 442, Rule 3.
        const newStyle = styleMap[group.style];
        let newOptions = options.havingStyle(newStyle);
        if (group.resetFont) {
            newOptions = newOptions.withFont('');
        }
        return sizingGroup(group.body, newOptions, options);
    },
    mathmlBuilder(group, options) {
        // Figure out what style we're changing to.
        const newStyle = styleMap[group.style];
        let newOptions = options.havingStyle(newStyle);
        if (group.resetFont) {
            newOptions = newOptions.withFont('');
        }

        const inner = mml.buildExpression(group.body, newOptions);

        const node = new MathNode("mstyle", inner);

        const styleAttributes = {
            "display": ["0", "true"],
            "text": ["0", "false"],
            "script": ["1", "false"],
            "scriptscript": ["2", "false"],
        };

        const attr = styleAttributes[group.style];

        node.setAttribute("scriptlevel", attr[0]);
        node.setAttribute("displaystyle", attr[1]);

        return node;
    },
});
