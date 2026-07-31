import type Parser from "./Parser";
import type Options from "./Options";
import type {ArgType, BreakToken} from "./types";
import type {AnyParseNode, NodeType, ParseNode, UnsupportedCmdParseNode} from "./types/nodes";
import type {HtmlDomNode} from "./domTree";
import type {Token} from "./Token";
import type {MathDomNode} from "./mathMLTree";

/** Context provided to function handlers for error messages. */
export type FunctionContext<FUNCNAME extends string = string> = {
    funcName: FUNCNAME;
    parser: Parser;
    token?: Token;
    breakOnTokenText?: BreakToken;
};

export type FunctionHandler<
    NODETYPE extends NodeType,
    FUNCNAME extends string = string,
> = (
    context: FunctionContext<FUNCNAME>,
    args: AnyParseNode[],
    optArgs: (AnyParseNode | null)[],
) => UnsupportedCmdParseNode | ParseNode<NODETYPE>;
// Note: reverse the order of the return type union will cause a flow error.
// See https://github.com/facebook/flow/issues/3663.

export type HtmlBuilder<NODETYPE extends NodeType> =
    (group: ParseNode<NODETYPE>, options: Options) => HtmlDomNode;
export type MathMLBuilder<NODETYPE extends NodeType> =
    (group: ParseNode<NODETYPE>, options: Options) => MathDomNode;

// More general version of `HtmlBuilder` for nodes (e.g. \sum, accent types)
// whose presence impacts super/subscripting. In this case, ParseNode<"supsub">
// delegates its HTML building to the HtmlBuilder corresponding to these nodes.
export type HtmlBuilderSupSub<NODETYPE extends NodeType> =
    (group: ParseNode<"supsub"> | ParseNode<NODETYPE>, options: Options) => HtmlDomNode;

/**
 * Parser-facing function spec.  Optional properties should use the defaults
 * documented below.
 */
export type FunctionSpec<
    NODETYPE extends NodeType,
    FUNCNAME extends string = string,
> = {
    /**
     * Unique string to differentiate parse nodes.
     * Also determines the type of the value returned by `handler`.
     */
    type: NODETYPE;

    /** The number of arguments the function takes. */
    numArgs: number;

    /**
     * An array corresponding to each argument of the function, giving the
     * type of argument that should be parsed. Its length should be equal
     * to `numOptionalArgs + numArgs`, and types for optional arguments
     * should appear before types for mandatory arguments.
     */
    argTypes?: ArgType[];

    /**
     * Whether it expands to a single token or a braced group of tokens.
     * If it's grouped, it can be used as an argument to primitive commands,
     * such as \sqrt (without the optional argument) and super/subscript.
     * (default false)
     */
    allowedInArgument?: boolean;

    /**
     * Whether or not the function is allowed inside text mode
     * (default false)
     */
    allowedInText?: boolean;

    /**
     * Whether or not the function is allowed inside math mode
     * (default true)
     */
    allowedInMath?: boolean;

    /**
     * The number of optional arguments the function should parse. If the
     * optional arguments aren't found, `null` will be passed to the handler in
     * their place.
     * (default 0)
     */
    numOptionalArgs?: number;

    /** Must be true if the function is an infix operator. */
    infix?: boolean;

    /** Whether or not the function is a TeX primitive. */
    primitive?: boolean;

    /**
     * The handler is called to handle these functions and their arguments and
     * returns a `ParseNode`.  It must be specified unless it's handled directly
     * in the parser.
     */
    handler: FunctionHandler<NODETYPE, FUNCNAME> | null | undefined;
};

/**
 * Builder fields consumed during registration.  These are stored separately in
 * `_htmlGroupBuilders` and `_mathmlGroupBuilders`, and are not used by Parser.
 */
export type FunctionBuilders<NODETYPE extends NodeType> = {
    /**
     * This function returns an object representing the DOM structure to be
     * created when rendering the defined LaTeX function.
     * This should not modify the `ParseNode`.
     */
    htmlBuilder?: HtmlBuilder<NODETYPE>;

    /**
     * This function returns an object representing the MathML structure to be
     * created when rendering the defined LaTeX function.
     * This should not modify the `ParseNode`.
     */
    mathmlBuilder?: MathMLBuilder<NODETYPE>;
};

/**
 * Full registration spec passed to `defineFunction`.  It combines the
 * parser-facing fields with optional builder fields and the names being
 * registered.
 */
type FunctionDefSpec<
    NODETYPE extends NodeType,
    NAMES extends readonly string[],
> =
    FunctionSpec<NODETYPE, NAMES[number]> & FunctionBuilders<NODETYPE> & {
        /**
         * The first argument to defineFunction is a single name or a list of names.
         * All functions named in such a list will share a single implementation.
         */
        names: NAMES;
    };

/**
 * All registered functions.
 * `functions.js` just exports this same dictionary again and makes it public.
 * `Parser.js` requires this dictionary.
 */
export const _functions: Record<string, FunctionSpec<NodeType>> = {};

/**
 * All HTML builders. Should be only used in the `define*` and the `build*ML`
 * functions.
 *
 * Builders for different node types are stored side by side, but
 * `HtmlBuilder<T>` is contravariant in `T`, so there is no single type
 * argument that makes storing/retrieving them typecheck.  `any` is used
 * as an existential-quantifier escape hatch.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const _htmlGroupBuilders: Record<string, HtmlBuilder<any>> = {};

/**
 * All MathML builders. Should be only used in the `define*` and the `build*ML`
 * functions.  See `_htmlGroupBuilders` above for the rationale behind `any`.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const _mathmlGroupBuilders: Record<string, MathMLBuilder<any>> = {};

export default function defineFunction<
    NODETYPE extends NodeType,
    const NAMES extends readonly string[],
>(
    data: FunctionDefSpec<NODETYPE, NAMES>,
) {
    const {type, names, htmlBuilder, mathmlBuilder} = data;
    for (let i = 0; i < names.length; ++i) {
        // To avoid destructuring and rebuilding an object,
        // we store the entire FunctionDefSpec object,
        // even though Parser only needs the FunctionSpec fields.
        _functions[names[i]] = data;
    }
    if (type) {
        if (htmlBuilder) {
            _htmlGroupBuilders[type] = htmlBuilder;
        }
        if (mathmlBuilder) {
            _mathmlGroupBuilders[type] = mathmlBuilder;
        }
    }
}

/**
 * Use this to register only the HTML and MathML builders for a function (e.g.
 * if the function's ParseNode is generated in Parser.js rather than via a
 * stand-alone handler provided to `defineFunction`).
 */
export function defineFunctionBuilders<NODETYPE extends NodeType>({
    type, htmlBuilder, mathmlBuilder,
}: {
    type: NODETYPE;
    htmlBuilder?: HtmlBuilder<NODETYPE>;
    mathmlBuilder: MathMLBuilder<NODETYPE>;
}) {
    if (htmlBuilder) {
        _htmlGroupBuilders[type] = htmlBuilder;
    }
    if (mathmlBuilder) {
        _mathmlGroupBuilders[type] = mathmlBuilder;
    }
}

export const normalizeArgument = function(arg: AnyParseNode): AnyParseNode {
    return arg.type === "ordgroup" && arg.body.length === 1 ? arg.body[0] : arg;
};

// Since the corresponding buildHTML/buildMathML function expects a
// list of elements, we normalize for different kinds of arguments
export const ordargument = function(arg: AnyParseNode): AnyParseNode[] {
    return arg.type === "ordgroup" ? arg.body : [arg];
};
