import type SourceLocation from "../SourceLocation";
import type {DelimiterSize, MathClass, Measurement, Mode, Side, StyleStr} from "./index";
import type {AlignSpec, ColSeparationType} from "../environments/array";
import type {Atom} from "../atoms";
import type {MathFont} from "./fonts";
import type {Token} from "../Token";

// ParseNode from `Parser.formatUnsupportedCmd`
export type UnsupportedCmdParseNode = ColorNode;

export type NodeType = AnyParseNode["type"];

export type ParseNode<T extends NodeType> = ParseNodeMap[T];

export type AnyParseNode =
    | ArrayNode
    | CommutativeDiagramLabelNode
    | CommutativeDiagramLabelParentNode
    | ColorNode
    | ColorTokenNode
    | OperatorNode
    | OrdinaryGroupNode
    | RawNode
    | SizeNode
    | StylingNode
    | SupSubNode
    | TagNode
    | TextNode
    | UrlNode
    | VerbatimNode
    | AtomNode
    | MathOrdinaryNode
    | TextOrdinaryNode
    | SpacingNode
    | AccentTokenNode
    | OperatorTokenNode
    | AccentNode
    | AccentUnderNode
    | CarriageReturnNode
    | DelimiterSizingNode
    | EncloseNode
    | EnvironmentNode
    | FontNode
    | GeneralizedFractionNode
    | HorizontalBoxNode
    | HorizontalBraceNode
    | HrefNode
    | HtmlNode
    | HtmlMathmlNode
    | IncludeGraphicsNode
    | InfixNode
    | InternalNode
    | KernNode
    | LapNode
    | LeftRightNode
    | ClosingDelimiterNode
    | MathChoiceNode
    | MiddleNode
    | MathClassNode
    | OperatorNameNode
    | OverlineNode
    | UnderlineNode
    | PhantomNode
    | VerticalPhantomNode
    | PoorMansBoldNode
    | RaiseBoxNode
    | RuleNode
    | SizingNode
    | SmashNode
    | SqrtNode
    | VerticalCenterNode
    | ExtensibleArrowNode;

// ParseNode's corresponding to Symbol `Group`s in symbols.js.
export type SymbolParseNode =
    ParseNode<"atom"> |
    ParseNode<"accent-token"> |
    ParseNode<"mathord"> |
    ParseNode<"op-token"> |
    ParseNode<"spacing"> |
    ParseNode<"textord">;

type ParseNodeMap = {
    [K in NodeType]: Extract<AnyParseNode, {type: K}>;
};

type BaseNode = {
    mode: Mode;
    loc?: SourceLocation | null;
};

type ArrayNode = BaseNode & {
    type: "array";
    colSeparationType?: ColSeparationType;
    hskipBeforeAndAfter?: boolean;
    addJot?: boolean;
    cols?: AlignSpec[];
    arraystretch: number;
    body: AnyParseNode[][];
    // List of rows in the (2D) array.
    rowGaps: (Measurement | null)[];
    hLinesBeforeRow: boolean[][];
    // Whether each row should be automatically numbered, or an explicit tag
    tags?: (boolean | AnyParseNode[])[];
    leqno?: boolean;
};

type CommutativeDiagramLabelNode = BaseNode & {
    type: "cdlabel";
    side: Side;
    label: AnyParseNode;
};

type CommutativeDiagramLabelParentNode = BaseNode & {
    type: "cdlabelparent";
    fragment: AnyParseNode;
}

type ColorNode = BaseNode & {
    type: "color";
    color: string;
    body: AnyParseNode[];
}

type ColorTokenNode = BaseNode & {
    type: "color-token";
    color: string;
}

type OperatorNodeBase = BaseNode & {
    type: "op";
    limits: boolean;
    alwaysHandleSupSub?: boolean;
    suppressBaseShift?: boolean;
    parentIsSupSub: boolean;
}

type OperatorNode =
    | OperatorNodeBase & {symbol: true; name: string; body?: never;}
    | OperatorNodeBase & {symbol: false; body: AnyParseNode[]; name?: never;}
    | OperatorNodeBase & {symbol: false; name: string; body?: never;};

type OrdinaryGroupNode = BaseNode & {
    type: "ordgroup";
    body: AnyParseNode[];
    semisimple?: boolean;
}

type RawNode = BaseNode & {
    type: "raw";
    string: string;
}

type SizeNode = BaseNode & {
    type: "size";
    value: Measurement;
    isBlank: boolean;
}

type StylingNode = BaseNode & {
    type: "styling";
    style: StyleStr;
    resetFont?: boolean;
    body: AnyParseNode[];
}

type SupSubNode = BaseNode & {
    type: "supsub";
    base: AnyParseNode | null;
} & (
    | {sup: AnyParseNode; sub?: AnyParseNode;}
    | {sup?: AnyParseNode; sub: AnyParseNode;}
    );

type TagNode = BaseNode & {
    type: "tag";
    body: AnyParseNode[];
    tag: AnyParseNode[];
}

type TextNode = BaseNode & {
    type: "text";
    body: AnyParseNode[];
    font?: string;
}

type UrlNode = BaseNode & {
    type: "url";
    url: string;
}

type VerbatimNode = BaseNode & {
    type: "verb";
    body: string;
    star: boolean;
}

type SymbolNode = BaseNode & {
    text: string;
}

type AtomNode = SymbolNode & {
    type: "atom";
    family: Atom;
}

type MathOrdinaryNode = SymbolNode & {
    type: "mathord";
}

type TextOrdinaryNode = SymbolNode & {
    type: "textord";
}

type SpacingNode = SymbolNode & {
    type: "spacing";
}

type AccentTokenNode = SymbolNode & {
    type: "accent-token";
}

type OperatorTokenNode = SymbolNode & {
    type: "op-token";
}

type AccentBaseNode = BaseNode & {
    label: string;
    isStretchy?: boolean;
    isShifty?: boolean;
    base: AnyParseNode;
}

type AccentNode = AccentBaseNode & {
    type: "accent";
}

type AccentUnderNode = AccentBaseNode & {
    type: "accentUnder";
}

type CarriageReturnNode = BaseNode & {
    type: "cr";
    newLine: boolean;
    size: Measurement | null;
}

type DelimiterSizingNode = BaseNode & {
    type: "delimsizing";
    size: DelimiterSize;
    mclass: "mopen" | "mclose" | "mrel" | "mord";
    delim: string;
}

type EncloseNode = BaseNode & {
    type: "enclose";
    body: AnyParseNode;
    label: string;
    backgroundColor?: string;
    borderColor?: string;
}

type EnvironmentNode = BaseNode & {
    type: "environment";
    name: string;
    nameGroup: AnyParseNode;
}

type FontNode = BaseNode & {
    type: "font";
    font: Exclude<MathFont, "">;
    body: AnyParseNode;
}

type GeneralizedFractionNode = BaseNode & {
    type: "genfrac";
    continued: boolean;
    numer: AnyParseNode;
    denom: AnyParseNode;
    hasBarLine: boolean;
    leftDelim: string | null;
    rightDelim: string | null;
    barSize: Measurement | null;
}

type HorizontalBoxNode = BaseNode & {
    type: "hbox";
    body: AnyParseNode[];
}

type HorizontalBraceNode = BaseNode & {
    type: "horizBrace";
    label: string;
    isOver: boolean;
    base: AnyParseNode;
}

type HrefNode = BaseNode & {
    type: "href";
    href: string;
    body: AnyParseNode[];
}

type HtmlNode = BaseNode & {
    type: "html";
    attributes: Record<string, string>;
    body: AnyParseNode[];
}

type HtmlMathmlNode = BaseNode & {
    type: "htmlmathml";
    html: AnyParseNode[];
    mathml: AnyParseNode[];
}

type IncludeGraphicsNode = BaseNode & {
    type: "includegraphics";
    alt: string;
    width: Measurement;
    height: Measurement;
    totalheight: Measurement;
    src: string;
}

type InfixNode = BaseNode & {
    type: "infix";
    replaceWith: string;
    size?: Measurement;
    token?: Token;
}

type InternalNode = BaseNode & {
    type: "internal";
}

type KernNode = BaseNode & {
    type: "kern";
    dimension: Measurement;
}

type LapNode = BaseNode & {
    type: "lap";
    alignment: string;
    body: AnyParseNode;
}
type LeftRightNode = BaseNode & {
    type: "leftright";
    body: AnyParseNode[];
    left: string;
    right: string;
    rightColor?: string;
}

type ClosingDelimiterNode = BaseNode & {
    type: "leftright-right";
    delim: string;
    color?: string;
}

type MathChoiceNode = BaseNode & {
    type: "mathchoice";
    display: AnyParseNode[];
    text: AnyParseNode[];
    script: AnyParseNode[];
    scriptscript: AnyParseNode[];
}

type MiddleNode = BaseNode & {
    type: "middle";
    delim: string;
}

type MathClassNode = BaseNode & {
    type: "mclass";
    mclass: MathClass;
    body: AnyParseNode[];
    isCharacterBox: boolean;
}

type OperatorNameNode = BaseNode & {
    type: "operatorname";
    body: AnyParseNode[];
    alwaysHandleSupSub: boolean;
    limits: boolean;
    parentIsSupSub: boolean;
}

type OverlineNode = BaseNode & {
    type: "overline";
    body: AnyParseNode;
}

type UnderlineNode = BaseNode & {
    type: "underline";
    body: AnyParseNode;
}

type PhantomNode = BaseNode & {
    type: "phantom";
    body: AnyParseNode[];
}

type VerticalPhantomNode = BaseNode & {
    type: "vphantom";
    body: AnyParseNode;
}

type PoorMansBoldNode = BaseNode & {
    type: "pmb";
    mclass: MathClass;
    body: AnyParseNode[];
}

type RaiseBoxNode = BaseNode & {
    type: "raisebox";
    dy: Measurement;
    body: AnyParseNode;
}

type RuleNode = BaseNode & {
    type: "rule";
    shift: Measurement | null | undefined;
    width: Measurement;
    height: Measurement;
}

type SizingNode = BaseNode & {
    type: "sizing";
    size: number;
    body: AnyParseNode[];
}

type SmashNode = BaseNode & {
    type: "smash";
    body: AnyParseNode;
    smashHeight: boolean;
    smashDepth: boolean;
}

type SqrtNode = BaseNode & {
    type: "sqrt";
    body: AnyParseNode;
    index: AnyParseNode | null;
}

type VerticalCenterNode = BaseNode & {
    type: "vcenter";
    body: AnyParseNode;
}

type ExtensibleArrowNode = BaseNode & {
    type: "xArrow";
    label: string;
    body: AnyParseNode;
    below: AnyParseNode | null;
}
