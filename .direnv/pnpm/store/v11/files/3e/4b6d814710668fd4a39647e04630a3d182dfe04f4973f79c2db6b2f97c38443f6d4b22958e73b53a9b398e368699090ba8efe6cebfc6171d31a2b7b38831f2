import { CustomPatternMatcherFunc } from '@chevrotain/types';
import { DSLMethodOpts } from '@chevrotain/types';
import { EmbeddedActionsParser } from '@chevrotain/types';
import { ILexerErrorMessageProvider } from '@chevrotain/types';
import { ILexingError } from '@chevrotain/types';
import { IMultiModeLexerDefinition } from '@chevrotain/types';
import { IOrAlt } from '@chevrotain/types';
import { IParserConfig } from '@chevrotain/types';
import { IParserErrorMessageProvider } from '@chevrotain/types';
import { IRecognitionException } from '@chevrotain/types';
import { IRuleConfig } from '@chevrotain/types';
import { IToken } from '@chevrotain/types';
import { Lexer as Lexer_2 } from '@chevrotain/types';
import { TokenPattern } from '@chevrotain/types';
import { TokenType } from '@chevrotain/types';
import { TokenTypeDictionary } from '@chevrotain/types';
import { TokenVocabulary } from '@chevrotain/types';

export declare interface AbnfAlternation extends langium_2.AstNode {
    readonly $container: AbnfGroup | AbnfOptionalGroup | AbnfRule;
    readonly $type: 'AbnfAlternation';
    alternatives: Array<AbnfConcatenation>;
}

export declare const AbnfAlternation: {
    readonly $type: "AbnfAlternation";
    readonly alternatives: "alternatives";
};

export declare interface AbnfConcatenation extends langium_2.AstNode {
    readonly $container: AbnfAlternation;
    readonly $type: 'AbnfConcatenation';
    elements: Array<AbnfElement>;
}

export declare const AbnfConcatenation: {
    readonly $type: "AbnfConcatenation";
    readonly elements: "elements";
};

export declare interface AbnfElement extends langium_2.AstNode {
    readonly $container: AbnfConcatenation;
    readonly $type: 'AbnfElement';
    primary: AbnfPrimary;
    repeat?: string;
}

export declare const AbnfElement: {
    readonly $type: "AbnfElement";
    readonly primary: "primary";
    readonly repeat: "repeat";
};

export declare interface AbnfGroup extends AbnfPrimary {
    readonly $type: 'AbnfGroup';
    element: AbnfAlternation;
}

export declare const AbnfGroup: {
    readonly $type: "AbnfGroup";
    readonly element: "element";
};

export declare interface AbnfNumVal extends AbnfPrimary {
    readonly $type: 'AbnfNumVal';
    value: string;
}

export declare const AbnfNumVal: {
    readonly $type: "AbnfNumVal";
    readonly value: "value";
};

export declare interface AbnfOptionalGroup extends AbnfPrimary {
    readonly $type: 'AbnfOptionalGroup';
    element: AbnfAlternation;
}

export declare const AbnfOptionalGroup: {
    readonly $type: "AbnfOptionalGroup";
    readonly element: "element";
};

export declare interface AbnfPrimary extends langium_2.AstNode {
    readonly $type: 'AbnfGroup' | 'AbnfNumVal' | 'AbnfOptionalGroup' | 'AbnfPrimary' | 'AbnfRuleName' | 'AbnfStringLiteral';
}

export declare const AbnfPrimary: {
    readonly $type: "AbnfPrimary";
};

export declare interface AbnfRule extends langium_2.AstNode {
    readonly $container: RailroadAbnf;
    readonly $type: 'AbnfRule';
    definition: AbnfAlternation;
    name: string;
}

export declare const AbnfRule: {
    readonly $type: "AbnfRule";
    readonly definition: "definition";
    readonly name: "name";
};

export declare interface AbnfRuleName extends AbnfPrimary {
    readonly $type: 'AbnfRuleName';
    name: string;
}

export declare const AbnfRuleName: {
    readonly $type: "AbnfRuleName";
    readonly name: "name";
};

export declare interface AbnfStringLiteral extends AbnfPrimary {
    readonly $type: 'AbnfStringLiteral';
    value: string;
}

export declare const AbnfStringLiteral: {
    readonly $type: "AbnfStringLiteral";
    readonly value: "value";
};

/**
 * An abstract implementation of the {@link AstReflection} interface.
 * Serves to cache subtype computation results to improve performance throughout different parts of Langium.
 */
declare abstract class AbstractAstReflection implements AstReflection {
    readonly types: AstMetaData;
    protected subtypes: Record<string, Record<string, boolean | undefined>>;
    protected allSubtypes: Record<string, string[] | undefined>;
    getAllTypes(): string[];
    getReferenceType(refInfo: ReferenceInfo): string;
    getTypeMetaData(type: string): TypeMetaData;
    isInstance(node: unknown, type: string): boolean;
    isSubtype(subtype: string, supertype: string): boolean;
    getAllSubTypes(type: string): string[];
}

declare interface AbstractCancellationTokenSource extends Disposable_2 {
    token: CancellationToken;
    cancel(): void;
}

declare abstract class AbstractCstNode implements CstNode {
    abstract get offset(): number;
    abstract get length(): number;
    abstract get end(): number;
    abstract get range(): Range_2;
    container?: CompositeCstNode;
    grammarSource?: AbstractElement;
    root: RootCstNode;
    private _astNode?;
    get hidden(): boolean;
    get astNode(): AstNode;
    set astNode(value: AstNode | undefined);
    get text(): string;
}

declare interface AbstractElement extends langium.AstNode {
    readonly $type: 'AbstractElement' | 'Action' | 'Alternatives' | 'Assignment' | 'CharacterRange' | 'CrossReference' | 'EndOfFile' | 'Group' | 'Keyword' | 'NegatedToken' | 'RegexToken' | 'RuleCall' | 'TerminalAlternatives' | 'TerminalElement' | 'TerminalGroup' | 'TerminalRuleCall' | 'UnorderedGroup' | 'UntilToken' | 'Wildcard';
    cardinality?: '*' | '+' | '?';
}

declare const AbstractElement: {
    readonly $type: "AbstractElement";
    readonly cardinality: "cardinality";
};

declare abstract class AbstractLangiumParser implements BaseParser {
    protected readonly lexer: Lexer;
    protected readonly wrapper: ChevrotainWrapper;
    protected _unorderedGroups: Map<string, boolean[]>;
    protected allRules: Map<string, RuleResult>;
    protected mainRule: RuleResult;
    constructor(services: LangiumCoreServices);
    alternatives(idx: number, choices: Array<IOrAlt<any>>): void;
    optional(idx: number, callback: DSLMethodOpts<unknown>): void;
    many(idx: number, callback: DSLMethodOpts<unknown>): void;
    atLeastOne(idx: number, callback: DSLMethodOpts<unknown>): void;
    abstract rule(rule: ParserRule | InfixRule, impl: RuleImpl): RuleResult;
    abstract consume(idx: number, tokenType: TokenType, feature: AbstractElement): void;
    abstract subrule(idx: number, rule: RuleResult, fragment: boolean, feature: AbstractElement, args: Args): void;
    abstract action($type: string, action: Action): void;
    getRule(name: string): RuleResult | undefined;
    isRecording(): boolean;
    get unorderedGroups(): Map<string, boolean[]>;
    getRuleStack(): number[];
    finalize(): void;
}

export declare abstract class AbstractMermaidTokenBuilder extends DefaultTokenBuilder {
    private keywords;
    constructor(keywords: string[]);
    protected buildKeywordTokens(rules: Stream<GrammarAST.AbstractRule>, terminalTokens: TokenType[], options?: TokenBuilderOptions): TokenType[];
}

export declare abstract class AbstractMermaidValueConverter extends DefaultValueConverter {
    /**
     * A method contains convert logic to be used by class.
     *
     * @param rule - Parsed rule.
     * @param input - Matched string.
     * @param cstNode - Node in the Concrete Syntax Tree (CST).
     * @returns converted the value if it's available or `undefined` if it's not.
     */
    protected abstract runCustomConverter(rule: GrammarAST.AbstractRule, input: string, cstNode: CstNode): ValueType | undefined;
    protected runConverter(rule: GrammarAST.AbstractRule, input: string, cstNode: CstNode): ValueType;
    private runCommonConverter;
}

declare abstract class AbstractParserErrorMessageProvider implements IParserErrorMessageProvider {
    buildMismatchTokenMessage(options: {
        expected: TokenType;
        actual: IToken;
        previous: IToken;
        ruleName: string;
    }): string;
    buildNotAllInputParsedMessage(options: {
        firstRedundant: IToken;
        ruleName: string;
    }): string;
    buildNoViableAltMessage(options: {
        expectedPathsPerAlt: TokenType[][][];
        actual: IToken[];
        previous: IToken;
        customUserDescription: string;
        ruleName: string;
    }): string;
    buildEarlyExitMessage(options: {
        expectedIterationPaths: TokenType[][];
        actual: IToken[];
        previous: IToken;
        customUserDescription: string;
        ruleName: string;
    }): string;
}

declare type AbstractParserRule = InfixRule | ParserRule;

declare const AbstractParserRule: {
    readonly $type: "AbstractParserRule";
};

declare type AbstractRule = AbstractParserRule | TerminalRule;

declare const AbstractRule: {
    readonly $type: "AbstractRule";
};

declare abstract class AbstractThreadedAsyncParser implements AsyncParser {
    /**
     * The thread count determines how many threads are used to parse files in parallel.
     * The default value is 8. Decreasing this value increases startup performance, but decreases parallel parsing performance.
     */
    protected threadCount: number;
    /**
     * The termination delay determines how long the parser waits for a thread to finish after a cancellation request.
     * The default value is 200(ms).
     */
    protected terminationDelay: number;
    protected workerPool: ParserWorker[];
    protected queue: Array<Deferred<ParserWorker>>;
    protected readonly hydrator: Hydrator;
    constructor(services: LangiumCoreServices);
    protected initializeWorkers(): void;
    parse<T extends AstNode>(text: string, cancelToken: CancellationToken): Promise<ParseResult<T>>;
    protected terminateWorker(worker: ParserWorker): void;
    protected acquireParserWorker(cancelToken: CancellationToken): Promise<ParserWorker>;
    protected abstract createWorker(): ParserWorker;
}

declare type AbstractType = AbstractParserRule | InferredType | Interface | Type;

declare const AbstractType: {
    readonly $type: "AbstractType";
};

declare interface Accelerator extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'Accelerator' | 'EOL';
    name: string;
    x: number;
    y: number;
}

declare const Accelerator: {
    readonly $type: "Accelerator";
    readonly name: "name";
    readonly x: "x";
    readonly y: "y";
};

declare interface Action extends AbstractElement {
    readonly $type: 'Action';
    feature?: FeatureName;
    inferredType?: InferredType;
    operator?: '+=' | '=';
    type?: langium.Reference<AbstractType>;
}

declare const Action: {
    readonly $type: "Action";
    readonly cardinality: "cardinality";
    readonly feature: "feature";
    readonly inferredType: "inferredType";
    readonly operator: "operator";
    readonly type: "type";
};

declare interface Alignment extends langium_2.AstNode {
    readonly $container: Architecture;
    readonly $type: 'Alignment' | 'EOL';
    direction: 'column' | 'row';
    members: Array<string>;
}

declare const Alignment: {
    readonly $type: "Alignment";
    readonly direction: "direction";
    readonly members: "members";
};

declare interface Alternatives extends AbstractElement {
    readonly $type: 'Alternatives';
    elements: Array<AbstractElement>;
}

declare const Alternatives: {
    readonly $type: "Alternatives";
    readonly cardinality: "cardinality";
    readonly elements: "elements";
};

declare interface Anchor extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'Anchor' | 'EOL';
    evolution: number;
    name: string;
    visibility: number;
}

declare const Anchor: {
    readonly $type: "Anchor";
    readonly evolution: "evolution";
    readonly name: "name";
    readonly visibility: "visibility";
};

declare interface Annotation extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'Annotation' | 'EOL';
    number: number;
    text: string;
    x: CoordinateValue;
    y: CoordinateValue;
}

declare const Annotation: {
    readonly $type: "Annotation";
    readonly number: "number";
    readonly text: "text";
    readonly x: "x";
    readonly y: "y";
};

declare interface Annotations extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'Annotations' | 'EOL';
    x: CoordinateValue;
    y: CoordinateValue;
}

declare const Annotations: {
    readonly $type: "Annotations";
    readonly x: "x";
    readonly y: "y";
};

export declare interface Architecture extends langium_2.AstNode {
    readonly $type: 'Architecture';
    accDescr?: string;
    accTitle?: string;
    alignments: Array<Alignment>;
    edges: Array<Edge>;
    groups: Array<Group_2>;
    junctions: Array<Junction>;
    services: Array<Service>;
    title?: string;
}

export declare const Architecture: {
    readonly $type: "Architecture";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly alignments: "alignments";
    readonly edges: "edges";
    readonly groups: "groups";
    readonly junctions: "junctions";
    readonly services: "services";
    readonly title: "title";
};

/**
 * Declaration of `Architecture` services.
 */
declare interface ArchitectureAddedServices {
    parser: {
        TokenBuilder: ArchitectureTokenBuilder;
        ValueConverter: ArchitectureValueConverter;
    };
}

export declare const ArchitectureGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'ArchitectureGrammar' language. */
declare namespace ArchitectureGrammar {
    const Terminals: {
        ARROW_DIRECTION: RegExp;
        ARROW_GROUP: RegExp;
        ARROW_INTO: RegExp;
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        STRING: RegExp;
        ID: RegExp;
        NEWLINE: RegExp;
        WHITESPACE: RegExp;
        YAML: RegExp;
        DIRECTIVE: RegExp;
        SINGLE_LINE_COMMENT: RegExp;
        ARCH_ICON: RegExp;
        ARCH_TITLE: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "-" | "--" | ":" | "align" | "architecture-beta" | "column" | "group" | "in" | "junction" | "row" | "service";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Alignment: Alignment;
        Architecture: Architecture;
        Edge: Edge;
        Group: Group_2;
        Junction: Junction;
        Service: Service;
    };
}

/**
 * Dependency injection module that overrides Langium default services and
 * contributes the declared `Architecture` services.
 */
export declare const ArchitectureModule: Module<ArchitectureServices, PartialLangiumCoreServices & ArchitectureAddedServices>;

/**
 * Union of Langium default services and `Architecture` services.
 */
export declare type ArchitectureServices = LangiumCoreServices & ArchitectureAddedServices;

declare class ArchitectureTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare class ArchitectureValueConverter extends AbstractMermaidValueConverter {
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

declare type Args = Record<string, boolean>;

declare interface ArrayLiteral extends langium.AstNode {
    readonly $container: ArrayLiteral | TypeAttribute;
    readonly $type: 'ArrayLiteral';
    elements: Array<ValueLiteral>;
}

declare const ArrayLiteral: {
    readonly $type: "ArrayLiteral";
    readonly elements: "elements";
};

declare interface ArrayType extends langium.AstNode {
    readonly $container: ArrayType | ReferenceType | Type | TypeAttribute | UnionType;
    readonly $type: 'ArrayType';
    elementType: TypeDefinition;
}

declare const ArrayType: {
    readonly $type: "ArrayType";
    readonly elementType: "elementType";
};

declare function assertCondition(condition: boolean, message?: string): asserts condition;

declare function assertUnreachable(_: never, message?: string): never;

/**
 * Assigns all mandatory AST properties to the specified node.
 *
 * @param reflection Reflection object used to gather mandatory properties for the node.
 * @param node Specified node is modified in place and properties are directly assigned.
 */
declare function assignMandatoryProperties(reflection: AstReflection, node: AstNode): void;

declare interface Assignment extends AbstractElement {
    readonly $type: 'Assignment';
    feature: FeatureName;
    operator: '+=' | '=' | '?=';
    predicate?: '->' | '=>';
    terminal: AbstractElement;
}

declare const Assignment: {
    readonly $type: "Assignment";
    readonly cardinality: "cardinality";
    readonly feature: "feature";
    readonly operator: "operator";
    readonly predicate: "predicate";
    readonly terminal: "terminal";
};

declare type Associativity = 'left' | 'right';

/**
 * A map of all AST node types and their meta data.
 */
declare interface AstMetaData {
    [type: string]: TypeMetaData;
}

/**
 * A node in the Abstract Syntax Tree (AST).
 */
declare interface AstNode {
    /** Every AST node has a type corresponding to what was specified in the grammar declaration. */
    readonly $type: string;
    /** The container node in the AST; every node except the root node has a container. */
    readonly $container?: AstNode;
    /** The property of the `$container` node that contains this node. This is either a direct reference or an array. */
    readonly $containerProperty?: string;
    /** In case `$containerProperty` is an array, the array index is stored here. */
    readonly $containerIndex?: number;
    /** The Concrete Syntax Tree (CST) node of the text range from which this node was parsed. */
    readonly $cstNode?: CstNode;
    /** The document containing the AST; only the root node has a direct reference to the document. */
    readonly $document?: LangiumDocument;
}

/**
 * A description of an AST node is used when constructing scopes and looking up cross-reference targets.
 */
declare interface AstNodeDescription {
    /** The target node; should be present only for local references (linking to the same document). */
    node?: AstNode;
    /**
     * The document segment that represents the range of the name of the AST node.
     */
    nameSegment?: DocumentSegment;
    /**
     * The document segment that represents the full range of the AST node.
     */
    selectionSegment?: DocumentSegment;
    /** `$type` property value of the AST node */
    type: string;
    /** Name of the AST node; this is usually determined by the `NameProvider` service. */
    name: string;
    /** URI to the document containing the AST node */
    documentUri: URI;
    /** Navigation path inside the document */
    path: string;
}

/**
 * Language-specific service for creating descriptions of AST nodes to be used for cross-reference resolutions.
 */
declare interface AstNodeDescriptionProvider {
    /**
     * Create a description for the given AST node. This service method is typically used while indexing
     * the contents of a document and during scope computation.
     *
     * @param node An AST node.
     * @param name The name to be used to refer to the AST node. By default, this is determined by the
     *     `NameProvider` service, but alternative names may be provided according to the semantics
     *     of your language.
     * @param document The document containing the AST node. If omitted, it is taken from the root AST node.
     */
    createDescription(node: AstNode, name: string | undefined, document?: LangiumDocument): AstNodeDescription;
}

/**
 * Language-specific service for locating an `AstNode` in a document.
 */
declare interface AstNodeLocator {
    /**
     * Creates a path represented by a `string` that identifies an `AstNode` inside its document.
     * It must be possible to retrieve exactly the same `AstNode` from the document using this path.
     *
     * @param node The `AstNode` for which to create the path.
     * @returns a path represented by a `string` that identifies `node` inside its document.
     * @see {@link getAstNode}
     */
    getAstNodePath(node: AstNode): string;
    /**
     * Locates an `AstNode` inside another node by following the given path.
     *
     * @param node Parent element.
     * @param path Describes how to locate the `AstNode` inside the given `node`.
     * @returns The `AstNode` located under the given path, or `undefined` if the path cannot be resolved.
     * @see {@link getAstNodePath}
     */
    getAstNode<T extends AstNode = AstNode>(node: AstNode, path: string): T | undefined;
}

/**
 * A {@link DocumentSegment} representing the definition area of an AstNode within the DSL text.
 * Usually contains text region information on all assigned property values of the AstNode,
 * and may contain the defining file's URI as string.
 */
declare interface AstNodeRegionWithAssignments extends DocumentSegment {
    /**
     * A record containing an entry for each assigned property of the AstNode.
     * The key is equal to the property name and the value is an array of the property values'
     * text regions, regardless of whether the property is a single value or list property.
     */
    assignments?: Record<string, DocumentSegment[]>;
    /**
     * The AstNode defining file's URI as string
     */
    documentURI?: string;
}

/**
 * Describes a union type including of all AstNode types containing cross-references.
 *  A is meant to be the interface `XXXAstType` fromm your generated `ast.ts` file.
 * Enhances compile-time validation of cross-reference distinctions, e.g. in scope providers
 *  in combination with `assertUnreachable(context.container)`.
 */
declare type AstNodeTypesWithCrossReferences<A extends AstTypeList<A>> = {
    [T in keyof A]-?: CrossReferencesOfAstNodeType<A[T]> extends never ? never : A[T];
}[keyof A];

/**
 * {@link AstNode}s that may carry a semantically relevant comment.
 */
declare interface AstNodeWithComment extends AstNode {
    $comment?: string;
}

/**
 * {@link AstNode}s that may carry information on their definition area within the DSL text.
 */
declare interface AstNodeWithTextRegion extends AstNode {
    $sourceText?: string;
    $textRegion?: AstNodeRegionWithAssignments;
}

/**
 * Service used for generic access to the structure of the AST. This service is shared between
 * all involved languages, so it operates on the superset of types of these languages.
 */
declare interface AstReflection {
    readonly types: AstMetaData;
    getAllTypes(): string[];
    getReferenceType(refInfo: ReferenceInfo): string;
    getTypeMetaData(type: string): TypeMetaData;
    isInstance(node: unknown, type: string): boolean;
    isSubtype(subtype: string, supertype: string): boolean;
    getAllSubTypes(type: string): string[];
}

declare interface AstStreamOptions {
    /**
     * Optional target range that the nodes in the stream need to intersect
     */
    range?: Range_2;
}

/**
 * Represents the enumeration-like type, that lists all AstNode types of your grammar.
 */
declare type AstTypeList<T> = Record<keyof T, AstNode>;

declare namespace AstUtils {
    export {
        linkContentToContainer,
        getContainerOfType,
        hasContainerOfType,
        getDocument,
        findRootNode,
        getReferenceNodes,
        streamContents,
        streamAllContents,
        streamAst,
        streamReferences,
        assignMandatoryProperties,
        copyAstNode,
        AstStreamOptions,
        DeepPartialAstNode
    }
}

declare interface AsyncDisposable_2 {
    /**
     * Dispose this object.
     */
    dispose(): Promise<void>;
}

/**
 * Async parser that allows cancellation of the current parsing process.
 *
 * @remarks
 * The sync parser implementation is blocking the event loop, which can become quite problematic for large files.
 * The default implementation is not actually async. It just wraps the sync parser in a promise. A real implementation would create worker threads or web workers to offload the parsing work.
 */
declare interface AsyncParser {
    /**
     * Parses the given text and returns the parse result.
     *
     * @param text The text to parse.
     * @param cancelToken A cancellation token that can be used to cancel the parsing process.
     * @returns A promise that resolves to the parse result.
     *
     * @throws `OperationCancelled` if the parsing process is cancelled.
     */
    parse<T extends AstNode>(text: string, cancelToken: CancellationToken): Promise<ParseResult<T>>;
}

declare interface Axis extends langium_2.AstNode {
    readonly $container: Radar;
    readonly $type: 'Axis';
    label?: string;
    name: string;
}

declare const Axis: {
    readonly $type: "Axis";
    readonly label: "label";
    readonly name: "name";
};

/**
 * Base interface for all parsers. Mainly used by the `parser-builder-base.ts` to perform work on different kinds of parsers.
 * The main use cases are:
 * * AST parser: Based on a string, create an AST for the current grammar
 * * Completion parser: Based on a partial string, identify the current position of the input within the grammar
 */
declare interface BaseParser {
    /**
     * Adds a new parser rule to the parser
     */
    rule(rule: ParserRule | InfixRule, impl: RuleImpl): RuleResult;
    /**
     * Returns the executable rule function for the specified rule name
     */
    getRule(name: string): RuleResult | undefined;
    /**
     * Performs alternatives parsing (the `|` operation in EBNF/Langium)
     */
    alternatives(idx: number, choices: Array<IOrAlt<any>>): void;
    /**
     * Parses the callback as optional (the `?` operation in EBNF/Langium)
     */
    optional(idx: number, callback: DSLMethodOpts<unknown>): void;
    /**
     * Parses the callback 0 or more times (the `*` operation in EBNF/Langium)
     */
    many(idx: number, callback: DSLMethodOpts<unknown>): void;
    /**
     * Parses the callback 1 or more times (the `+` operation in EBNF/Langium)
     */
    atLeastOne(idx: number, callback: DSLMethodOpts<unknown>): void;
    /**
     * Consumes a specific token type from the token input stream.
     * Requires a unique index within the rule for a specific token type.
     */
    consume(idx: number, tokenType: TokenType, feature: AbstractElement): void;
    /**
     * Invokes the executable function for a given parser rule.
     * Requires a unique index within the rule for a specific sub rule.
     * Arguments can be supplied to the rule invocation for semantic predicates
     */
    subrule(idx: number, rule: RuleResult, fragment: boolean, feature: AbstractElement, args: Args): void;
    /**
     * Executes a grammar action that modifies the currently active AST node
     */
    action($type: string, action: Action): void;
    /**
     * Whether the parser is currently actually in use or in "recording mode".
     * Recording mode is activated once when the parser is analyzing itself.
     * During this phase, no input exists and therefore no AST should be constructed
     */
    isRecording(): boolean;
    /**
     * Current state of the unordered groups
     */
    get unorderedGroups(): Map<string, boolean[]>;
    /**
     * The rule stack indicates the indices of rules that are currently invoked,
     * in order of their invocation.
     */
    getRuleStack(): number[];
}

declare class BiMap<K, V> {
    private map;
    private inverse;
    get size(): number;
    constructor();
    constructor(elements: Array<[K, V]>);
    clear(): void;
    set(key: K, value: V): this;
    get(key: K): V | undefined;
    getKey(value: V): K | undefined;
    delete(key: K): boolean;
}

declare interface BooleanLiteral extends langium.AstNode {
    readonly $container: ArrayLiteral | Conjunction | Disjunction | Group | NamedArgument | Negation | TypeAttribute;
    readonly $type: 'BooleanLiteral';
    true: boolean;
}

declare const BooleanLiteral: {
    readonly $type: "BooleanLiteral";
    readonly true: "true";
};

export declare interface Branch extends langium_2.AstNode {
    readonly $container: GitGraph;
    readonly $type: 'Branch' | 'EOL';
    name: string;
    order: number;
}

export declare const Branch: {
    readonly $type: "Branch";
    readonly name: "name";
    readonly order: "order";
};

declare interface BuildOptions {
    /**
     * Control the linking and references indexing phase with this option. The default if not specified is `true`.
     * If set to `false`, references can still be resolved - that's done lazily when you access the `ref` property of
     * a reference. But you won't get any diagnostics for linking errors and the references won't be considered
     * when updating other documents.
     */
    eagerLinking?: boolean;
    /**
     * Control the validation phase with this option:
     *  - `true` enables all validation checks and forces revalidating the documents
     *    In order to include additional, custom validation categories, override `DefaultDocumentBuilder.getAllValidationCategories(...)`.
     *  - `false` or `undefined` disables all validation checks
     *  - An object runs only the necessary validation checks; the `categories` property restricts this to a specific subset (which might include custom categories as well).
     */
    validation?: boolean | ValidationOptions;
}

/**
 * @since 3.16.0
 */
declare interface CallHierarchyClientCapabilities {
    /**
     * Whether implementation supports dynamic registration. If this is set to `true`
     * the client supports the new `(TextDocumentRegistrationOptions & StaticRegistrationOptions)`
     * return value for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
}

declare namespace Cancellation {
    export {
        CancellationToken,
        AbstractCancellationTokenSource,
        CancellationTokenSource
    }
}

/**
 * Defines a CancellationToken. This interface is not
 * intended to be implemented. A CancellationToken must
 * be created via a CancellationTokenSource.
 */
declare interface CancellationToken {
    /**
     * Is `true` when the token has been cancelled, `false` otherwise.
     */
    readonly isCancellationRequested: boolean;
    /**
     * An {@link Event event} which fires upon cancellation.
     */
    readonly onCancellationRequested: Event_2<any>;
}

declare namespace CancellationToken {
    const None: CancellationToken;
    const Cancelled: CancellationToken;
    function is(value: any): value is CancellationToken;
}

declare class CancellationTokenSource implements AbstractCancellationTokenSource {
    private _token;
    get token(): CancellationToken;
    cancel(): void;
    dispose(): void;
}

declare type Cardinality = '?' | '*' | '+' | undefined;

declare interface CharacterRange extends TerminalElement {
    readonly $type: 'CharacterRange';
    left: Keyword;
    right?: Keyword;
}

declare const CharacterRange: {
    readonly $type: "CharacterRange";
    readonly cardinality: "cardinality";
    readonly left: "left";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
    readonly right: "right";
};

declare interface Checkout extends langium_2.AstNode {
    readonly $container: GitGraph;
    readonly $type: 'Checkout' | 'EOL';
    branch: string;
}

declare const Checkout: {
    readonly $type: "Checkout";
    readonly branch: "branch";
};

declare interface CherryPicking extends langium_2.AstNode {
    readonly $container: GitGraph;
    readonly $type: 'CherryPicking' | 'EOL';
    id: string;
    parent: string;
    tags: Array<string>;
}

declare const CherryPicking: {
    readonly $type: "CherryPicking";
    readonly id: "id";
    readonly parent: "parent";
    readonly tags: "tags";
};

/**
 * This class wraps the embedded actions parser of chevrotain and exposes protected methods.
 * This way, we can build the `LangiumParser` as a composition.
 */
declare class ChevrotainWrapper extends EmbeddedActionsParser {
    definitionErrors: IParserDefinitionError[];
    constructor(tokens: TokenVocabulary, config: IParserConfig);
    get IS_RECORDING(): boolean;
    DEFINE_RULE(name: string, impl: RuleImpl, config?: IRuleConfig<any>): RuleResult;
    wrapSelfAnalysis(): void;
    wrapConsume(idx: number, tokenType: TokenType): IToken;
    wrapSubrule(idx: number, rule: RuleResult, args: Args): unknown;
    wrapOr(idx: number, choices: Array<IOrAlt<any>>): void;
    wrapOption(idx: number, callback: DSLMethodOpts<unknown>): void;
    wrapMany(idx: number, callback: DSLMethodOpts<unknown>): void;
    wrapAtLeastOne(idx: number, callback: DSLMethodOpts<unknown>): void;
    rule(rule: RuleResult): any;
}

declare interface ClassDefStatement extends langium_2.AstNode {
    readonly $type: 'ClassDefStatement';
    className: string;
    styleText: string;
}

declare const ClassDefStatement: {
    readonly $type: "ClassDefStatement";
    readonly className: "className";
    readonly styleText: "styleText";
};

/**
 * Defines the capabilities provided by the client.
 */
declare interface ClientCapabilities {
    /**
     * Workspace specific client capabilities.
     */
    workspace?: WorkspaceClientCapabilities;
    /**
     * Text document specific client capabilities.
     */
    textDocument?: TextDocumentClientCapabilities;
    /**
     * Capabilities specific to the notebook document support.
     *
     * @since 3.17.0
     */
    notebookDocument?: NotebookDocumentClientCapabilities;
    /**
     * Window specific client capabilities.
     */
    window?: WindowClientCapabilities;
    /**
     * General client capabilities.
     *
     * @since 3.16.0
     */
    general?: GeneralClientCapabilities;
    /**
     * Experimental client capabilities.
     */
    experimental?: LSPAny_2;
}

/**
 * The Client Capabilities of a {@link CodeActionRequest}.
 */
declare interface CodeActionClientCapabilities {
    /**
     * Whether code action supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * The client support code action literals of type `CodeAction` as a valid
     * response of the `textDocument/codeAction` request. If the property is not
     * set the request can only return `Command` literals.
     *
     * @since 3.8.0
     */
    codeActionLiteralSupport?: {
        /**
         * The code action kind is support with the following value
         * set.
         */
        codeActionKind: {
            /**
             * The code action kind values the client supports. When this
             * property exists the client also guarantees that it will
             * handle values outside its set gracefully and falls back
             * to a default value when unknown.
             */
            valueSet: CodeActionKind[];
        };
    };
    /**
     * Whether code action supports the `isPreferred` property.
     *
     * @since 3.15.0
     */
    isPreferredSupport?: boolean;
    /**
     * Whether code action supports the `disabled` property.
     *
     * @since 3.16.0
     */
    disabledSupport?: boolean;
    /**
     * Whether code action supports the `data` property which is
     * preserved between a `textDocument/codeAction` and a
     * `codeAction/resolve` request.
     *
     * @since 3.16.0
     */
    dataSupport?: boolean;
    /**
     * Whether the client supports resolving additional code action
     * properties via a separate `codeAction/resolve` request.
     *
     * @since 3.16.0
     */
    resolveSupport?: {
        /**
         * The properties that a client can resolve lazily.
         */
        properties: string[];
    };
    /**
     * Whether the client honors the change annotations in
     * text edits and resource operations returned via the
     * `CodeAction#edit` property by for example presenting
     * the workspace edit in the user interface and asking
     * for confirmation.
     *
     * @since 3.16.0
     */
    honorsChangeAnnotations?: boolean;
}

/**
 * The kind of a code action.
 *
 * Kinds are a hierarchical list of identifiers separated by `.`, e.g. `"refactor.extract.function"`.
 *
 * The set of kinds is open and client needs to announce the kinds it supports to the server during
 * initialization.
 */
declare type CodeActionKind = string;

/**
 * A set of predefined code action kinds
 */
declare namespace CodeActionKind {
    /**
     * Empty kind.
     */
    const Empty: '';
    /**
     * Base kind for quickfix actions: 'quickfix'
     */
    const QuickFix: 'quickfix';
    /**
     * Base kind for refactoring actions: 'refactor'
     */
    const Refactor: 'refactor';
    /**
     * Base kind for refactoring extraction actions: 'refactor.extract'
     *
     * Example extract actions:
     *
     * - Extract method
     * - Extract function
     * - Extract variable
     * - Extract interface from class
     * - ...
     */
    const RefactorExtract: 'refactor.extract';
    /**
     * Base kind for refactoring inline actions: 'refactor.inline'
     *
     * Example inline actions:
     *
     * - Inline function
     * - Inline variable
     * - Inline constant
     * - ...
     */
    const RefactorInline: 'refactor.inline';
    /**
     * Base kind for refactoring rewrite actions: 'refactor.rewrite'
     *
     * Example rewrite actions:
     *
     * - Convert JavaScript function to class
     * - Add or remove parameter
     * - Encapsulate field
     * - Make method static
     * - Move method to base class
     * - ...
     */
    const RefactorRewrite: 'refactor.rewrite';
    /**
     * Base kind for source actions: `source`
     *
     * Source code actions apply to the entire file.
     */
    const Source: 'source';
    /**
     * Base kind for an organize imports source action: `source.organizeImports`
     */
    const SourceOrganizeImports: 'source.organizeImports';
    /**
     * Base kind for auto-fix source actions: `source.fixAll`.
     *
     * Fix all actions automatically fix errors that have a clear fix that do not require user input.
     * They should not suppress errors or perform unsafe fixes such as generating new types or classes.
     *
     * @since 3.15.0
     */
    const SourceFixAll: 'source.fixAll';
}

/**
 * Structure to capture a description for an error code.
 *
 * @since 3.16.0
 */
declare interface CodeDescription {
    /**
     * An URI to open with more information about the diagnostic error.
     */
    href: URI_2;
}

/**
 * The CodeDescription namespace provides functions to deal with descriptions for diagnostic codes.
 *
 * @since 3.16.0
 */
declare namespace CodeDescription {
    function is(value: any): value is CodeDescription;
}

/**
 * The client capabilities  of a {@link CodeLensRequest}.
 */
declare interface CodeLensClientCapabilities {
    /**
     * Whether code lens supports dynamic registration.
     */
    dynamicRegistration?: boolean;
}

/**
 * @since 3.16.0
 */
declare interface CodeLensWorkspaceClientCapabilities {
    /**
     * Whether the client implementation supports a refresh request sent from the
     * server to the client.
     *
     * Note that this event is global and will force the client to refresh all
     * code lenses currently shown. It should be used with absolute care and is
     * useful for situation where a server for example detect a project wide
     * change that requires such a calculation.
     */
    refreshSupport?: boolean;
}

/**
 * Provides comments for AST nodes.
 */
declare interface CommentProvider {
    /**
     * Returns the comment associated with the specified AST node.
     * @param node The AST node to get the comment for.
     * @returns The comment associated with the specified AST node or `undefined` if there is no comment.
     */
    getComment(node: AstNode): string | undefined;
}

export declare interface Commit extends langium_2.AstNode {
    readonly $container: GitGraph;
    readonly $type: 'Commit' | 'EOL';
    id: string;
    message: string;
    tags: Array<string>;
    type: 'HIGHLIGHT' | 'NORMAL' | 'REVERSE';
}

export declare const Commit: {
    readonly $type: "Commit";
    readonly id: "id";
    readonly message: "message";
    readonly tags: "tags";
    readonly type: "type";
};

declare interface Common extends langium_2.AstNode {
    readonly $type: 'Common';
    accDescr?: string;
    accTitle?: string;
    title?: string;
}

declare const Common: {
    readonly $type: "Common";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly title: "title";
};

export declare class CommonTokenBuilder extends AbstractMermaidTokenBuilder {
}

export declare class CommonValueConverter extends AbstractMermaidValueConverter {
    protected runCustomConverter(_rule: GrammarAST.AbstractRule, _input: string, _cstNode: CstNode): ValueType | undefined;
}

declare function compareRange(range: Range_2, to: Range_2): RangeComparison;

/**
 * Completion client capabilities
 */
declare interface CompletionClientCapabilities {
    /**
     * Whether completion supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * The client supports the following `CompletionItem` specific
     * capabilities.
     */
    completionItem?: {
        /**
         * Client supports snippets as insert text.
         *
         * A snippet can define tab stops and placeholders with `$1`, `$2`
         * and `${3:foo}`. `$0` defines the final tab stop, it defaults to
         * the end of the snippet. Placeholders with equal identifiers are linked,
         * that is typing in one will update others too.
         */
        snippetSupport?: boolean;
        /**
         * Client supports commit characters on a completion item.
         */
        commitCharactersSupport?: boolean;
        /**
         * Client supports the following content formats for the documentation
         * property. The order describes the preferred format of the client.
         */
        documentationFormat?: MarkupKind[];
        /**
         * Client supports the deprecated property on a completion item.
         */
        deprecatedSupport?: boolean;
        /**
         * Client supports the preselect property on a completion item.
         */
        preselectSupport?: boolean;
        /**
         * Client supports the tag property on a completion item. Clients supporting
         * tags have to handle unknown tags gracefully. Clients especially need to
         * preserve unknown tags when sending a completion item back to the server in
         * a resolve call.
         *
         * @since 3.15.0
         */
        tagSupport?: {
            /**
             * The tags supported by the client.
             */
            valueSet: CompletionItemTag[];
        };
        /**
         * Client support insert replace edit to control different behavior if a
         * completion item is inserted in the text or should replace text.
         *
         * @since 3.16.0
         */
        insertReplaceSupport?: boolean;
        /**
         * Indicates which properties a client can resolve lazily on a completion
         * item. Before version 3.16.0 only the predefined properties `documentation`
         * and `details` could be resolved lazily.
         *
         * @since 3.16.0
         */
        resolveSupport?: {
            /**
             * The properties that a client can resolve lazily.
             */
            properties: string[];
        };
        /**
         * The client supports the `insertTextMode` property on
         * a completion item to override the whitespace handling mode
         * as defined by the client (see `insertTextMode`).
         *
         * @since 3.16.0
         */
        insertTextModeSupport?: {
            valueSet: InsertTextMode[];
        };
        /**
         * The client has support for completion item label
         * details (see also `CompletionItemLabelDetails`).
         *
         * @since 3.17.0
         */
        labelDetailsSupport?: boolean;
    };
    completionItemKind?: {
        /**
         * The completion item kind values the client supports. When this
         * property exists the client also guarantees that it will
         * handle values outside its set gracefully and falls back
         * to a default value when unknown.
         *
         * If this property is not present the client only supports
         * the completion items kinds from `Text` to `Reference` as defined in
         * the initial version of the protocol.
         */
        valueSet?: CompletionItemKind[];
    };
    /**
     * Defines how the client handles whitespace and indentation
     * when accepting a completion item that uses multi line
     * text in either `insertText` or `textEdit`.
     *
     * @since 3.17.0
     */
    insertTextMode?: InsertTextMode;
    /**
     * The client supports to send additional context information for a
     * `textDocument/completion` request.
     */
    contextSupport?: boolean;
    /**
     * The client supports the following `CompletionList` specific
     * capabilities.
     *
     * @since 3.17.0
     */
    completionList?: {
        /**
         * The client supports the following itemDefaults on
         * a completion list.
         *
         * The value lists the supported property names of the
         * `CompletionList.itemDefaults` object. If omitted
         * no properties are supported.
         *
         * @since 3.17.0
         */
        itemDefaults?: string[];
    };
}

/**
 * The kind of a completion entry.
 */
declare namespace CompletionItemKind {
    const Text: 1;
    const Method: 2;
    const Function: 3;
    const Constructor: 4;
    const Field: 5;
    const Variable: 6;
    const Class: 7;
    const Interface: 8;
    const Module: 9;
    const Property: 10;
    const Unit: 11;
    const Value: 12;
    const Enum: 13;
    const Keyword: 14;
    const Snippet: 15;
    const Color: 16;
    const File: 17;
    const Reference: 18;
    const Folder: 19;
    const EnumMember: 20;
    const Constant: 21;
    const Struct: 22;
    const Event: 23;
    const Operator: 24;
    const TypeParameter: 25;
}

declare type CompletionItemKind = 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12 | 13 | 14 | 15 | 16 | 17 | 18 | 19 | 20 | 21 | 22 | 23 | 24 | 25;

/**
 * Completion item tags are extra annotations that tweak the rendering of a completion
 * item.
 *
 * @since 3.15.0
 */
declare namespace CompletionItemTag {
    /**
     * Render a completion as obsolete, usually using a strike-out.
     */
    const Deprecated = 1;
}

declare type CompletionItemTag = 1;

declare interface CompletionParserResult {
    tokens: IToken[];
    elementStack: AbstractElement[];
    tokenIndex: number;
}

declare interface Component extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'Component' | 'EOL';
    decorator?: Decorator;
    evolution: number;
    inertia: boolean;
    label?: Label;
    name: string;
    visibility: number;
}

declare const Component: {
    readonly $type: "Component";
    readonly decorator: "decorator";
    readonly evolution: "evolution";
    readonly inertia: "inertia";
    readonly label: "label";
    readonly name: "name";
    readonly visibility: "visibility";
};

/**
 * A composite CST node contains other nodes, but no directly associated token.
 */
declare interface CompositeCstNode extends CstNode {
    readonly content: CstNode[];
}

declare class CompositeCstNodeImpl extends AbstractCstNode implements CompositeCstNode {
    readonly content: CstNode[];
    private _rangeCache?;
    get offset(): number;
    get length(): number;
    get end(): number;
    get range(): Range_2;
    private get firstNonHiddenNode();
    private get lastNonHiddenNode();
}

declare type Condition = BooleanLiteral | Conjunction | Disjunction | Negation | ParameterReference;

declare const Condition: {
    readonly $type: "Condition";
};

declare interface ConfigurationInitializedParams extends InitializedParams {
    register?: (params: DidChangeConfigurationRegistrationOptions) => void;
    fetchConfiguration?: (configuration: ConfigurationItem[]) => Promise<any>;
}

declare interface ConfigurationItem {
    /**
     * The scope to get the configuration section for.
     */
    scopeUri?: URI_3;
    /**
     * The configuration section asked for.
     */
    section?: string;
}

declare interface ConfigurationProvider {
    /**
     * A promise that resolves when the configuration provider is ready to be used.
     */
    readonly ready: Promise<void>;
    /**
     * When used in a language server context, this method is called when the server receives
     * the `initialize` request.
     */
    initialize(params: InitializeParams): void;
    /**
     * When used in a language server context, this method is called when the server receives
     * the `initialized` notification.
     */
    initialized(params: ConfigurationInitializedParams): Promise<void>;
    /**
     * Returns a configuration value stored for the given language.
     *
     * @param language The language id
     * @param configuration Configuration name
     */
    getConfiguration(language: string, configuration: string): Promise<any>;
    /**
     *  Updates the cached configurations using the `change` notification parameters.
     *
     * @param change The parameters of a change configuration notification.
     * `settings` property of the change object could be expressed as `Record<string, Record<string, any>>`
     */
    updateConfiguration(change: DidChangeConfigurationParams): void;
    /**
     * Get notified after a configuration section has been updated.
     */
    onConfigurationSectionUpdate(callback: ConfigurationSectionUpdateListener): Disposable_2;
}

declare interface ConfigurationSectionUpdate {
    /**
     * The name of the configuration section that has been updated.
     */
    section: string;
    /**
     * The updated configuration section.
     */
    configuration: any;
}

declare type ConfigurationSectionUpdateListener = (update: ConfigurationSectionUpdate) => void;

declare interface Conjunction extends langium.AstNode {
    readonly $container: Conjunction | Disjunction | Group | NamedArgument | Negation;
    readonly $type: 'Conjunction';
    left: Condition;
    right: Condition;
}

declare const Conjunction: {
    readonly $type: "Conjunction";
    readonly left: "left";
    readonly right: "right";
};

declare class ContextCache<Context, Key, Value, ContextKey = Context> extends DisposableCache {
    private readonly cache;
    private readonly converter;
    constructor(converter?: (input: Context) => ContextKey);
    has(contextKey: Context, key: Key): boolean;
    set(contextKey: Context, key: Key, value: Value): void;
    get(contextKey: Context, key: Key): Value | undefined;
    get(contextKey: Context, key: Key, provider: () => Value): Value;
    delete(contextKey: Context, key: Key): boolean;
    clear(): void;
    clear(contextKey: Context): void;
    protected cacheForContext(contextKey: Context): Map<Key, Value>;
}

declare type CoordinateValue = number;

/**
 * Creates a deep copy of the specified AST node.
 * The resulting copy will only contain semantically relevant information, such as the `$type` property and AST properties.
 *
 * @param node The AST node to deeply copy.
 * @param buildReference References are not copied, instead this function is called to rebuild them.
 * @param trace For the sake of tracking copied nodes and their originals a `trace` map can be provided (optional).
 */
declare function copyAstNode<T extends AstNode = AstNode>(node: T, buildReference: (node: AstNode, property: string, refNode: CstNode | undefined, refText: string, origReference: Reference<AstNode>) => Reference<AstNode>, trace?: Map<AstNode, AstNode>): T;

/**
 * Create the full set of services required by Langium.
 *
 * First inject the shared services by merging two modules:
 *  - Langium default shared services
 *  - Services generated by langium-cli
 *
 * Then inject the language-specific services by merging three modules:
 *  - Langium default language-specific services
 *  - Services generated by langium-cli
 *  - Services specified in this file
 * @param context - Optional module context with the LSP connection
 * @returns An object wrapping the shared services and the language-specific services
 */
export declare function createArchitectureServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Architecture: ArchitectureServices;
};

declare function createCompletionParser(services: LangiumCoreServices): LangiumCompletionParser;

export declare function createCynefinServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Cynefin: CynefinServices;
};

/**
 * Creates a dependency injection module configuring the default core services.
 * This is a set of services that are dedicated to a specific language.
 */
declare function createDefaultCoreModule(context: DefaultCoreModuleContext): Module<LangiumCoreServices, LangiumDefaultCoreServices>;

/**
 * Creates a dependency injection module configuring the default shared core services.
 * This is the set of services that are shared between multiple languages.
 */
declare function createDefaultSharedCoreModule(context: DefaultSharedCoreModuleContext): Module<LangiumSharedCoreServices, LangiumDefaultSharedCoreServices>;

export declare function createEventModelingServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    EventModel: EventModelingServices;
};

export declare function createGitGraphServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    GitGraph: GitGraphServices;
};

/**
 * Create the default grammar configuration (used by `createDefaultModule`). This can be overridden in a
 * language-specific module.
 */
declare function createGrammarConfig(services: LangiumCoreServices): GrammarConfig;

/**
 * Create the full set of services required by Langium.
 *
 * First inject the shared services by merging two modules:
 *  - Langium default shared services
 *  - Services generated by langium-cli
 *
 * Then inject the language-specific services by merging three modules:
 *  - Langium default language-specific services
 *  - Services generated by langium-cli
 *  - Services specified in this file
 * @param context - Optional module context with the LSP connection
 * @returns An object wrapping the shared services and the language-specific services
 */
export declare function createInfoServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Info: InfoServices;
};

/**
 * Create and finalize a Langium parser. The parser rules are derived from the grammar, which is
 * available at `services.Grammar`.
 */
declare function createLangiumParser(services: LangiumCoreServices): LangiumParser;

/**
 * Create the full set of services required by Langium.
 *
 * First inject the shared services by merging two modules:
 *  - Langium default shared services
 *  - Services generated by langium-cli
 *
 * Then inject the language-specific services by merging three modules:
 *  - Langium default language-specific services
 *  - Services generated by langium-cli
 *  - Services specified in this file
 * @param context - Optional module context with the LSP connection
 * @returns An object wrapping the shared services and the language-specific services
 */
export declare function createPacketServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Packet: PacketServices;
};

declare function createParser<T extends BaseParser>(grammar: Grammar, parser: T, tokens: TokenTypeDictionary): T;

/**
 * Create the full set of services required by Langium.
 *
 * First inject the shared services by merging two modules:
 *  - Langium default shared services
 *  - Services generated by langium-cli
 *
 * Then inject the language-specific services by merging three modules:
 *  - Langium default language-specific services
 *  - Services generated by langium-cli
 *  - Services specified in this file
 * @param context - Optional module context with the LSP connection
 * @returns An object wrapping the shared services and the language-specific services
 */
export declare function createPieServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Pie: PieServices;
};

/**
 * Create the full set of services required by Langium.
 *
 * First inject the shared services by merging two modules:
 *  - Langium default shared services
 *  - Services generated by langium-cli
 *
 * Then inject the language-specific services by merging three modules:
 *  - Langium default language-specific services
 *  - Services generated by langium-cli
 *  - Services specified in this file
 * @param context - Optional module context with the LSP connection
 * @returns An object wrapping the shared services and the language-specific services
 */
export declare function createRadarServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Radar: RadarServices;
};

export declare function createRailroadAbnfServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    RailroadAbnf: RailroadAbnfServices;
};

export declare function createRailroadEbnfServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    RailroadEbnf: RailroadEbnfServices;
};

export declare function createRailroadPegServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    RailroadPeg: RailroadPegServices;
};

export declare function createRailroadServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Railroad: RailroadServices;
};

/**
 * Create the full set of services required by Langium.
 *
 * First inject the shared services by merging two modules:
 *  - Langium default shared services
 *  - Services generated by langium-cli
 *
 * Then inject the language-specific services by merging three modules:
 *  - Langium default language-specific services
 *  - Services generated by langium-cli
 *  - Services specified in this file
 * @param context - Optional module context with the LSP connection
 * @returns An object wrapping the shared services and the language-specific services
 */
export declare function createTreemapServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Treemap: TreemapServices;
};

/**
 * Create the full set of services required by Langium.
 *
 * First inject the shared services by merging two modules:
 *  - Langium default shared services
 *  - Services generated by langium-cli
 *
 * Then inject the language-specific services by merging three modules:
 *  - Langium default language-specific services
 *  - Services generated by langium-cli
 *  - Services specified in this file
 * @param context - Optional module context with the LSP connection
 * @returns An object wrapping the shared services and the language-specific services
 */
export declare function createTreeViewServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    TreeView: TreeViewServices;
};

/**
 * Create the full set of services required by Langium.
 *
 * First inject the shared services by merging two modules:
 *  - Langium default shared services
 *  - Services generated by langium-cli
 *
 * Then inject the language-specific services by merging three modules:
 *  - Langium default language-specific services
 *  - Services generated by langium-cli
 *  - Services specified in this file
 * @param context - Optional module context with the LSP connection
 * @returns An object wrapping the shared services and the language-specific services
 */
export declare function createWardleyServices(context?: DefaultSharedCoreModuleContext): {
    shared: LangiumSharedCoreServices;
    Wardley: WardleyServices;
};

declare interface CrossReference extends AbstractElement {
    readonly $type: 'CrossReference';
    deprecatedSyntax: boolean;
    isMulti: boolean;
    terminal?: AbstractElement;
    type: langium.Reference<AbstractType>;
}

declare const CrossReference: {
    readonly $type: "CrossReference";
    readonly cardinality: "cardinality";
    readonly deprecatedSyntax: "deprecatedSyntax";
    readonly isMulti: "isMulti";
    readonly terminal: "terminal";
    readonly type: "type";
};

/**
 * Describes a union type including only names(!) of the cross-reference properties of the given AstNode type.
 * Enhances compile-time validation of cross-reference distinctions, e.g. in scope providers
 *  in combination with `assertUnreachable(context.property)`.
 */
declare type CrossReferencesOfAstNodeType<N extends AstNode> = SingleCrossReferencesOfAstNodeType<N> | MultiCrossReferencesOfAstNodeType<N>;

/**
 * A node in the Concrete Syntax Tree (CST).
 */
declare interface CstNode extends DocumentSegment {
    /** The container node in the CST */
    readonly container?: CompositeCstNode;
    /** The actual text */
    readonly text: string;
    /** The root CST node */
    readonly root: RootCstNode;
    /** The grammar element from which this node was parsed */
    readonly grammarSource?: AbstractElement;
    /** The AST node created from this CST node */
    readonly astNode: AstNode;
    /** Whether the token is hidden, i.e. not explicitly part of the containing grammar rule */
    readonly hidden: boolean;
}

declare class CstNodeBuilder {
    private rootNode;
    private nodeStack;
    get current(): CompositeCstNodeImpl;
    buildRootNode(input: string): RootCstNode;
    buildCompositeNode(feature: AbstractElement): CompositeCstNode;
    buildLeafNode(token: IToken, feature?: AbstractElement): LeafCstNode;
    removeNode(node: CstNode): void;
    addHiddenNodes(tokens: IToken[]): void;
    construct(item: {
        $type: string | symbol | undefined;
        $cstNode: CstNode;
        $infix?: boolean;
    }): void;
}

declare namespace CstUtils {
    export {
        getDatatypeNode,
        streamCst,
        flattenCst,
        isChildNode,
        tokenToRange,
        toDocumentSegment,
        compareRange,
        inRange,
        findDeclarationNodeAtOffset,
        findCommentNode,
        isCommentNode,
        findLeafNodeAtOffset,
        findLeafNodeBeforeOffset,
        getPreviousNode,
        getNextNode,
        getStartlineNode,
        getInteriorNodes,
        RangeComparison,
        DefaultNameRegexp
    }
}

declare interface Curve extends langium_2.AstNode {
    readonly $container: Radar;
    readonly $type: 'Curve';
    entries: Array<Entry>;
    label?: string;
    name: string;
}

declare const Curve: {
    readonly $type: "Curve";
    readonly entries: "entries";
    readonly label: "label";
    readonly name: "name";
};

export declare interface Cynefin extends langium_2.AstNode {
    readonly $type: 'Cynefin';
    accDescr?: string;
    accTitle?: string;
    domains: Array<DomainBlock>;
    title?: string;
    transitions: Array<Transition>;
}

export declare const Cynefin: {
    readonly $type: "Cynefin";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly domains: "domains";
    readonly title: "title";
    readonly transitions: "transitions";
};

declare interface CynefinAddedServices {
    parser: {
        TokenBuilder: CynefinTokenBuilder;
        ValueConverter: CommonValueConverter;
    };
}

export declare const CynefinGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'CynefinGrammar' language. */
declare namespace CynefinGrammar {
    const Terminals: {
        DOMAIN_NAME: RegExp;
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        STRING: RegExp;
        NEWLINE: RegExp;
        WHITESPACE: RegExp;
        YAML: RegExp;
        DIRECTIVE: RegExp;
        SINGLE_LINE_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "-->" | ":" | "cynefin-beta" | "cynefin-beta:";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Cynefin: Cynefin;
        DomainBlock: DomainBlock;
        DomainItem: DomainItem;
        Transition: Transition;
    };
}

export declare const CynefinModule: Module<CynefinServices, PartialLangiumCoreServices & CynefinAddedServices>;

export declare type CynefinServices = LangiumCoreServices & CynefinAddedServices;

declare class CynefinTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare const DatatypeSymbol: unique symbol;

declare interface Deaccelerator extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'Deaccelerator' | 'EOL';
    name: string;
    x: number;
    y: number;
}

declare const Deaccelerator: {
    readonly $type: "Deaccelerator";
    readonly name: "name";
    readonly x: "x";
    readonly y: "y";
};

/**
 * @since 3.14.0
 */
declare interface DeclarationClientCapabilities {
    /**
     * Whether declaration supports dynamic registration. If this is set to `true`
     * the client supports the new `DeclarationRegistrationOptions` return value
     * for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
    /**
     * The client supports additional metadata in the form of declaration links.
     */
    linkSupport?: boolean;
}

declare interface Decorator extends langium_2.AstNode {
    readonly $container: Component;
    readonly $type: 'Decorator';
    strategy: string;
}

declare const Decorator: {
    readonly $type: "Decorator";
    readonly strategy: "strategy";
};

/**
 * A deep partial type definition for services. We look into T to see whether its type definition contains
 * any methods. If it does, it's one of our services and therefore should not be partialized.
 */
declare type DeepPartial<T> = T[keyof T] extends Function ? T : {
    [P in keyof T]?: DeepPartial<T[P]>;
};

/**
 * Recursively makes all properties of an AstNode optional, except for those
 * that start with a dollar sign ($) or are of type boolean or are of type array.
 * If the type is a Reference or an Array, it applies the transformation recursively
 * to the inner type.
 * Otherwise the type is returned as is.
 *
 * @template T - The type to be transformed.
 */
declare type DeepPartialAstNode<T> = T extends Reference<infer U extends AstNode> ? Reference<DeepPartialAstNode<U>> : T extends AstNode ? {
    [K in keyof T as K extends `$${string}` | (T[K] extends (boolean | any[]) ? K : never) ? K : never]: DeepPartialAstNode<T[K]>;
} & {
    [K in keyof T as K extends `$${string}` ? never : T[K] extends (boolean | any[]) ? never : K]?: DeepPartialAstNode<T[K]>;
} : T extends Array<infer U> ? Array<DeepPartialAstNode<U>> : T;

declare const DEFAULT_TOKENIZE_OPTIONS: TokenizeOptions;

declare class DefaultAstNodeDescriptionProvider implements AstNodeDescriptionProvider {
    protected readonly astNodeLocator: AstNodeLocator;
    protected readonly nameProvider: NameProvider;
    constructor(services: LangiumCoreServices);
    createDescription(node: AstNode, name: string | undefined, document?: LangiumDocument): AstNodeDescription;
}

declare class DefaultAstNodeLocator implements AstNodeLocator {
    protected segmentSeparator: string;
    protected indexSeparator: string;
    getAstNodePath(node: AstNode): string;
    protected getPathSegment({ $containerProperty, $containerIndex }: AstNode): string;
    getAstNode<T extends AstNode = AstNode>(node: AstNode, path: string): T | undefined;
}

/**
 * Default implementation of the async parser which simply wraps the sync parser in a promise.
 *
 * @remarks
 * A real implementation would create worker threads or web workers to offload the parsing work.
 */
declare class DefaultAsyncParser implements AsyncParser {
    protected readonly syncParser: LangiumParser;
    constructor(services: LangiumCoreServices);
    parse<T extends AstNode>(text: string, _cancelToken: CancellationToken): Promise<ParseResult<T>>;
}

declare class DefaultCommentProvider implements CommentProvider {
    protected readonly grammarConfig: () => GrammarConfig;
    constructor(services: LangiumCoreServices);
    getComment(node: AstNode): string | undefined;
}

/**
 * Base configuration provider for building up other configuration providers
 */
declare class DefaultConfigurationProvider implements ConfigurationProvider {
    protected readonly serviceRegistry: ServiceRegistry;
    protected readonly _ready: Deferred<void>;
    protected readonly onConfigurationSectionUpdateEmitter: Emitter<ConfigurationSectionUpdate>;
    protected settings: Record<string, Record<string, any>>;
    protected workspaceConfig: boolean;
    constructor(services: LangiumSharedCoreServices);
    get ready(): Promise<void>;
    initialize(params: InitializeParams): void;
    initialized(params: ConfigurationInitializedParams): Promise<void>;
    /**
     *  Updates the cached configurations using the `change` notification parameters.
     *
     * @param change The parameters of a change configuration notification.
     * `settings` property of the change object could be expressed as `Record<string, Record<string, any>>`
     */
    updateConfiguration(change: DidChangeConfigurationParams): void;
    protected updateSectionConfiguration(section: string, configuration: any): void;
    /**
     * Returns a configuration value stored for the given language.
     *
     * @param language The language id
     * @param configuration Configuration name
     */
    getConfiguration(language: string, configuration: string): Promise<any>;
    protected toSectionName(languageId: string): string;
    get onConfigurationSectionUpdate(): Event_2<ConfigurationSectionUpdate>;
}

/**
 * Context required for creating the default language-specific dependency injection module.
 */
declare interface DefaultCoreModuleContext {
    shared: LangiumSharedCoreServices;
}

declare class DefaultDocumentBuilder implements DocumentBuilder {
    updateBuildOptions: BuildOptions;
    protected readonly langiumDocuments: LangiumDocuments;
    protected readonly langiumDocumentFactory: LangiumDocumentFactory;
    protected readonly textDocuments: TextDocumentProvider | undefined;
    protected readonly indexManager: IndexManager;
    protected readonly fileSystemProvider: FileSystemProvider;
    protected readonly workspaceManager: () => WorkspaceManager;
    protected readonly serviceRegistry: ServiceRegistry;
    protected readonly updateListeners: DocumentUpdateListener[];
    protected readonly buildPhaseListeners: MultiMap<DocumentState, DocumentBuildListener>;
    protected readonly documentPhaseListeners: MultiMap<DocumentState, DocumentPhaseListener>;
    protected readonly buildState: Map<string, DocumentBuildState>;
    protected readonly documentBuildWaiters: Map<string, Deferred<void>>;
    protected currentState: DocumentState;
    constructor(services: LangiumSharedCoreServices);
    build<T extends AstNode>(documents: Array<LangiumDocument<T>>, options?: BuildOptions, cancelToken?: CancellationToken): Promise<void>;
    update(changed: URI[], deleted: URI[], cancelToken?: CancellationToken): Promise<void>;
    protected resultsAreIncomplete(document: LangiumDocument, options: BuildOptions | undefined): boolean;
    protected findMissingValidationCategories(document: LangiumDocument, options: BuildOptions | undefined): ValidationCategory[];
    protected findChangedUris(changed: URI): Promise<URI[]>;
    protected emitUpdate(changed: URI[], deleted: URI[]): Promise<void>;
    /**
     * Sort the given documents by priority. By default, documents with an open text document are prioritized.
     * This is useful to ensure that visible documents show their diagnostics before all other documents.
     *
     * This improves the responsiveness in large workspaces as users usually don't care about diagnostics
     * in files that are currently not opened in the editor.
     */
    protected sortDocuments(documents: LangiumDocument[]): LangiumDocument[];
    private hasTextDocument;
    /**
     * Check whether the given document should be relinked after changes were found in the given URIs.
     */
    protected shouldRelink(document: LangiumDocument, changedUris: Set<string>): boolean;
    onUpdate(callback: DocumentUpdateListener): Disposable_3;
    resetToState<T extends AstNode>(document: LangiumDocument<T>, state: DocumentState): void;
    protected cleanUpDeleted<T extends AstNode>(document: LangiumDocument<T>): void;
    /**
     * Build the given documents by stepping through all build phases. If a document's state indicates
     * that a certain build phase is already done, the phase is skipped for that document.
     *
     * @param documents The documents to build.
     * @param options the {@link BuildOptions} to use.
     * @param cancelToken A cancellation token that can be used to cancel the build.
     * @returns A promise that resolves when the build is done.
     */
    protected buildDocuments(documents: LangiumDocument[], options: BuildOptions, cancelToken: CancellationToken): Promise<void>;
    protected markAsCompleted(document: LangiumDocument): void;
    /**
     * Runs prior to beginning the build process to update the {@link DocumentBuildState} for each document
     *
     * @param documents collection of documents to be built
     * @param options the {@link BuildOptions} to use
     */
    protected prepareBuild(documents: LangiumDocument[], options: BuildOptions): void;
    /**
     * Runs a cancelable operation on a set of documents to bring them to a specified {@link DocumentState}.
     *
     * @param documents The array of documents to process.
     * @param targetState The target {@link DocumentState} to bring the documents to.
     * @param cancelToken A token that can be used to cancel the operation.
     * @param callback A function to be called for each document.
     * @returns A promise that resolves when all documents have been processed or the operation is canceled.
     * @throws Will throw `OperationCancelled` if the operation is canceled via a `CancellationToken`.
     */
    protected runCancelable(documents: LangiumDocument[], targetState: DocumentState, cancelToken: CancellationToken, callback: (document: LangiumDocument) => MaybePromise<unknown>): Promise<void>;
    onBuildPhase(targetState: DocumentState, callback: DocumentBuildListener): Disposable_3;
    onDocumentPhase(targetState: DocumentState, callback: DocumentPhaseListener): Disposable_3;
    waitUntil(state: DocumentState, cancelToken?: CancellationToken): Promise<void>;
    waitUntil(state: DocumentState, uri?: URI, cancelToken?: CancellationToken): Promise<URI>;
    protected awaitDocumentState(state: DocumentState, uri: URI, cancelToken: CancellationToken): Promise<URI>;
    protected awaitBuilderState(state: DocumentState, cancelToken: CancellationToken): Promise<void>;
    protected notifyDocumentPhase(document: LangiumDocument, state: DocumentState, cancelToken: CancellationToken): Promise<void>;
    protected notifyBuildPhase(documents: LangiumDocument[], state: DocumentState, cancelToken: CancellationToken): Promise<void>;
    /**
     * Determine whether the given document should be linked during a build. The default
     * implementation checks the `eagerLinking` property of the build options. If it's set to `true`
     * or `undefined`, the document is included in the linking phase. This also affects the
     * references indexing phase, which depends on eager linking.
     */
    protected shouldLink(document: LangiumDocument): boolean;
    /**
     * Determine whether the given document should be validated during a build. The default
     * implementation checks the `validation` property of the build options. If it's set to `true`
     * or a `ValidationOptions` object, the document is included in the validation phase.
     */
    protected shouldValidate(document: LangiumDocument): boolean;
    /**
     * Run validation checks on the given document and store the resulting diagnostics in the document.
     * If the document already contains diagnostics, the new ones are added to the list.
     */
    protected validate(document: LangiumDocument, cancelToken: CancellationToken): Promise<void>;
    protected getBuildOptions(document: LangiumDocument): BuildOptions;
}

declare class DefaultDocumentValidator implements DocumentValidator {
    protected readonly validationRegistry: ValidationRegistry;
    protected readonly metadata: LanguageMetaData;
    protected readonly profiler: LangiumProfiler | undefined;
    protected readonly languageId: string;
    constructor(services: LangiumCoreServices);
    validateDocument(document: LangiumDocument, options?: ValidationOptions, cancelToken?: CancellationToken): Promise<Diagnostic[]>;
    protected processLexingErrors(parseResult: ParseResult, diagnostics: Diagnostic[], _options: ValidationOptions): void;
    protected processParsingErrors(parseResult: ParseResult, diagnostics: Diagnostic[], _options: ValidationOptions): void;
    protected processLinkingErrors(document: LangiumDocument, diagnostics: Diagnostic[], _options: ValidationOptions): void;
    protected validateAst(rootNode: AstNode, options: ValidationOptions, cancelToken?: CancellationToken): Promise<Diagnostic[]>;
    protected validateAstBefore(rootNode: AstNode, options: ValidationOptions, acceptor: ValidationAcceptor, cancelToken?: CancellationToken): Promise<void>;
    protected validateAstNodes(rootNode: AstNode, options: ValidationOptions, acceptor: ValidationAcceptor, cancelToken?: CancellationToken): Promise<void>;
    protected validateSingleNodeOptions(_node: AstNode, _options: ValidationOptions): ValidateSingleNodeOptions;
    protected validateAstAfter(rootNode: AstNode, options: ValidationOptions, acceptor: ValidationAcceptor, cancelToken?: CancellationToken): Promise<void>;
    protected toDiagnostic<N extends AstNode>(severity: ValidationSeverity, message: string, info: DiagnosticInfo<N, string>): Diagnostic;
    protected getSource(): string | undefined;
}

declare class DefaultHydrator implements Hydrator {
    protected readonly grammar: Grammar;
    protected readonly lexer: Lexer;
    protected readonly linker: Linker;
    protected readonly grammarElementIdMap: BiMap<AbstractElement, number>;
    protected readonly tokenTypeIdMap: BiMap<number, TokenType>;
    constructor(services: LangiumCoreServices);
    dehydrate(result: ParseResult<AstNode>): ParseResult<object>;
    protected dehydrateLexerReport(lexerReport: LexingReport): LexingReport;
    protected createDehyrationContext(node: AstNode): DehydrateContext;
    protected dehydrateAstNode(node: AstNode, context: DehydrateContext): object;
    protected dehydrateReference(reference: Reference, context: DehydrateContext): any;
    protected dehydrateCstNode(node: CstNode, context: DehydrateContext): any;
    hydrate<T extends AstNode = AstNode>(result: ParseResult<object>): ParseResult<T>;
    protected createHydrationContext(node: any): HydrateContext;
    protected hydrateAstNode(node: any, context: HydrateContext): AstNode;
    protected setParent(node: any, parent: any): any;
    protected hydrateReference(reference: any, node: AstNode, name: string, context: HydrateContext): Reference;
    protected hydrateCstNode(cstNode: any, context: HydrateContext, num?: number): CstNode;
    protected hydrateCstLeafNode(cstNode: any): LeafCstNode;
    protected getTokenType(name: string): TokenType;
    protected getGrammarElementId(node: AbstractElement | undefined): number | undefined;
    protected getGrammarElement(id: number): AbstractElement | undefined;
    protected createGrammarElementIdMap(): void;
}

declare class DefaultIndexManager implements IndexManager {
    protected readonly serviceRegistry: ServiceRegistry;
    protected readonly documents: LangiumDocuments;
    protected readonly astReflection: AstReflection;
    /**
     * The symbol index stores all `AstNodeDescription` items exported by a document.
     * The key used in this map is the string representation of the specific document URI.
     */
    protected readonly symbolIndex: Map<string, AstNodeDescription[]>;
    /**
     * This is a cache for the `allElements()` method.
     * It caches the descriptions from `symbolIndex` grouped by types.
     */
    protected readonly symbolByTypeIndex: ContextCache<string, string, AstNodeDescription[], string>;
    /**
     * This index keeps track of all `ReferenceDescription` items exported by a document.
     * This is used to compute which elements are affected by a document change
     * and for finding references to an AST node.
     */
    protected readonly referenceIndex: Map<string, ReferenceDescription[]>;
    constructor(services: LangiumSharedCoreServices);
    findAllReferences(targetNode: AstNode, astNodePath: string): Stream<ReferenceDescription>;
    allElements(nodeType?: string, uris?: Set<string>): Stream<AstNodeDescription>;
    protected getFileDescriptions(uri: string, nodeType?: string): AstNodeDescription[];
    remove(uri: URI): void;
    removeContent(uri: URI): void;
    removeReferences(uri: URI): void;
    updateContent(document: LangiumDocument, cancelToken?: CancellationToken): Promise<void>;
    updateReferences(document: LangiumDocument, cancelToken?: CancellationToken): Promise<void>;
    isAffected(document: LangiumDocument, changedUris: Set<string>): boolean;
}

declare class DefaultJsonSerializer implements JsonSerializer {
    /** The set of AstNode properties to be ignored by the serializer. */
    ignoreProperties: Set<string>;
    /** The document that is currently processed by the serializer; this is used by the replacer function.  */
    protected currentDocument: LangiumDocument | undefined;
    protected readonly langiumDocuments: LangiumDocuments;
    protected readonly astNodeLocator: AstNodeLocator;
    protected readonly nameProvider: NameProvider;
    protected readonly commentProvider: CommentProvider;
    constructor(services: LangiumCoreServices);
    serialize(node: AstNode, options?: JsonSerializeOptions): string;
    deserialize<T extends AstNode = AstNode>(content: string, options?: JsonDeserializeOptions): T;
    protected replacer(key: string, value: unknown, { refText, sourceText, textRegions, comments, uriConverter }: JsonSerializeOptions): unknown;
    protected addAstNodeRegionWithAssignmentsTo(node: AstNodeWithTextRegion): AstNodeWithTextRegion | undefined;
    protected linkNode(node: GenericAstNode, root: AstNode, options: JsonDeserializeOptions, container?: AstNode, containerProperty?: string, containerIndex?: number): void;
    protected reviveReference(container: AstNode, property: string, root: AstNode, reference: IntermediateReference, options: JsonDeserializeOptions): Reference | MultiReference | undefined;
    protected getRefNode(root: AstNode, uri: string, uriConverter?: (uri: string) => URI): AstNode | string;
}

declare class DefaultLangiumDocumentFactory implements LangiumDocumentFactory {
    protected readonly serviceRegistry: ServiceRegistry;
    protected readonly textDocuments?: TextDocumentProvider;
    protected readonly fileSystemProvider: FileSystemProvider;
    constructor(services: LangiumSharedCoreServices);
    fromUri<T extends AstNode = AstNode>(uri: URI, cancellationToken?: CancellationToken): Promise<LangiumDocument<T>>;
    fromTextDocument<T extends AstNode = AstNode>(textDocument: TextDocument, uri?: URI, options?: ParserOptions): LangiumDocument<T>;
    fromTextDocument<T extends AstNode = AstNode>(textDocument: TextDocument, uri: URI | undefined, cancellationToken: CancellationToken): Promise<LangiumDocument<T>>;
    fromString<T extends AstNode = AstNode>(text: string, uri: URI, options?: ParserOptions): LangiumDocument<T>;
    fromString<T extends AstNode = AstNode>(text: string, uri: URI, cancellationToken: CancellationToken): Promise<LangiumDocument<T>>;
    fromModel<T extends AstNode = AstNode>(model: T, uri: URI): LangiumDocument<T>;
    protected create<T extends AstNode = AstNode>(uri: URI, content: string | TextDocument | {
        $model: T;
    }, options?: ParserOptions): LangiumDocument<T>;
    protected createAsync<T extends AstNode = AstNode>(uri: URI, content: string | TextDocument, cancelToken: CancellationToken): Promise<LangiumDocument<T>>;
    /**
     * Create a LangiumDocument from a given parse result.
     *
     * A TextDocument is created on demand if it is not provided as argument here. Usually this
     * should not be necessary because the main purpose of the TextDocument is to convert between
     * text ranges and offsets, which is done solely in LSP request handling.
     *
     * With the introduction of {@link update} below this method is supposed to be mainly called
     * during workspace initialization and on addition/recognition of new files, while changes in
     * existing documents are processed via {@link update}.
     */
    protected createLangiumDocument<T extends AstNode = AstNode>(parseResult: ParseResult<T>, uri: URI, textDocument?: TextDocument, text?: string): LangiumDocument<T>;
    update<T extends AstNode = AstNode>(document: Mutable<LangiumDocument<T>>, cancellationToken: CancellationToken): Promise<LangiumDocument<T>>;
    protected parse<T extends AstNode>(uri: URI, text: string, options?: ParserOptions): ParseResult<T>;
    protected parseAsync<T extends AstNode>(uri: URI, text: string, cancellationToken: CancellationToken): Promise<ParseResult<T>>;
    protected createTextDocumentGetter(uri: URI, text?: string): () => TextDocument;
}

declare class DefaultLangiumDocuments implements LangiumDocuments {
    protected readonly services: LangiumSharedCoreServices;
    protected readonly langiumDocumentFactory: LangiumDocumentFactory;
    private documentBuilder;
    protected readonly documentTrie: UriTrie<LangiumDocument<AstNode>>;
    constructor(services: LangiumSharedCoreServices);
    get all(): Stream<LangiumDocument>;
    addDocument(document: LangiumDocument): void;
    getDocument(uri: URI): LangiumDocument | undefined;
    getDocuments(folder: URI): LangiumDocument[];
    getOrCreateDocument(uri: URI, cancellationToken?: CancellationToken): Promise<LangiumDocument>;
    createDocument(uri: URI, text: string): LangiumDocument;
    createDocument(uri: URI, text: string, cancellationToken: CancellationToken): Promise<LangiumDocument>;
    hasDocument(uri: URI): boolean;
    /**
     * @deprecated Since 4.2 use `DocumentBuilder.resetToState(DocumentState.Changed)` instead
     * TODO remove this for the next major release
     */
    invalidateDocument(uri: URI): LangiumDocument | undefined;
    deleteDocument(uri: URI): LangiumDocument | undefined;
    deleteDocuments(folder: URI): LangiumDocument[];
}

declare class DefaultLangiumProfiler implements LangiumProfiler {
    protected activeCategories: Set<ProfilingCategory>;
    protected readonly allCategories: ReadonlySet<ProfilingCategory>;
    protected readonly records: MultiMap<string, ProfilingRecord>;
    constructor(activeCategories?: Set<ProfilingCategory>);
    isActive(category: ProfilingCategory): boolean;
    start(...categories: ProfilingCategory[]): void;
    stop(...categories: ProfilingCategory[]): void;
    createTask(category: ProfilingCategory, taskId: string): ProfilingTask;
    protected dumpRecord(category: string, record: ProfilingRecord): ProfilingRecord;
    getRecords(...categories: ProfilingCategory[]): Stream<ProfilingRecord>;
}

declare class DefaultLexer implements Lexer {
    protected readonly tokenBuilder: TokenBuilder;
    protected readonly errorMessageProvider: ILexerErrorMessageProvider;
    protected tokenTypes: TokenTypeDictionary;
    protected chevrotainLexer: Lexer_2;
    constructor(services: LangiumCoreServices);
    get definition(): TokenTypeDictionary;
    tokenize(text: string, _options?: TokenizeOptions): LexerResult;
    protected toTokenTypeDictionary(buildTokens: TokenVocabulary): TokenTypeDictionary;
}

declare class DefaultLexerErrorMessageProvider implements ILexerErrorMessageProvider {
    buildUnexpectedCharactersMessage(fullText: string, startOffset: number, length: number, line?: number, column?: number): string;
    buildUnableToPopLexerModeMessage(token: IToken): string;
}

declare class DefaultLinker implements Linker {
    protected readonly reflection: AstReflection;
    protected readonly scopeProvider: ScopeProvider;
    protected readonly astNodeLocator: AstNodeLocator;
    protected readonly langiumDocuments: () => LangiumDocuments;
    protected readonly profiler: LangiumProfiler | undefined;
    protected readonly languageId: string;
    constructor(services: LangiumCoreServices);
    link(document: LangiumDocument, cancelToken?: CancellationToken): Promise<void>;
    protected doLink(refInfo: ReferenceInfo, document: LangiumDocument): void;
    unlink(document: LangiumDocument): void;
    getCandidate(refInfo: ReferenceInfo): AstNodeDescription | LinkingError;
    getCandidates(refInfo: ReferenceInfo): AstNodeDescription[] | LinkingError;
    buildReference(node: AstNode, property: string, refNode: CstNode | undefined, refText: string): Reference;
    buildMultiReference(node: AstNode, property: string, refNode: CstNode | undefined, refText: string): MultiReference;
    protected throwCyclicReferenceError(node: AstNode, property: string, refText: string): never;
    protected getLinkedNode(refInfo: ReferenceInfo): {
        node?: AstNode;
        descr?: AstNodeDescription;
        error?: LinkingError;
    };
    protected loadAstNode(nodeDescription: AstNodeDescription): AstNode | undefined;
    protected createLinkingError(refInfo: ReferenceInfo, targetDescription?: AstNodeDescription): LinkingError;
}

declare interface DefaultMultiReference extends MultiReference {
    _items: MultiReferenceItem[] | typeof RefResolving | undefined;
    _linkingError?: LinkingError;
}

declare class DefaultNameProvider implements NameProvider {
    getName(node: AstNode): string | undefined;
    getNameNode(node: AstNode): CstNode | undefined;
}

declare const DefaultNameRegexp: RegExp;

declare interface DefaultReference extends Reference {
    _ref?: AstNode | LinkingError | typeof RefResolving;
    _nodeDescription?: AstNodeDescription;
}

declare class DefaultReferenceDescriptionProvider implements ReferenceDescriptionProvider {
    protected readonly nodeLocator: AstNodeLocator;
    constructor(services: LangiumCoreServices);
    createDescriptions(document: LangiumDocument, cancelToken?: CancellationToken): Promise<ReferenceDescription[]>;
    protected createInfoDescriptions(refInfo: ReferenceInfo): ReferenceDescription[];
}

declare class DefaultReferences implements References {
    protected readonly nameProvider: NameProvider;
    protected readonly index: IndexManager;
    protected readonly nodeLocator: AstNodeLocator;
    protected readonly documents: LangiumDocuments;
    protected hasMultiReference: boolean;
    constructor(services: LangiumCoreServices);
    findDeclarations(sourceCstNode: CstNode): AstNode[];
    /**
     * Returns all self-references for the specified node.
     * Since the node can be part of a multi-reference, this method returns all nodes that are part of the same multi-reference.
     */
    protected getSelfNodes(node: AstNode): AstNode[];
    protected getNodeFromReferenceDescription(ref?: ReferenceDescription): AstNode | undefined;
    findDeclarationNodes(sourceCstNode: CstNode): CstNode[];
    findReferences(targetNode: AstNode, options: FindReferencesOptions): Stream<ReferenceDescription>;
    protected getSelfReferences(targetNode: AstNode): ReferenceDescription[];
}

/**
 * The default scope computation creates and collects descriptions of the AST nodes to be exported into the
 * _global_ scope from the given document. By default those are the document's root AST node and its directly
 * contained child nodes.
 *
 * Besides, it gathers all AST nodes that have a name (according to the `NameProvider` service) and that are to be
 * included in the local scope of their particular container nodes. They are collected in a `DocumentSymbols` table.
 * As a result, for every cross-reference in the AST, target elements from the same level (siblings) and further up
 * towards the root (parents and siblings of parents) are visible.
 * Elements being nested inside lower levels (children, children of siblings and parents' siblings)
 * are _invisible_ by default, but that can be changed by customizing this service.
 */
declare class DefaultScopeComputation implements ScopeComputation {
    protected readonly nameProvider: NameProvider;
    protected readonly descriptions: AstNodeDescriptionProvider;
    constructor(services: LangiumCoreServices);
    collectExportedSymbols(document: LangiumDocument, cancelToken?: CancellationToken): Promise<AstNodeDescription[]>;
    /**
     * Creates {@link AstNodeDescription AstNodeDescriptions} for the given {@link AstNode parentNode} and its children.
     * The list of children to be considered is determined by the function parameter {@link children}.
     * By default only the direct children of {@link parentNode} are visited, nested nodes are not exported.
     *
     * @param parentNode AST node to be exported, i.e., of which an {@link AstNodeDescription} shall be added to the returned list.
     * @param document The document containing the AST node to be exported.
     * @param children A function called with {@link parentNode} as single argument and returning an {@link Iterable} supplying the children to be visited, which must be directly or transitively contained in {@link parentNode}.
     * @param cancelToken Indicates when to cancel the current operation.
     * @throws `OperationCancelled` if a user action occurs during execution.
     * @returns A list of {@link AstNodeDescription AstNodeDescriptions} to be published to index.
     */
    collectExportedSymbolsForNode(parentNode: AstNode, document: LangiumDocument<AstNode>, children?: (root: AstNode) => Iterable<AstNode>, cancelToken?: CancellationToken): Promise<AstNodeDescription[]>;
    /**
     * Adds a single node to the list of exports if it has a name. Override this method to change how
     * symbols are exported, e.g. by modifying their exported name.
     */
    protected addExportedSymbol(node: AstNode, exports: AstNodeDescription[], document: LangiumDocument): void;
    collectLocalSymbols(document: LangiumDocument, cancelToken?: CancellationToken): Promise<LocalSymbols>;
    /**
     * Adds a single node to the local symbols of its containing document if it has a name.
     * The default implementation makes the node visible in the subtree of its container if it does have a container.
     * Override this method to change this, e.g. by increasing the visibility to a higher level in the AST.
     */
    protected addLocalSymbol(node: AstNode, document: LangiumDocument, symbols: MultiMap<AstNode, AstNodeDescription>): void;
}

declare class DefaultScopeProvider implements ScopeProvider {
    protected readonly reflection: AstReflection;
    protected readonly nameProvider: NameProvider;
    protected readonly descriptions: AstNodeDescriptionProvider;
    protected readonly indexManager: IndexManager;
    protected readonly globalScopeCache: WorkspaceCache<string, Scope>;
    constructor(services: LangiumCoreServices);
    getScope(context: ReferenceInfo): Scope;
    /**
     * Create a scope for the given collection of AST node descriptions.
     */
    protected createScope(elements: Iterable<AstNodeDescription>, outerScope?: Scope, options?: ScopeOptions): Scope;
    /**
     * Create a scope for the given collection of AST nodes, which need to be transformed into respective
     * descriptions first. This is done using the `NameProvider` and `AstNodeDescriptionProvider` services.
     */
    protected createScopeForNodes(elements: Iterable<AstNode>, outerScope?: Scope, options?: ScopeOptions): Scope;
    /**
     * Create a global scope filtered for the given reference type.
     */
    protected getGlobalScope(referenceType: string, _context: ReferenceInfo): Scope;
}

/**
 * Generic registry for Langium services, but capable of being used with extending service sets as well (such as the lsp-complete LangiumCoreServices set)
 */
declare class DefaultServiceRegistry implements ServiceRegistry {
    protected readonly languageIdMap: Map<string, LangiumCoreServices>;
    protected readonly fileExtensionMap: Map<string, LangiumCoreServices>;
    protected readonly fileNameMap: Map<string, LangiumCoreServices>;
    /**
     * @deprecated Since 3.1.0. Use the new `fileExtensionMap` (or `languageIdMap`) property instead.
     */
    protected get map(): Map<string, LangiumCoreServices> | undefined;
    protected readonly textDocuments?: TextDocumentProvider;
    constructor(services?: LangiumSharedCoreServices);
    register(language: LangiumCoreServices): void;
    getServices(uri: URI): LangiumCoreServices;
    hasServices(uri: URI): boolean;
    get all(): readonly LangiumCoreServices[];
}

/**
 * Context required for creating the default shared dependency injection module.
 */
declare interface DefaultSharedCoreModuleContext {
    /**
     * Factory function to create a {@link FileSystemProvider}.
     *
     * Langium exposes an `EmptyFileSystem` and `NodeFileSystem`, exported through `langium/node`.
     * When running Langium as part of a vscode language server or a Node.js app, using the `NodeFileSystem` is recommended,
     * the `EmptyFileSystem` in every other use case.
     */
    fileSystemProvider: (services: LangiumSharedCoreServices) => FileSystemProvider;
}

declare class DefaultTokenBuilder implements TokenBuilder {
    /**
     * The list of diagnostics stored during the lexing process of a single text.
     */
    protected diagnostics: LexingDiagnostic[];
    buildTokens(grammar: Grammar, options?: TokenBuilderOptions): TokenVocabulary;
    flushLexingReport(text: string): LexingReport;
    protected popDiagnostics(): LexingDiagnostic[];
    protected buildTerminalTokens(rules: Stream<AbstractRule>): TokenType[];
    protected buildTerminalToken(terminal: TerminalRule): TokenType;
    protected requiresCustomPattern(regex: RegExp): boolean;
    protected regexPatternFunction(regex: RegExp): CustomPatternMatcherFunc;
    protected buildKeywordTokens(rules: Stream<AbstractRule>, terminalTokens: TokenType[], options?: TokenBuilderOptions): TokenType[];
    protected buildKeywordToken(keyword: Keyword, terminalTokens: TokenType[], caseInsensitive: boolean): TokenType;
    protected buildKeywordPattern(keyword: Keyword, caseInsensitive: boolean): TokenPattern;
    protected findLongerAlt(keyword: Keyword, terminalTokens: TokenType[]): TokenType[];
}

declare class DefaultValueConverter implements ValueConverter {
    convert(input: string, cstNode: CstNode): ValueType;
    protected runConverter(rule: AbstractRule, input: string, cstNode: CstNode): ValueType;
}

declare class DefaultWorkspaceLock implements WorkspaceLock {
    private previousTokenSource;
    private writeQueue;
    private readQueue;
    private done;
    write(action: (token: CancellationToken) => MaybePromise<void>): Promise<void>;
    read<T>(action: () => MaybePromise<T>): Promise<T>;
    private enqueue;
    private performNextOperation;
    cancelWrite(): void;
}

declare class DefaultWorkspaceManager implements WorkspaceManager {
    initialBuildOptions: BuildOptions;
    protected readonly serviceRegistry: ServiceRegistry;
    protected readonly langiumDocuments: LangiumDocuments;
    protected readonly documentBuilder: DocumentBuilder;
    protected readonly fileSystemProvider: FileSystemProvider;
    protected readonly mutex: WorkspaceLock;
    protected readonly _ready: Deferred<void>;
    protected folders?: WorkspaceFolder_2[];
    constructor(services: LangiumSharedCoreServices);
    get ready(): Promise<void>;
    get workspaceFolders(): readonly WorkspaceFolder_2[] | undefined;
    initialize(params: InitializeParams): void;
    initialized(_params: InitializedParams): Promise<void>;
    initializeWorkspace(folders: WorkspaceFolder_2[], cancelToken?: CancellationToken): Promise<void>;
    /**
     * Performs the uninterruptable startup sequence of the workspace manager.
     * This methods loads all documents in the workspace and other documents and returns them.
     */
    protected performStartup(folders: WorkspaceFolder_2[]): Promise<LangiumDocument[]>;
    protected loadWorkspaceDocuments(uris: Stream<URI>, collector: (document: LangiumDocument) => void): Promise<void>;
    /**
     * Load all additional documents that shall be visible in the context of the given workspace
     * folders and add them to the collector. This can be used to include built-in libraries of
     * your language, which can be either loaded from provided files or constructed in memory.
     */
    protected loadAdditionalDocuments(_folders: WorkspaceFolder_2[], _collector: (document: LangiumDocument) => void): Promise<void>;
    /**
     * Determine the root folder of the source documents in the given workspace folder.
     * The default implementation returns the URI of the workspace folder, but you can override
     * this to return a subfolder like `src` instead.
     */
    protected getRootFolder(workspaceFolder: WorkspaceFolder_2): URI;
    /**
     * Traverse the file system folder identified by the given URI and its subfolders. All
     * contained files that match the file extensions are added to the `uris` array.
     */
    protected traverseFolder(folderPath: URI, uris: URI[]): Promise<void>;
    searchFolder(uri: URI): Promise<URI[]>;
    /**
     * Determine whether the given folder entry shall be included while indexing the workspace.
     */
    shouldIncludeEntry(entry: FileSystemNode): boolean;
}

/**
 * Simple implementation of the deferred pattern.
 * An object that exposes a promise and functions to resolve and reject it.
 */
declare class Deferred<T = void> {
    resolve: (value: T) => this;
    reject: (err?: unknown) => this;
    promise: Promise<T>;
}

/**
 * Client Capabilities for a {@link DefinitionRequest}.
 */
declare interface DefinitionClientCapabilities {
    /**
     * Whether definition supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * The client supports additional metadata in the form of definition links.
     *
     * @since 3.14.0
     */
    linkSupport?: boolean;
}

declare interface DehydrateContext {
    astNodes: Map<AstNode, any>;
    cstNodes: Map<CstNode, any>;
}

/**
 * Delays the execution of the current code to the next tick of the event loop.
 * Don't call this method directly in a tight loop to prevent too many promises from being created.
 */
declare function delayNextTick(): Promise<void>;

/**
 * Represents a diagnostic, such as a compiler error or warning. Diagnostic objects
 * are only valid in the scope of a resource.
 */
declare interface Diagnostic {
    /**
     * The range at which the message applies
     */
    range: Range_2;
    /**
     * The diagnostic's severity. Can be omitted. If omitted it is up to the
     * client to interpret diagnostics as error, warning, info or hint.
     */
    severity?: DiagnosticSeverity;
    /**
     * The diagnostic's code, which usually appear in the user interface.
     */
    code?: integer | string;
    /**
     * An optional property to describe the error code.
     * Requires the code field (above) to be present/not null.
     *
     * @since 3.16.0
     */
    codeDescription?: CodeDescription;
    /**
     * A human-readable string describing the source of this
     * diagnostic, e.g. 'typescript' or 'super lint'. It usually
     * appears in the user interface.
     */
    source?: string;
    /**
     * The diagnostic's message. It usually appears in the user interface
     */
    message: string;
    /**
     * Additional metadata about the diagnostic.
     *
     * @since 3.15.0
     */
    tags?: DiagnosticTag[];
    /**
     * An array of related diagnostic information, e.g. when symbol-names within
     * a scope collide all definitions can be marked via this property.
     */
    relatedInformation?: DiagnosticRelatedInformation[];
    /**
     * A data entry field that is preserved between a `textDocument/publishDiagnostics`
     * notification and `textDocument/codeAction` request.
     *
     * @since 3.16.0
     */
    data?: LSPAny;
}

/**
 * The Diagnostic namespace provides helper functions to work with
 * {@link Diagnostic} literals.
 */
declare namespace Diagnostic {
    /**
     * Creates a new Diagnostic literal.
     */
    function create(range: Range_2, message: string, severity?: DiagnosticSeverity, code?: integer | string, source?: string, relatedInformation?: DiagnosticRelatedInformation[]): Diagnostic;
    /**
     * Checks whether the given literal conforms to the {@link Diagnostic} interface.
     */
    function is(value: any): value is Diagnostic;
}

/**
 * Client capabilities specific to diagnostic pull requests.
 *
 * @since 3.17.0
 */
declare type DiagnosticClientCapabilities = {
    /**
     * Whether implementation supports dynamic registration. If this is set to `true`
     * the client supports the new `(TextDocumentRegistrationOptions & StaticRegistrationOptions)`
     * return value for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
    /**
     * Whether the clients supports related documents for document diagnostic pulls.
     */
    relatedDocumentSupport?: boolean;
};

/**
 * Shape of information commonly used in the `data` field of diagnostics.
 */
declare interface DiagnosticData {
    /** Diagnostic code for identifying which code action to apply. This code is _not_ shown in the user interface. */
    code: string;
    /** Specifies where to apply the code action in the form of a `DocumentSegment`. */
    actionSegment?: DocumentSegment;
    /** Specifies where to apply the code action in the form of a `Range`. */
    actionRange?: Range_2;
}

/**
 * Create DiagnosticData for a given diagnostic code. The result can be put into the `data` field of a DiagnosticInfo.
 */
declare function diagnosticData(code: string): DiagnosticData;

declare type DiagnosticInfo<N extends AstNode, P extends string = Properties<N>> = {
    /** The AST node to which the diagnostic is attached. */
    node: N;
    /** If a property name is given, the diagnostic is restricted to the corresponding text region. */
    property?: P;
    /** If the value of a keyword is given, the diagnostic will appear at its corresponding text region */
    keyword?: string;
    /** In case of a multi-value property (array), an index can be given to select a specific element. */
    index?: number;
    /** If you want to create a diagnostic independent to any property, use the range property. */
    range?: Range_2;
    /** The diagnostic's code, which usually appear in the user interface. */
    code?: integer | string;
    /** An optional property to describe the error code. */
    codeDescription?: CodeDescription;
    /** Additional metadata about the diagnostic. */
    tags?: DiagnosticTag[];
    /** An array of related diagnostic information, e.g. when symbol-names within a scope collide all definitions can be marked via this property. */
    relatedInformation?: DiagnosticRelatedInformation[];
    /** A data entry field that is preserved between a `textDocument/publishDiagnostics` notification and `textDocument/codeAction` request. */
    data?: unknown;
};

/**
 * Represents a related message and source code location for a diagnostic. This should be
 * used to point to code locations that cause or related to a diagnostics, e.g when duplicating
 * a symbol in a scope.
 */
declare interface DiagnosticRelatedInformation {
    /**
     * The location of this related diagnostic information.
     */
    location: Location_2;
    /**
     * The message of this related diagnostic information.
     */
    message: string;
}

/**
 * The DiagnosticRelatedInformation namespace provides helper functions to work with
 * {@link DiagnosticRelatedInformation} literals.
 */
declare namespace DiagnosticRelatedInformation {
    /**
     * Creates a new DiagnosticRelatedInformation literal.
     */
    function create(location: Location_2, message: string): DiagnosticRelatedInformation;
    /**
     * Checks whether the given literal conforms to the {@link DiagnosticRelatedInformation} interface.
     */
    function is(value: any): value is DiagnosticRelatedInformation;
}

/**
 * The diagnostic's severity.
 */
declare namespace DiagnosticSeverity {
    /**
     * Reports an error.
     */
    const Error: 1;
    /**
     * Reports a warning.
     */
    const Warning: 2;
    /**
     * Reports an information.
     */
    const Information: 3;
    /**
     * Reports a hint.
     */
    const Hint: 4;
}

declare type DiagnosticSeverity = 1 | 2 | 3 | 4;

/**
 * The diagnostic tags.
 *
 * @since 3.15.0
 */
declare namespace DiagnosticTag {
    /**
     * Unused or unnecessary code.
     *
     * Clients are allowed to render diagnostics with this tag faded out instead of having
     * an error squiggle.
     */
    const Unnecessary: 1;
    /**
     * Deprecated or obsolete code.
     *
     * Clients are allowed to rendered diagnostics with this tag strike through.
     */
    const Deprecated: 2;
}

declare type DiagnosticTag = 1 | 2;

/**
 * The diagnostic tags.
 *
 * @since 3.15.0
 */
declare namespace DiagnosticTag_2 {
    /**
     * Unused or unnecessary code.
     *
     * Clients are allowed to render diagnostics with this tag faded out instead of having
     * an error squiggle.
     */
    const Unnecessary: 1;
    /**
     * Deprecated or obsolete code.
     *
     * Clients are allowed to rendered diagnostics with this tag strike through.
     */
    const Deprecated: 2;
}

declare type DiagnosticTag_2 = 1 | 2;

/**
 * Workspace client capabilities specific to diagnostic pull requests.
 *
 * @since 3.17.0
 */
declare type DiagnosticWorkspaceClientCapabilities = {
    /**
     * Whether the client implementation supports a refresh request sent from
     * the server to the client.
     *
     * Note that this event is global and will force the client to refresh all
     * pulled diagnostics currently shown. It should be used with absolute care and
     * is useful for situation where a server for example detects a project wide
     * change that requires such a calculation.
     */
    refreshSupport?: boolean;
};

export declare type DiagramAST = Info | Packet | Pie | Architecture | GitGraph | EventModel | Radar | Railroad | RailroadEbnf | RailroadAbnf | RailroadPeg | Treemap | TreeView | Wardley | Cynefin;

declare interface DidChangeConfigurationClientCapabilities {
    /**
     * Did change configuration notification supports dynamic registration.
     */
    dynamicRegistration?: boolean;
}

/**
 * The parameters of a change configuration notification.
 */
declare interface DidChangeConfigurationParams {
    /**
     * The actual changed settings
     */
    settings: LSPAny_2;
}

declare interface DidChangeConfigurationRegistrationOptions {
    section?: string | string[];
}

declare interface DidChangeWatchedFilesClientCapabilities {
    /**
     * Did change watched files notification supports dynamic registration. Please note
     * that the current protocol doesn't support static configuration for file changes
     * from the server side.
     */
    dynamicRegistration?: boolean;
    /**
     * Whether the client has support for {@link  RelativePattern relative pattern}
     * or not.
     *
     * @since 3.17.0
     */
    relativePatternSupport?: boolean;
}

declare interface Direction extends GitGraph {
    readonly $type: 'Direction';
    dir: 'BT' | 'LR' | 'TB';
}

declare const Direction: {
    readonly $type: "Direction";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly dir: "dir";
    readonly statements: "statements";
    readonly title: "title";
};

declare interface Disjunction extends langium.AstNode {
    readonly $container: Conjunction | Disjunction | Group | NamedArgument | Negation;
    readonly $type: 'Disjunction';
    left: Condition;
    right: Condition;
}

declare const Disjunction: {
    readonly $type: "Disjunction";
    readonly left: "left";
    readonly right: "right";
};

declare interface Disposable_2 {
    /**
     * Dispose this object.
     */
    dispose(): void;
}

declare namespace Disposable_2 {
    function create(func: () => void): Disposable_2;
}

/******************************************************************************
 * Copyright 2021 TypeFox GmbH
 * This program and the accompanying materials are made available under the
 * terms of the MIT License, which is available in the project root.
 ******************************************************************************/
declare interface Disposable_3 {
    /**
     * Dispose this object.
     */
    dispose(): void;
}

declare namespace Disposable_3 {
    function create(callback: () => Promise<void>): AsyncDisposable_2;
    function create(callback: () => void): Disposable_3;
}

declare abstract class DisposableCache implements Disposable_3 {
    protected toDispose: Disposable_3[];
    protected isDisposed: boolean;
    onDispose(disposable: Disposable_3): void;
    dispose(): void;
    protected throwIfDisposed(): void;
    abstract clear(): void;
}

/**
 * Provides documentation for AST nodes.
 */
declare interface DocumentationProvider {
    /**
     * Returns a markdown documentation string for the specified AST node.
     *
     * The default implementation `JSDocDocumentationProvider` will inspect the comment associated with the specified node.
     */
    getDocumentation(node: AstNode): string | undefined;
}

/**
 * Shared-service for building and updating `LangiumDocument`s.
 */
declare interface DocumentBuilder {
    /** The options used for rebuilding documents after an update. */
    updateBuildOptions: BuildOptions;
    /**
     * Execute all necessary build steps for the given documents.
     *
     * @param documents Set of documents to be built.
     * @param options Options for the document builder.
     * @param cancelToken Indicates when to cancel the current operation.
     * @throws `OperationCanceled` if a user action occurs during execution
     */
    build<T extends AstNode>(documents: Array<LangiumDocument<T>>, options?: BuildOptions, cancelToken?: CancellationToken): Promise<void>;
    /**
     * This method is called when a document change is detected. It updates the state of all
     * affected documents, including those with references to the changed ones, so they are rebuilt.
     *
     * @param changed URIs of changed or created documents
     * @param deleted URIs of deleted documents
     * @param cancelToken allows to cancel the current operation
     * @throws `OperationCancelled` if cancellation is detected during execution
     */
    update(changed: URI[], deleted: URI[], cancelToken?: CancellationToken): Promise<void>;
    /**
     * Notify the given callback when a document update was triggered, but before any document
     * is rebuilt. Listeners to this event should not perform any long-running task.
     */
    onUpdate(callback: DocumentUpdateListener): Disposable_3;
    /**
     * Reset the state of a document to the specified state, removing any derived data as needed.
     *
     * @param document The document to reset.
     * @param state The state to reset the document to.
     */
    resetToState<T extends AstNode>(document: LangiumDocument<T>, state: DocumentState): void;
    /**
     * Notify the given callback when a set of documents has been built reaching the specified target state.
     */
    onBuildPhase(targetState: DocumentState, callback: DocumentBuildListener): Disposable_3;
    /**
     * Notify the specified callback when a document has been built reaching the specified target state.
     * Unlike {@link onBuildPhase} the listener is called for every single document.
     *
     * There are two main advantages compared to {@link onBuildPhase}:
     * 1. If the build is cancelled, {@link onDocumentPhase} will still fire for documents that have reached a specific state.
     *    Meanwhile, {@link onBuildPhase} won't fire for that state.
     * 2. The {@link DocumentBuilder} ensures that all {@link DocumentPhaseListener} instances are called for a built document.
     *    Even if the build is cancelled before those listeners were called.
     */
    onDocumentPhase(targetState: DocumentState, callback: DocumentPhaseListener): Disposable_3;
    /**
     * Wait until the workspace has reached the specified state for all documents.
     *
     * @param state The desired state. The promise won't resolve until all documents have reached this state
     * @param cancelToken Optionally allows to cancel the wait operation, disposing any listeners in the process
     * @throws `OperationCancelled` if cancellation has been requested before the state has been reached
     */
    waitUntil(state: DocumentState, cancelToken?: CancellationToken): Promise<void>;
    /**
     * Wait until the document specified by the {@link uri} has reached the specified state.
     *
     * @param state The desired state. The promise won't resolve until the document has reached this state.
     * @param uri The specified URI that points to the document. If the URI does not exist, the promise will resolve once the workspace has reached the specified state.
     * @param cancelToken Optionally allows to cancel the wait operation, disposing any listeners in the process.
     * @return The URI of the document that has reached the desired state, or `undefined` if the document does not exist.
     * @throws `OperationCancelled` if cancellation has been requested before the state has been reached
     */
    waitUntil(state: DocumentState, uri?: URI, cancelToken?: CancellationToken): Promise<URI | undefined>;
}

declare type DocumentBuildListener = (built: LangiumDocument[], cancelToken: CancellationToken) => void | Promise<void>;

declare interface DocumentBuildState {
    /** Whether a document has completed its last build process. */
    completed: boolean;
    /** The options used for the last build process. */
    options: BuildOptions;
    /** Additional information about the last build result. */
    result?: {
        validationChecks?: ValidationCategory[];
    };
}

/**
 * Every key/value pair in this cache is scoped to a document.
 * If this document is changed or deleted, all associated key/value pairs are deleted.
 */
declare class DocumentCache<K, V> extends ContextCache<URI | string, K, V, string> {
    /**
     * Creates a new document cache.
     *
     * @param sharedServices Service container instance to hook into document lifecycle events.
     * @param state Optional document state on which the cache should evict.
     * If not provided, the cache will evict on `DocumentBuilder#onUpdate`.
     * *Deleted* documents are considered in both cases.
     *
     * Providing a state here will use `DocumentBuilder#onDocumentPhase` instead,
     * which triggers on all documents that have been affected by this change, assuming that the
     * state is `DocumentState.Linked` or a later state.
     */
    constructor(sharedServices: LangiumSharedCoreServices, state?: DocumentState);
}

declare interface DocumentColorClientCapabilities {
    /**
     * Whether implementation supports dynamic registration. If this is set to `true`
     * the client supports the new `DocumentColorRegistrationOptions` return value
     * for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
}

/**
 * Client capabilities of a {@link DocumentFormattingRequest}.
 */
declare interface DocumentFormattingClientCapabilities {
    /**
     * Whether formatting supports dynamic registration.
     */
    dynamicRegistration?: boolean;
}

/**
 * Client Capabilities for a {@link DocumentHighlightRequest}.
 */
declare interface DocumentHighlightClientCapabilities {
    /**
     * Whether document highlight supports dynamic registration.
     */
    dynamicRegistration?: boolean;
}

/**
 * The client capabilities of a {@link DocumentLinkRequest}.
 */
declare interface DocumentLinkClientCapabilities {
    /**
     * Whether document link supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * Whether the client supports the `tooltip` property on `DocumentLink`.
     *
     * @since 3.15.0
     */
    tooltipSupport?: boolean;
}

/**
 * Client capabilities of a {@link DocumentOnTypeFormattingRequest}.
 */
declare interface DocumentOnTypeFormattingClientCapabilities {
    /**
     * Whether on type formatting supports dynamic registration.
     */
    dynamicRegistration?: boolean;
}

declare type DocumentPhaseListener = (built: LangiumDocument, cancelToken: CancellationToken) => void | Promise<void>;

/**
 * Client capabilities of a {@link DocumentRangeFormattingRequest}.
 */
declare interface DocumentRangeFormattingClientCapabilities {
    /**
     * Whether range formatting supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * Whether the client supports formatting multiple ranges at once.
     *
     * @since 3.18.0
     * @proposed
     */
    rangesSupport?: boolean;
}

declare interface DocumentSegment {
    readonly range: Range_2;
    readonly offset: number;
    readonly length: number;
    readonly end: number;
}

/**
 * A document is subject to several phases that are run in predefined order. Any state value implies that
 * smaller state values are finished as well.
 */
declare enum DocumentState {
    /**
     * The text content has changed and needs to be parsed again. The AST held by this outdated
     * document instance is no longer valid.
     */
    Changed = 0,
    /**
     * An AST has been created from the text content. The document structure can be traversed,
     * but cross-references cannot be resolved yet. If necessary, the structure can be manipulated
     * at this stage as a preprocessing step.
     */
    Parsed = 1,
    /**
     * The `IndexManager` service has processed AST nodes of this document. This means the
     * exported symbols are available in the global scope and can be resolved from other documents.
     */
    IndexedContent = 2,
    /**
     * The `ScopeComputation` service has processed this document. This means the document's locally accessible
     * symbols are captured in a `DocumentSymbols` table and can be looked up by the `ScopeProvider` service.
     * Once a document has reached this state, you may follow every reference - it will lazily
     * resolve its `ref` property and yield either the target AST node or `undefined` in case
     * the target is not in scope.
     */
    ComputedScopes = 3,
    /**
     * The `Linker` service has processed this document. All outgoing references have been
     * resolved or marked as erroneous.
     */
    Linked = 4,
    /**
     * The `IndexManager` service has processed AST node references of this document. This is
     * necessary to determine which documents are affected by a change in one of the workspace
     * documents.
     */
    IndexedReferences = 5,
    /**
     * The `DocumentValidator` service has processed this document. The language server listens
     * to the results of this phase and sends diagnostics to the client.
     */
    Validated = 6
}

/**
 * Client Capabilities for a {@link DocumentSymbolRequest}.
 */
declare interface DocumentSymbolClientCapabilities {
    /**
     * Whether document symbol supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * Specific capabilities for the `SymbolKind` in the
     * `textDocument/documentSymbol` request.
     */
    symbolKind?: {
        /**
         * The symbol kind values the client supports. When this
         * property exists the client also guarantees that it will
         * handle values outside its set gracefully and falls back
         * to a default value when unknown.
         *
         * If this property is not present the client only supports
         * the symbol kinds from `File` to `Array` as defined in
         * the initial version of the protocol.
         */
        valueSet?: SymbolKind[];
    };
    /**
     * The client supports hierarchical document symbols.
     */
    hierarchicalDocumentSymbolSupport?: boolean;
    /**
     * The client supports tags on `SymbolInformation`. Tags are supported on
     * `DocumentSymbol` if `hierarchicalDocumentSymbolSupport` is set to true.
     * Clients supporting tags have to handle unknown tags gracefully.
     *
     * @since 3.16.0
     */
    tagSupport?: {
        /**
         * The tags supported by the client.
         */
        valueSet: SymbolTag[];
    };
    /**
     * The client supports an additional label presented in the UI when
     * registering a document symbol provider.
     *
     * @since 3.16.0
     */
    labelSupport?: boolean;
}

declare type DocumentUpdateListener = (changed: URI[], deleted: URI[]) => void | Promise<void>;

/**
 * A tagging type for string properties that are actually URIs.
 */
declare type DocumentUri = string;

/**
 * A tagging type for string properties that are actually document URIs.
 */
declare type DocumentUri_2 = string;

declare namespace DocumentUri_2 {
    function is(value: any): value is DocumentUri_2;
}

/**
 * A tagging type for string properties that are actually document URIs.
 */
declare type DocumentUri_3 = string;

declare namespace DocumentUri_3 {
    function is(value: any): value is DocumentUri_3;
}

/**
 * Language-specific service for validating `LangiumDocument`s.
 */
declare interface DocumentValidator {
    /**
     * Validates the whole specified document.
     *
     * @param document specified document to validate
     * @param options options to control the validation process
     * @param cancelToken allows to cancel the current operation
     * @throws `OperationCanceled` if a user action occurs during execution
     */
    validateDocument(document: LangiumDocument, options?: ValidationOptions, cancelToken?: CancellationToken): Promise<Diagnostic[]>;
}

declare namespace DocumentValidator {
    const LexingError = "lexing-error";
    const LexingWarning = "lexing-warning";
    const LexingInfo = "lexing-info";
    const LexingHint = "lexing-hint";
    const ParsingError = "parsing-error";
    const LinkingError = "linking-error";
}

export declare interface DomainBlock extends langium_2.AstNode {
    readonly $container: Cynefin;
    readonly $type: 'DomainBlock';
    domain: string;
    items: Array<DomainItem>;
}

export declare const DomainBlock: {
    readonly $type: "DomainBlock";
    readonly domain: "domain";
    readonly items: "items";
};

export declare interface DomainItem extends langium_2.AstNode {
    readonly $container: DomainBlock;
    readonly $type: 'DomainItem';
    label: string;
}

export declare const DomainItem: {
    readonly $type: "DomainItem";
    readonly label: "label";
};

/**
 * Use this `IteratorResult` when implementing a `StreamImpl` to indicate that there are no more elements in the stream.
 */
declare const DONE_RESULT: IteratorReturnResult<undefined>;

/**
 * Eagerly load all services in the given dependency injection container. This is sometimes
 * necessary because services can register event listeners in their constructors.
 */
declare function eagerLoad<T>(item: T): T;

export declare interface EbnfChoice extends langium_2.AstNode {
    readonly $container: EbnfGroup | EbnfOptional | EbnfRepetition | EbnfRule;
    readonly $type: 'EbnfChoice';
    alternatives: Array<EbnfSequence>;
}

export declare const EbnfChoice: {
    readonly $type: "EbnfChoice";
    readonly alternatives: "alternatives";
};

export declare interface EbnfExceptionPostfix extends EbnfPostfix {
    readonly $type: 'EbnfExceptionPostfix';
    except: EbnfPrimary;
}

export declare const EbnfExceptionPostfix: {
    readonly $type: "EbnfExceptionPostfix";
    readonly except: "except";
};

export declare interface EbnfGroup extends EbnfPrimary {
    readonly $type: 'EbnfGroup';
    element: EbnfChoice;
}

export declare const EbnfGroup: {
    readonly $type: "EbnfGroup";
    readonly element: "element";
};

export declare interface EbnfNonTerminal extends EbnfPrimary {
    readonly $type: 'EbnfNonTerminal';
    name: string;
}

export declare const EbnfNonTerminal: {
    readonly $type: "EbnfNonTerminal";
    readonly name: "name";
};

export declare interface EbnfOneOrMorePostfix extends EbnfPostfix {
    readonly $type: 'EbnfOneOrMorePostfix';
    operator: string;
}

export declare const EbnfOneOrMorePostfix: {
    readonly $type: "EbnfOneOrMorePostfix";
    readonly operator: "operator";
};

export declare interface EbnfOptional extends EbnfPrimary {
    readonly $type: 'EbnfOptional';
    element: EbnfChoice;
}

export declare const EbnfOptional: {
    readonly $type: "EbnfOptional";
    readonly element: "element";
};

export declare interface EbnfOptionalPostfix extends EbnfPostfix {
    readonly $type: 'EbnfOptionalPostfix';
    operator: string;
}

export declare const EbnfOptionalPostfix: {
    readonly $type: "EbnfOptionalPostfix";
    readonly operator: "operator";
};

export declare interface EbnfPostfix extends langium_2.AstNode {
    readonly $type: 'EbnfExceptionPostfix' | 'EbnfOneOrMorePostfix' | 'EbnfOptionalPostfix' | 'EbnfPostfix' | 'EbnfZeroOrMorePostfix';
}

export declare const EbnfPostfix: {
    readonly $type: "EbnfPostfix";
};

export declare interface EbnfPrimary extends langium_2.AstNode {
    readonly $type: 'EbnfGroup' | 'EbnfNonTerminal' | 'EbnfOptional' | 'EbnfPrimary' | 'EbnfRepetition' | 'EbnfSpecial' | 'EbnfTerminal';
}

export declare const EbnfPrimary: {
    readonly $type: "EbnfPrimary";
};

export declare interface EbnfRepetition extends EbnfPrimary {
    readonly $type: 'EbnfRepetition';
    element: EbnfChoice;
}

export declare const EbnfRepetition: {
    readonly $type: "EbnfRepetition";
    readonly element: "element";
};

export declare interface EbnfRule extends langium_2.AstNode {
    readonly $container: RailroadEbnf;
    readonly $type: 'EbnfRule';
    definition: EbnfChoice;
    name: string;
}

export declare const EbnfRule: {
    readonly $type: "EbnfRule";
    readonly definition: "definition";
    readonly name: "name";
};

export declare interface EbnfSequence extends langium_2.AstNode {
    readonly $container: EbnfChoice;
    readonly $type: 'EbnfSequence';
    elements: Array<EbnfTerm>;
}

export declare const EbnfSequence: {
    readonly $type: "EbnfSequence";
    readonly elements: "elements";
};

export declare interface EbnfSpecial extends EbnfPrimary {
    readonly $type: 'EbnfSpecial';
    text: string;
}

export declare const EbnfSpecial: {
    readonly $type: "EbnfSpecial";
    readonly text: "text";
};

export declare interface EbnfTerm extends langium_2.AstNode {
    readonly $container: EbnfSequence;
    readonly $type: 'EbnfTerm';
    base: EbnfPrimary;
    postfixes: Array<EbnfPostfix>;
}

export declare const EbnfTerm: {
    readonly $type: "EbnfTerm";
    readonly base: "base";
    readonly postfixes: "postfixes";
};

export declare interface EbnfTerminal extends EbnfPrimary {
    readonly $type: 'EbnfTerminal';
    value: string;
}

export declare const EbnfTerminal: {
    readonly $type: "EbnfTerminal";
    readonly value: "value";
};

export declare interface EbnfZeroOrMorePostfix extends EbnfPostfix {
    readonly $type: 'EbnfZeroOrMorePostfix';
    operator: string;
}

export declare const EbnfZeroOrMorePostfix: {
    readonly $type: "EbnfZeroOrMorePostfix";
    readonly operator: "operator";
};

declare interface Edge extends langium_2.AstNode {
    readonly $container: Architecture;
    readonly $type: 'EOL' | 'Edge';
    lhsDir: string;
    lhsGroup: boolean;
    lhsId: string;
    lhsInto: boolean;
    rhsDir: string;
    rhsGroup: boolean;
    rhsId: string;
    rhsInto: boolean;
    title?: string;
}

declare const Edge: {
    readonly $type: "Edge";
    readonly lhsDir: "lhsDir";
    readonly lhsGroup: "lhsGroup";
    readonly lhsId: "lhsId";
    readonly lhsInto: "lhsInto";
    readonly rhsDir: "rhsDir";
    readonly rhsGroup: "rhsGroup";
    readonly rhsId: "rhsId";
    readonly rhsInto: "rhsInto";
    readonly title: "title";
};

declare type EM_EID = string;

declare type EM_FI = string;

export declare interface EmDataEntity extends langium_2.AstNode {
    readonly $container: EventModel;
    readonly $type: 'EmDataEntity';
    dataBlockValue: string;
    dataType?: EmDataType;
    name: EM_EID;
}

export declare const EmDataEntity: {
    readonly $type: "EmDataEntity";
    readonly dataBlockValue: "dataBlockValue";
    readonly dataType: "dataType";
    readonly name: "name";
};

export declare type EmDataType = 'figma' | 'html' | 'jsobj' | 'json' | 'md' | 'salt' | 'text' | 'uri';

export declare type EmFrame = EmResetFrame | EmTimeFrame;

export declare const EmFrame: {
    readonly $type: "EmFrame";
};

declare interface EmGwt extends langium_2.AstNode {
    readonly $container: EventModel;
    readonly $type: 'EmGwt';
    givenStatements: Array<EmGwtStatement>;
    sourceFrame: langium_2.Reference<EmFrame>;
    thenStatements: Array<EmGwtStatement>;
    whenStatements: Array<EmGwtStatement>;
}

declare const EmGwt: {
    readonly $type: "EmGwt";
    readonly givenStatements: "givenStatements";
    readonly sourceFrame: "sourceFrame";
    readonly thenStatements: "thenStatements";
    readonly whenStatements: "whenStatements";
};

declare interface EmGwtStatement extends langium_2.AstNode {
    readonly $container: EmGwt;
    readonly $type: 'EmGwtStatement' | 'EmModelEntityType';
    entityIdentifier: langium_2.Reference<EmModelEntity>;
}

declare const EmGwtStatement: {
    readonly $type: "EmGwtStatement";
    readonly entityIdentifier: "entityIdentifier";
};

declare class Emitter<T> {
    private _options?;
    private static _noop;
    private _event;
    private _callbacks;
    constructor(_options?: EmitterOptions | undefined);
    /**
     * For the public to allow to subscribe
     * to events from this Emitter
     */
    get event(): Event_2<T>;
    /**
     * To be kept private to fire an event to
     * subscribers
     */
    fire(event: T): any;
    dispose(): void;
}

declare interface EmitterOptions {
    onFirstListenerAdd?: Function;
    onLastListenerRemove?: Function;
}

declare interface EmModelEntity extends langium_2.AstNode {
    readonly $container: EventModel;
    readonly $type: 'EmModelEntity';
    name: QualifiedName;
}

declare const EmModelEntity: {
    readonly $type: "EmModelEntity";
    readonly name: "name";
};

export declare type EmModelEntityType = 'cmd' | 'command' | 'event' | 'evt' | 'pcr' | 'processor' | 'readmodel' | 'rmo' | 'ui';

declare interface EmNoteEntity extends langium_2.AstNode {
    readonly $container: EventModel;
    readonly $type: 'EmNoteEntity';
    dataBlockValue: string;
    dataType?: EmDataType;
    sourceFrame: langium_2.Reference<EmFrame>;
}

declare const EmNoteEntity: {
    readonly $type: "EmNoteEntity";
    readonly dataBlockValue: "dataBlockValue";
    readonly dataType: "dataType";
    readonly sourceFrame: "sourceFrame";
};

declare const EMPTY_SCOPE: Scope;

/**
 * An empty stream of any type.
 */
declare const EMPTY_STREAM: Stream<any>;

declare const EmptyFileSystem: {
    fileSystemProvider: () => EmptyFileSystemProvider;
};

declare class EmptyFileSystemProvider implements FileSystemProvider {
    stat(_uri: URI): Promise<FileSystemNode>;
    statSync(_uri: URI): FileSystemNode;
    exists(): Promise<boolean>;
    existsSync(): boolean;
    readBinary(): Promise<Uint8Array>;
    readBinarySync(): Uint8Array;
    readFile(): Promise<string>;
    readFileSync(): string;
    readDirectory(): Promise<FileSystemNode[]>;
    readDirectorySync(): FileSystemNode[];
}

declare interface EmResetFrame extends langium_2.AstNode {
    readonly $container: EventModel;
    readonly $type: 'EmResetFrame';
    dataInlineValue?: string;
    dataReference?: langium_2.Reference<EmDataEntity>;
    dataType?: EmDataType;
    entityIdentifier: QualifiedName;
    modelEntityType: EmModelEntityType;
    name: EM_FI;
    sourceFrames: Array<langium_2.Reference<EmFrame>>;
}

declare const EmResetFrame: {
    readonly $type: "EmResetFrame";
    readonly dataInlineValue: "dataInlineValue";
    readonly dataReference: "dataReference";
    readonly dataType: "dataType";
    readonly entityIdentifier: "entityIdentifier";
    readonly modelEntityType: "modelEntityType";
    readonly name: "name";
    readonly sourceFrames: "sourceFrames";
};

declare interface EmTimeFrame extends langium_2.AstNode {
    readonly $container: EventModel;
    readonly $type: 'EmTimeFrame';
    dataInlineValue?: string;
    dataReference?: langium_2.Reference<EmDataEntity>;
    dataType?: EmDataType;
    entityIdentifier: QualifiedName;
    modelEntityType: EmModelEntityType;
    name: EM_FI;
    sourceFrames: Array<langium_2.Reference<EmFrame>>;
}

declare const EmTimeFrame: {
    readonly $type: "EmTimeFrame";
    readonly dataInlineValue: "dataInlineValue";
    readonly dataReference: "dataReference";
    readonly dataType: "dataType";
    readonly entityIdentifier: "entityIdentifier";
    readonly modelEntityType: "modelEntityType";
    readonly name: "name";
    readonly sourceFrames: "sourceFrames";
};

declare interface EndOfFile extends AbstractElement {
    readonly $type: 'EndOfFile';
}

declare const EndOfFile: {
    readonly $type: "EndOfFile";
    readonly cardinality: "cardinality";
};

declare interface Entry extends langium_2.AstNode {
    readonly $container: Curve;
    readonly $type: 'Entry';
    axis?: langium_2.Reference<Axis>;
    value: number;
}

declare const Entry: {
    readonly $type: "Entry";
    readonly axis: "axis";
    readonly value: "value";
};

declare class ErrorWithLocation extends Error {
    constructor(node: CstNode | undefined, message: string);
}

declare function escapeRegExp(value: string): string;

/**
 * Represents a typed event.
 */
declare interface Event_2<T> {
    /**
     *
     * @param listener The listener function will be called when the event happens.
     * @param thisArgs The 'this' which will be used when calling the event listener.
     * @param disposables An array to which a {{IDisposable}} will be added.
     * @return
     */
    (listener: (e: T) => any, thisArgs?: any, disposables?: Disposable_2[]): Disposable_2;
}

declare namespace Event_2 {
    const None: Event_2<any>;
}

export declare interface EventModel extends langium_2.AstNode {
    readonly $type: 'EventModel';
    accDescr?: string;
    accTitle?: string;
    dataEntities: Array<EmDataEntity>;
    frames: Array<EmFrame>;
    gwtEntities: Array<EmGwt>;
    modelEntities: Array<EmModelEntity>;
    noteEntities: Array<EmNoteEntity>;
    title?: string;
}

export declare const EventModel: {
    readonly $type: "EventModel";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly dataEntities: "dataEntities";
    readonly frames: "frames";
    readonly gwtEntities: "gwtEntities";
    readonly modelEntities: "modelEntities";
    readonly noteEntities: "noteEntities";
    readonly title: "title";
};

/** Contains the reachable terminals & keywords and all available types of the 'EventModeling' language. */
declare namespace EventModeling {
    const Terminals: {
        EM_ID: RegExp;
        EM_FID: RegExp;
        EM_DATA_INLINE: RegExp;
        EM_DATA_BLOCK: RegExp;
        EM_ACC_DESCR: RegExp;
        EM_ACC_TITLE: RegExp;
        EM_TITLE: RegExp;
        EM_WS: RegExp;
        EM_YAML: RegExp;
        EM_DIRECTIVE: RegExp;
        EM_SINGLE_LINE_COMMENT: RegExp;
        EM_ML_COMMENT: RegExp;
        EM_SL_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "->>" | "." | "[[" | "]]" | "`" | "cmd" | "command" | "data" | "entity" | "event" | "eventmodeling" | "evt" | "figma" | "given" | "gwt" | "html" | "jsobj" | "json" | "md" | "note" | "pcr" | "processor" | "readmodel" | "resetframe" | "rf" | "rmo" | "salt" | "text" | "tf" | "then" | "timeframe" | "ui" | "uri" | "when";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Common: Common;
        EmDataEntity: EmDataEntity;
        EmFrame: EmFrame;
        EmGwt: EmGwt;
        EmGwtStatement: EmGwtStatement;
        EmModelEntity: EmModelEntity;
        EmNoteEntity: EmNoteEntity;
        EmResetFrame: EmResetFrame;
        EmTimeFrame: EmTimeFrame;
        EventModel: EventModel;
    };
}

declare interface EventModelingAddedServices {
    parser: {
        TokenBuilder: EventModelingTokenBuilder;
        ValueConverter: CommonValueConverter;
    };
    validation: {
        EventModelingValidator: EventModelingValidator;
    };
}

export declare const EventModelingGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

export declare const EventModelingModule: Module<EventModelingServices, PartialLangiumCoreServices & EventModelingAddedServices>;

export declare type EventModelingServices = LangiumCoreServices & EventModelingAddedServices;

declare class EventModelingTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare class EventModelingValidator {
    checkSourceFrameTypes(frame: EmTimeFrame | EmResetFrame, accept: ValidationAcceptor): void;
    private validateSources;
}

declare interface Evolution extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'EOL' | 'Evolution';
    stages: Array<EvolutionStage>;
}

declare const Evolution: {
    readonly $type: "Evolution";
    readonly stages: "stages";
};

declare interface EvolutionStage extends langium_2.AstNode {
    readonly $container: Evolution;
    readonly $type: 'EvolutionStage';
    boundary?: number;
    name: string;
    secondName?: string;
}

declare const EvolutionStage: {
    readonly $type: "EvolutionStage";
    readonly boundary: "boundary";
    readonly name: "name";
    readonly secondName: "secondName";
};

declare interface Evolve extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'EOL' | 'Evolve';
    component: string;
    target: number;
}

declare const Evolve: {
    readonly $type: "Evolve";
    readonly component: "component";
    readonly target: "target";
};

/**
 * The client capabilities of a {@link ExecuteCommandRequest}.
 */
declare interface ExecuteCommandClientCapabilities {
    /**
     * Execute command supports dynamic registration.
     */
    dynamicRegistration?: boolean;
}

/**
 * Describes a union type including only names(!) of properties of type T whose property value is of a certain type K,
 *  or 'never' in case of no such properties.
 * It evaluates the value type regardless of the property being optional or not by converting T to Required<T>.
 * Note the '-?' in '[I in keyof T]-?:' that is required to map all optional but un-intended properties to 'never'.
 * Without that, optional props like those inherited from 'AstNode' would be mapped to 'never|undefined',
 *  and the subsequent value mapping ('...[keyof T]') would yield 'undefined' instead of 'never' for AstNode types
 *  not having any property matching type K, which in turn yields follow-up errors.
 */
declare type ExtractKeysOfValueType<T, K> = {
    [I in keyof T]-?: Required<T>[I] extends K ? I : never;
}[keyof T];

declare type FailureHandlingKind = 'abort' | 'transactional' | 'undo' | 'textOnlyTransactional';

declare namespace FailureHandlingKind {
    /**
     * Applying the workspace change is simply aborted if one of the changes provided
     * fails. All operations executed before the failing operation stay executed.
     */
    const Abort: FailureHandlingKind;
    /**
     * All operations are executed transactional. That means they either all
     * succeed or no changes at all are applied to the workspace.
     */
    const Transactional: FailureHandlingKind;
    /**
     * If the workspace edit contains only textual file changes they are executed transactional.
     * If resource changes (create, rename or delete file) are part of the change the failure
     * handling strategy is abort.
     */
    const TextOnlyTransactional: FailureHandlingKind;
    /**
     * The client tries to undo the operations already executed. But there is no
     * guarantee that this is succeeding.
     */
    const Undo: FailureHandlingKind;
}

declare type FeatureName = 'assoc' | 'current' | 'entry' | 'extends' | 'false' | 'fragment' | 'grammar' | 'hidden' | 'import' | 'infer' | 'infers' | 'infix' | 'interface' | 'left' | 'on' | 'returns' | 'right' | 'terminal' | 'true' | 'type' | 'with' | PrimitiveType | string;

/**
 * Capabilities relating to events from file operations by the user in the client.
 *
 * These events do not come from the file system, they come from user operations
 * like renaming a file in the UI.
 *
 * @since 3.16.0
 */
declare interface FileOperationClientCapabilities {
    /**
     * Whether the client supports dynamic registration for file requests/notifications.
     */
    dynamicRegistration?: boolean;
    /**
     * The client has support for sending didCreateFiles notifications.
     */
    didCreate?: boolean;
    /**
     * The client has support for sending willCreateFiles requests.
     */
    willCreate?: boolean;
    /**
     * The client has support for sending didRenameFiles notifications.
     */
    didRename?: boolean;
    /**
     * The client has support for sending willRenameFiles requests.
     */
    willRename?: boolean;
    /**
     * The client has support for sending didDeleteFiles notifications.
     */
    didDelete?: boolean;
    /**
     * The client has support for sending willDeleteFiles requests.
     */
    willDelete?: boolean;
}

/**
 * The FileSelector provides file names and extensions used by this extension.
 */
declare interface FileSelector {
    /** Allowed file extensions (e.g., ["ts", "js"]). */
    fileExtensions: string[];
    /** Allowed file names (e.g., ["config", "settings"]). */
    fileNames: string[];
}

declare type FileSystemFilter = (node: FileSystemNode) => boolean;

declare interface FileSystemNode {
    readonly isFile: boolean;
    readonly isDirectory: boolean;
    readonly uri: URI;
}

/**
 * Provides methods to interact with an abstract file system. The default implementation is based on the node.js `fs` API.
 */
declare interface FileSystemProvider {
    /**
     * Gets the status of a file or directory.
     * The status includes meta data such as whether the node is a file or directory.
     * @param uri The URI of the file or directory.
     */
    stat(uri: URI): Promise<FileSystemNode>;
    /**
     * Gets the status of a file or directory synchronously.
     * The status includes meta data such as whether the node is a file or directory.
     * @param uri The URI of the file or directory.
     */
    statSync(uri: URI): FileSystemNode;
    /**
     * Checks if a file exists at the specified URI.
     * @returns `true` if a file exists at the specified URI, `false` otherwise.
     */
    exists(uri: URI): Promise<boolean>;
    /**
     * Checks if a file exists at the specified URI synchronously.
     * @returns `true` if a file exists at the specified URI, `false` otherwise.
     */
    existsSync(uri: URI): boolean;
    /**
     * Reads a binary file asynchronously from a given URI.
     * @returns The binary content of the file with the specified URI.
     */
    readBinary(uri: URI): Promise<Uint8Array>;
    /**
     * Reads a binary file synchronously from a given URI.
     * @returns The binary content of the file with the specified URI.
     */
    readBinarySync(uri: URI): Uint8Array;
    /**
     * Reads a document asynchronously from a given URI.
     * @returns The string content of the file with the specified URI.
     */
    readFile(uri: URI): Promise<string>;
    /**
     * Reads a document synchronously from a given URI.
     * @returns The string content of the file with the specified
     */
    readFileSync(uri: URI): string;
    /**
     * Reads the directory information for the given URI.
     * @returns The list of file system entries that are contained within the specified directory.
     */
    readDirectory(uri: URI): Promise<FileSystemNode[]>;
    /**
     * Reads the directory information for the given URI synchronously.
     * @returns The list of file system entries that are contained within the specified directory.
     */
    readDirectorySync(uri: URI): FileSystemNode[];
}

/**
 * If the given CST node was parsed in the context of a property assignment, the respective `Assignment` grammar
 * node is returned. If no assignment is found, the result is `undefined`.
 *
 * @param cstNode A CST node for which to find a property assignment.
 */
declare function findAssignment(cstNode: CstNode): GrammarAST.Assignment | undefined;

declare function findCommentNode(cstNode: CstNode | undefined, commentNames: string[]): CstNode | undefined;

/**
 * Performs `findLeafNodeAtOffset` with a minor difference: When encountering a character that matches the `nameRegexp` argument,
 * it will instead return the leaf node at the `offset - 1` position.
 *
 * For LSP services, users expect that the declaration of an element is available if the cursor is directly after the element.
 */
declare function findDeclarationNodeAtOffset(cstNode: CstNode | undefined, offset: number, nameRegexp?: RegExp): LeafCstNode | undefined;

/**
 * Finds the leaf CST node at the specified 0-based string offset.
 * Note that the given offset will be within the range of the returned leaf node.
 *
 * If the offset does not point to a CST node (but just white space), this method will return `undefined`.
 *
 * @param node The CST node to search through.
 * @param offset The specified offset.
 * @returns The CST node at the specified offset.
 */
declare function findLeafNodeAtOffset(node: CstNode, offset: number): LeafCstNode | undefined;

/**
 * Finds the leaf CST node at the specified 0-based string offset.
 * If no CST node exists at the specified position, it will return the leaf node before it.
 *
 * If there is no leaf node before the specified offset, this method will return `undefined`.
 *
 * @param node The CST node to search through.
 * @param offset The specified offset.
 * @returns The CST node closest to the specified offset.
 */
declare function findLeafNodeBeforeOffset(node: CstNode, offset: number): LeafCstNode | undefined;

/**
 * Find an assignment to the `name` property for the given grammar type. This requires the `type` to be inferred
 * from a parser rule, and that rule must contain an assignment to the `name` property. In all other cases,
 * this function returns `undefined`.
 */
declare function findNameAssignment(type: GrammarAST.AbstractType): GrammarAST.Assignment | undefined;

/**
 * Find a single CST node within the given node that corresponds to the specified keyword.
 *
 * @param node A CST node in which to look for keywords. If this is undefined, the result is `undefined`.
 * @param keyword A keyword as specified in the grammar.
 * @param index If no index is specified or the index is less than zero, the first found node is returned. If the
 *        specified index exceeds the number of keyword occurrences, the last found node is returned. Otherwise,
 *        the node with the specified index is returned.
 */
declare function findNodeForKeyword(node: CstNode | undefined, keyword: string, index?: number): CstNode | undefined;

/**
 * Find a single CST node within the given node that contributes to the specified property.
 *
 * @param node A CST node in which to look for property assignments. If this is undefined, the result is `undefined`.
 * @param property A property name of the constructed AST node. If this is undefined, the result is `undefined`.
 * @param index If no index is specified or the index is less than zero, the first found node is returned. If the
 *        specified index exceeds the number of assignments to the property, the last found node is returned. Otherwise,
 *        the node with the specified index is returned.
 */
declare function findNodeForProperty(node: CstNode | undefined, property: string | undefined, index?: number): CstNode | undefined;

/**
 * Find all CST nodes within the given node that correspond to the specified keyword.
 *
 * @param node A CST node in which to look for keywords. If this is undefined, the result is an empty array.
 * @param keyword A keyword as specified in the grammar.
 */
declare function findNodesForKeyword(node: CstNode | undefined, keyword: string): CstNode[];

declare function findNodesForKeywordInternal(node: CstNode, keyword: string, element: AstNode | undefined): CstNode[];

/**
 * Find all CST nodes within the given node that contribute to the specified property.
 *
 * @param node A CST node in which to look for property assignments. If this is undefined, the result is an empty array.
 * @param property A property name of the constructed AST node. If this is undefined, the result is an empty array.
 */
declare function findNodesForProperty(node: CstNode | undefined, property: string | undefined): CstNode[];

declare interface FindReferencesOptions {
    /**
     * When set, the `findReferences` method will only return references/declarations from the specified document.
     */
    documentUri?: URI;
    /**
     * Whether the returned list of references should include the declaration.
     */
    includeDeclaration?: boolean;
}

/**
 * Returns the root node of the given AST node by following the `$container` references.
 */
declare function findRootNode(node: AstNode): AstNode;

declare type FlatStream<T, Depth extends number> = {
    'done': Stream<T>;
    'recur': T extends Iterable<infer Content> ? FlatStream<Content, MinusOne<Depth>> : Stream<T>;
}[Depth extends 0 ? 'done' : 'recur'];

/**
 * Create a stream of all leaf nodes that are directly and indirectly contained in the given root node.
 */
declare function flattenCst(node: CstNode): Stream<LeafCstNode>;

declare interface FoldingRangeClientCapabilities {
    /**
     * Whether implementation supports dynamic registration for folding range
     * providers. If this is set to `true` the client supports the new
     * `FoldingRangeRegistrationOptions` return value for the corresponding
     * server capability as well.
     */
    dynamicRegistration?: boolean;
    /**
     * The maximum number of folding ranges that the client prefers to receive
     * per document. The value serves as a hint, servers are free to follow the
     * limit.
     */
    rangeLimit?: uinteger_2;
    /**
     * If set, the client signals that it only supports folding complete lines.
     * If set, client will ignore specified `startCharacter` and `endCharacter`
     * properties in a FoldingRange.
     */
    lineFoldingOnly?: boolean;
    /**
     * Specific options for the folding range kind.
     *
     * @since 3.17.0
     */
    foldingRangeKind?: {
        /**
         * The folding range kind values the client supports. When this
         * property exists the client also guarantees that it will
         * handle values outside its set gracefully and falls back
         * to a default value when unknown.
         */
        valueSet?: FoldingRangeKind[];
    };
    /**
     * Specific options for the folding range.
     *
     * @since 3.17.0
     */
    foldingRange?: {
        /**
         * If set, the client signals that it supports setting collapsedText on
         * folding ranges to display custom labels instead of the default text.
         *
         * @since 3.17.0
         */
        collapsedText?: boolean;
    };
}

/**
 * A set of predefined range kinds.
 */
declare namespace FoldingRangeKind {
    /**
     * Folding range for a comment
     */
    const Comment = "comment";
    /**
     * Folding range for an import or include
     */
    const Imports = "imports";
    /**
     * Folding range for a region (e.g. `#region`)
     */
    const Region = "region";
}

/**
 * A predefined folding range kind.
 *
 * The type is a string since the value set is extensible
 */
declare type FoldingRangeKind = string;

/**
 * Client workspace capabilities specific to folding ranges
 *
 * @since 3.18.0
 * @proposed
 */
declare interface FoldingRangeWorkspaceClientCapabilities {
    /**
     * Whether the client implementation supports a refresh request sent from the
     * server to the client.
     *
     * Note that this event is global and will force the client to refresh all
     * folding ranges currently shown. It should be used with absolute care and is
     * useful for situation where a server for example detects a project wide
     * change that requires such a calculation.
     *
     * @since 3.18.0
     * @proposed
     */
    refreshSupport?: boolean;
}

/**
 * General client capabilities.
 *
 * @since 3.16.0
 */
declare interface GeneralClientCapabilities {
    /**
     * Client capability that signals how the client
     * handles stale requests (e.g. a request
     * for which the client will not process the response
     * anymore since the information is outdated).
     *
     * @since 3.17.0
     */
    staleRequestSupport?: {
        /**
         * The client will actively cancel the request.
         */
        cancel: boolean;
        /**
         * The list of requests for which the client
         * will retry the request if it receives a
         * response with error code `ContentModified`
         */
        retryOnContentModified: string[];
    };
    /**
     * Client capabilities specific to regular expressions.
     *
     * @since 3.16.0
     */
    regularExpressions?: RegularExpressionsClientCapabilities;
    /**
     * Client capabilities specific to the client's markdown parser.
     *
     * @since 3.16.0
     */
    markdown?: MarkdownClientCapabilities;
    /**
     * The position encodings supported by the client. Client and server
     * have to agree on the same position encoding to ensure that offsets
     * (e.g. character position in a line) are interpreted the same on both
     * sides.
     *
     * To keep the protocol backwards compatible the following applies: if
     * the value 'utf-16' is missing from the array of position encodings
     * servers can assume that the client supports UTF-16. UTF-16 is
     * therefore a mandatory encoding.
     *
     * If omitted it defaults to ['utf-16'].
     *
     * Implementation considerations: since the conversion from one encoding
     * into another requires the content of the file / line the conversion
     * is best done where the file is read which is usually on the server
     * side.
     *
     * @since 3.17.0
     */
    positionEncodings?: PositionEncodingKind[];
}

declare interface GenericAstNode extends AstNode {
    [key: string]: unknown;
}

declare function getActionAtElement(element: GrammarAST.AbstractElement): GrammarAST.Action | undefined;

declare function getActionType(action: GrammarAST.Action): string | undefined;

/**
 * Returns all rules that can be reached from the topmost rules of the specified grammar (entry and hidden terminal rules).
 *
 * @param grammar The grammar that contains all rules
 * @param allTerminals Whether or not to include terminals that are referenced only by other terminals
 * @returns A list of referenced parser and terminal rules. If the grammar contains no entry rule,
 *      this function returns all rules of the specified grammar.
 */
declare function getAllReachableRules(grammar: GrammarAST.Grammar, allTerminals: boolean): Set<GrammarAST.AbstractRule>;

/**
 * Returns all parser rules which provide types which are used in the grammar as type in cross-references.
 * @param grammar the grammar to investigate
 * @returns the set of parser rules whose contributed types are used as type in cross-references
 */
declare function getAllRulesUsedForCrossReferences(grammar: GrammarAST.Grammar): Set<GrammarAST.ParserRule>;

/**
 * Walk along the hierarchy of containers from the given AST node to the root and return the first
 * node that matches the type predicate. If the start node itself matches, it is returned.
 * If no container matches, `undefined` is returned.
 */
declare function getContainerOfType<T extends AstNode>(node: AstNode | undefined, typePredicate: (n: AstNode) => n is T): T | undefined;

/**
 * Determines the grammar expression used to parse a cross-reference (usually a reference to a terminal rule).
 * A cross-reference can declare this expression explicitly in the form `[Type : Terminal]`, but if `Terminal`
 * is omitted, this function attempts to infer it from the name of the referenced `Type` (using `findNameAssignment`).
 *
 * Returns the grammar expression used to parse the given cross-reference, or `undefined` if it is not declared
 * and cannot be inferred.
 */
declare function getCrossReferenceTerminal(crossRef: GrammarAST.CrossReference): GrammarAST.AbstractElement | undefined;

/**
 * Attempts to find the CST node that belongs to the datatype element that contains the given CST node.
 *
 * @param cstNode The CST node for which to find the datatype node.
 * @returns The CST node corresponding to the datatype element, or the undefined if no such element exists.
 */
declare function getDatatypeNode(cstNode: CstNode): CstNode | undefined;

declare function getDiagnosticRange<N extends AstNode>(info: DiagnosticInfo<N, string>): Range_2;

/**
 * Retrieve the document in which the given AST node is contained. A reference to the document is
 * usually held by the root node of the AST.
 *
 * @throws an error if the node is not contained in a document.
 */
declare function getDocument<T extends AstNode = AstNode>(node: AstNode): LangiumDocument<T>;

/**
 * Returns the entry rule of the given grammar, if any. If the grammar file does not contain an entry rule,
 * the result is `undefined`.
 */
declare function getEntryRule(grammar: GrammarAST.Grammar): GrammarAST.ParserRule | undefined;

declare function getExplicitRuleType(rule: GrammarAST.AbstractRule): string | undefined;

/**
 * Returns all hidden terminal rules of the given grammar, if any.
 */
declare function getHiddenRules(grammar: GrammarAST.Grammar): GrammarAST.AbstractRule[];

declare function getInteriorNodes(start: CstNode, end: CstNode): CstNode[];

declare function getNextNode(node: CstNode, hidden?: boolean): CstNode | undefined;

declare function getPreviousNode(node: CstNode, hidden?: boolean): CstNode | undefined;

/**
 * Returns all AST nodes that are referenced by the given reference or multi-reference.
 */
declare function getReferenceNodes(reference: Reference | MultiReference): AstNode[];

/**
 * This function is used at runtime to get the actual type of the values produced by the given rule at runtime.
 * For data type rules, the name of the declared return type of the rule is returned (if any),
 * e.g. "INT_value returns number: MY_INT;" returns "number".
 * @param rule the given rule
 * @returns the name of the type of the produced values of the rule at runtime
 */
declare function getRuleType(rule: GrammarAST.AbstractRule): string;

/**
 * This function is used at development time (for code generation and the internal type system) to get the type of the AST node produced by the given rule.
 * For data type rules, the name of the rule is returned,
 * e.g. "INT_value returns number: MY_INT;" returns "INT_value".
 * @param rule the given rule
 * @returns the name of the AST node type of the rule
 */
declare function getRuleTypeName(rule: GrammarAST.AbstractRule): string;

declare function getStartlineNode(node: CstNode): CstNode;

declare function getTerminalParts(regexp: RegExp | string): Array<{
    start: string;
    end: string;
}>;

declare function getTypeName(type: GrammarAST.AbstractType | GrammarAST.Action): string;

export declare interface GitGraph extends langium_2.AstNode {
    readonly $type: 'Direction' | 'GitGraph';
    accDescr: string;
    accTitle: string;
    statements: Array<Statement>;
    title: string;
}

export declare const GitGraph: {
    readonly $type: "GitGraph";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly statements: "statements";
    readonly title: "title";
};

declare interface GitGraphAddedServices {
    parser: {
        TokenBuilder: GitGraphTokenBuilder;
        ValueConverter: CommonValueConverter;
    };
}

export declare const GitGraphGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'GitGraphGrammar' language. */
declare namespace GitGraphGrammar {
    const Terminals: {
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        INT: RegExp;
        STRING: RegExp;
        NEWLINE: RegExp;
        WHITESPACE: RegExp;
        YAML: RegExp;
        DIRECTIVE: RegExp;
        SINGLE_LINE_COMMENT: RegExp;
        REFERENCE: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = ":" | "BT" | "HIGHLIGHT" | "LR" | "NORMAL" | "REVERSE" | "TB" | "branch" | "checkout" | "cherry-pick" | "commit" | "gitGraph" | "gitGraph:" | "id:" | "merge" | "msg:" | "order:" | "parent:" | "switch" | "tag:" | "type:";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Branch: Branch;
        Checkout: Checkout;
        CherryPicking: CherryPicking;
        Commit: Commit;
        Direction: Direction;
        GitGraph: GitGraph;
        Merge: Merge;
        Statement: Statement;
    };
}

export declare const GitGraphModule: Module<GitGraphServices, PartialLangiumCoreServices & GitGraphAddedServices>;

export declare type GitGraphServices = LangiumCoreServices & GitGraphAddedServices;

declare class GitGraphTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare interface Grammar extends langium.AstNode {
    readonly $type: 'Grammar';
    imports: Array<GrammarImport>;
    interfaces: Array<Interface>;
    isDeclared: boolean;
    name?: string;
    rules: Array<AbstractRule>;
    types: Array<Type>;
}

declare const Grammar: {
    readonly $type: "Grammar";
    readonly imports: "imports";
    readonly interfaces: "interfaces";
    readonly isDeclared: "isDeclared";
    readonly name: "name";
    readonly rules: "rules";
    readonly types: "types";
};

declare namespace GrammarAST {
    export {
        isAbstractElement,
        isAbstractParserRule,
        isAbstractRule,
        isAbstractType,
        isAction,
        isAlternatives,
        isArrayLiteral,
        isArrayType,
        isAssignment,
        isBooleanLiteral,
        isCharacterRange,
        isCondition,
        isConjunction,
        isCrossReference,
        isDisjunction,
        isEndOfFile,
        isGrammar,
        isGrammarImport,
        isGroup,
        isInferredType,
        isInfixRule,
        isInfixRuleOperatorList,
        isInfixRuleOperators,
        isInterface,
        isKeyword,
        isNamedArgument,
        isNegatedToken,
        isNegation,
        isNumberLiteral,
        isParameter,
        isParameterReference,
        isParserRule,
        isReferenceType,
        isRegexToken,
        isReturnType,
        isRuleCall,
        isSimpleType,
        isStringLiteral,
        isTerminalAlternatives,
        isTerminalElement,
        isTerminalGroup,
        isTerminalRule,
        isTerminalRuleCall,
        isType,
        isTypeAttribute,
        isTypeDefinition,
        isUnionType,
        isUnorderedGroup,
        isUntilToken,
        isValueLiteral,
        isWildcard,
        LangiumGrammarTerminals,
        LangiumGrammarTerminalNames,
        LangiumGrammarKeywordNames,
        LangiumGrammarTokenNames,
        AbstractElement,
        AbstractParserRule,
        AbstractRule,
        AbstractType,
        Action,
        Alternatives,
        ArrayLiteral,
        ArrayType,
        Assignment,
        Associativity,
        BooleanLiteral,
        CharacterRange,
        Condition,
        Conjunction,
        CrossReference,
        Disjunction,
        EndOfFile,
        FeatureName,
        Grammar,
        GrammarImport,
        Group,
        InferredType,
        InfixRule,
        InfixRuleOperatorList,
        InfixRuleOperators,
        Interface,
        Keyword,
        NamedArgument,
        NegatedToken,
        Negation,
        NumberLiteral,
        Parameter,
        ParameterReference,
        ParserRule,
        PrimitiveType,
        ReferenceType,
        RegexToken,
        ReturnType_2 as ReturnType,
        RuleCall,
        SimpleType,
        StringLiteral,
        TerminalAlternatives,
        TerminalElement,
        TerminalGroup,
        TerminalRule,
        TerminalRuleCall,
        Type,
        TypeAttribute,
        TypeDefinition,
        UnionType,
        UnorderedGroup,
        UntilToken,
        ValueLiteral,
        Wildcard,
        LangiumGrammarAstType,
        LangiumGrammarAstReflection,
        reflection
    }
}

declare interface GrammarConfig {
    /**
     * Lists all rule names which are classified as multiline comment rules
     */
    multilineCommentRules: string[];
    /**
     * A regular expression which matches characters of names
     */
    nameRegexp: RegExp;
}

declare interface GrammarImport extends langium.AstNode {
    readonly $container: Grammar;
    readonly $type: 'GrammarImport';
    path: string;
}

declare const GrammarImport: {
    readonly $type: "GrammarImport";
    readonly path: "path";
};

declare namespace GrammarUtils {
    export {
        getEntryRule,
        getHiddenRules,
        getAllReachableRules,
        getAllRulesUsedForCrossReferences,
        getCrossReferenceTerminal,
        isCommentTerminal,
        findNodesForProperty,
        findNodeForProperty,
        findNodesForKeyword,
        findNodeForKeyword,
        findNodesForKeywordInternal,
        findAssignment,
        findNameAssignment,
        getActionAtElement,
        isOptionalCardinality,
        isArrayCardinality,
        isArrayOperator,
        isDataTypeRule,
        isDataType,
        getExplicitRuleType,
        getTypeName,
        getActionType,
        getRuleTypeName,
        getRuleType,
        terminalRegex,
        Cardinality,
        Operator
    }
}

declare interface Group extends AbstractElement {
    readonly $type: 'Group';
    elements: Array<AbstractElement>;
    guardCondition?: Condition;
    predicate?: '->' | '=>';
}

declare const Group: {
    readonly $type: "Group";
    readonly cardinality: "cardinality";
    readonly elements: "elements";
    readonly guardCondition: "guardCondition";
    readonly predicate: "predicate";
};

declare interface Group_2 extends langium_2.AstNode {
    readonly $container: Architecture;
    readonly $type: 'EOL' | 'Group';
    icon?: string;
    id: string;
    in: string;
    title?: string;
}

declare const Group_2: {
    readonly $type: "Group";
    readonly icon: "icon";
    readonly id: "id";
    readonly in: "in";
    readonly title: "title";
};

/**
 * Walk along the hierarchy of containers from the given AST node to the root and check for existence
 * of a container that matches the given predicate. The start node is included in the checks.
 */
declare function hasContainerOfType(node: AstNode | undefined, predicate: (n: AstNode) => boolean): boolean;

declare interface HoverClientCapabilities {
    /**
     * Whether hover supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * Client supports the following content formats for the content
     * property. The order describes the preferred format of the client.
     */
    contentFormat?: MarkupKind[];
}

declare interface HydrateContext {
    astNodes: Map<any, AstNode>;
    cstNodes: Map<any, CstNode>;
}

/**
 * The hydrator service is responsible for allowing AST parse results to be sent across worker threads.
 */
declare interface Hydrator {
    /**
     * Converts a parse result to a plain object. The resulting object can be sent across worker threads.
     */
    dehydrate(result: ParseResult<AstNode>): ParseResult<object>;
    /**
     * Converts a plain object to a parse result. The included AST node can then be used in the main thread.
     * Calling this method on objects that have not been dehydrated first will result in undefined behavior.
     */
    hydrate<T extends AstNode = AstNode>(result: ParseResult<object>): ParseResult<T>;
}

/**
 * @since 3.6.0
 */
declare interface ImplementationClientCapabilities {
    /**
     * Whether implementation supports dynamic registration. If this is set to `true`
     * the client supports the new `ImplementationRegistrationOptions` return value
     * for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
    /**
     * The client supports additional metadata in the form of definition links.
     *
     * @since 3.14.0
     */
    linkSupport?: boolean;
}

declare type IndentationAwareDelimiter<TokenName extends string> = [begin: TokenName, end: TokenName];

/**
 * A lexer that is aware of indentation in the input text.
 * The only purpose of this lexer is to reset the internal state of the {@link IndentationAwareTokenBuilder}
 * between the tokenization of different text inputs.
 *
 * In your module, you can override the default lexer with this one as such:
 * ```ts
 * parser: {
 *    TokenBuilder: () => new IndentationAwareTokenBuilder(),
 *    Lexer: (services) => new IndentationAwareLexer(services),
 * }
 * ```
 */
declare class IndentationAwareLexer extends DefaultLexer {
    protected readonly indentationTokenBuilder: IndentationAwareTokenBuilder;
    constructor(services: LangiumCoreServices);
    tokenize(text: string, options?: TokenizeOptions): LexerResult;
}

/**
 * A token builder that is sensitive to indentation in the input text.
 * It will generate tokens for indentation and dedentation based on the indentation level.
 *
 * The first generic parameter corresponds to the names of terminal tokens,
 * while the second one corresponds to the names of keyword tokens.
 * Both parameters are optional and can be imported from `./generated/ast.js`.
 *
 * Inspired by https://github.com/chevrotain/chevrotain/blob/master/examples/lexer/python_indentation/python_indentation.js
 */
declare class IndentationAwareTokenBuilder<Terminals extends string = string, KeywordName extends string = string> extends DefaultTokenBuilder {
    /**
     * The stack stores all the previously matched indentation levels to understand how deeply the next tokens are nested.
     * The stack is valid for lexing
     */
    protected indentationStack: number[];
    readonly options: IndentationTokenBuilderOptions<Terminals, KeywordName>;
    /**
     * The token type to be used for indentation tokens
     */
    readonly indentTokenType: TokenType;
    /**
     * The token type to be used for dedentation tokens
     */
    readonly dedentTokenType: TokenType;
    /**
     * A regular expression to match a series of tabs and/or spaces.
     * Override this to customize what the indentation is allowed to consist of.
     */
    protected whitespaceRegExp: RegExp;
    constructor(options?: Partial<IndentationTokenBuilderOptions<NoInfer<Terminals>, NoInfer<KeywordName>>>);
    buildTokens(grammar: Grammar, options?: TokenBuilderOptions | undefined): TokenVocabulary;
    flushLexingReport(text: string): IndentationLexingReport;
    /**
     * Helper function to check if the current position is the start of a new line.
     *
     * @param text The full input string.
     * @param offset The current position at which to check
     * @returns Whether the current position is the start of a new line
     */
    protected isStartOfLine(text: string, offset: number): boolean;
    /**
     * A helper function used in matching both indents and dedents.
     *
     * @param text The full input string.
     * @param offset The current position at which to attempt a match
     * @param tokens Previously scanned tokens
     * @param groups Token Groups
     * @returns The current and previous indentation levels and the matched whitespace
     */
    protected matchWhitespace(text: string, offset: number, tokens: IToken[], groups: Record<string, IToken[]>): {
        currIndentLevel: number;
        prevIndentLevel: number;
        match: RegExpExecArray | null;
    };
    /**
     * Helper function to create an instance of an indentation token.
     *
     * @param tokenType Indent or dedent token type
     * @param text Full input string, used to calculate the line number
     * @param image The original image of the token (tabs or spaces)
     * @param offset Current position in the input string
     * @returns The indentation token instance
     */
    protected createIndentationTokenInstance(tokenType: TokenType, text: string, image: string, offset: number): IToken;
    /**
     * Helper function to get the line number at a given offset.
     *
     * @param text Full input string, used to calculate the line number
     * @param offset Current position in the input string
     * @returns The line number at the given offset
     */
    protected getLineNumber(text: string, offset: number): number;
    /**
     * A custom pattern for matching indents
     *
     * @param text The full input string.
     * @param offset The offset at which to attempt a match
     * @param tokens Previously scanned tokens
     * @param groups Token Groups
     */
    protected indentMatcher(text: string, offset: number, tokens: IToken[], groups: Record<string, IToken[]>): ReturnType<CustomPatternMatcherFunc>;
    /**
     * A custom pattern for matching dedents
     *
     * @param text The full input string.
     * @param offset The offset at which to attempt a match
     * @param tokens Previously scanned tokens
     * @param groups Token Groups
     */
    protected dedentMatcher(text: string, offset: number, tokens: IToken[], groups: Record<string, IToken[]>): ReturnType<CustomPatternMatcherFunc>;
    protected buildTerminalToken(terminal: TerminalRule): TokenType;
    /**
     * Resets the indentation stack between different runs of the lexer
     *
     * @param text Full text that was tokenized
     * @returns Remaining dedent tokens to match all previous indents at the end of the file
     */
    flushRemainingDedents(text: string): IToken[];
}

declare const indentationBuilderDefaultOptions: IndentationTokenBuilderOptions;

declare interface IndentationLexingReport extends LexingReport {
    /** Dedent tokens that are necessary to close the remaining indents. */
    remainingDedents: IToken[];
}

declare interface IndentationTokenBuilderOptions<TerminalName extends string = string, KeywordName extends string = string> {
    /**
     * The name of the token used to denote indentation in the grammar.
     * A possible definition in the grammar could look like this:
     * ```langium
     * terminal INDENT: ':synthetic-indent:';
     * ```
     *
     * @default 'INDENT'
     */
    indentTokenName: TerminalName;
    /**
     * The name of the token used to denote deindentation in the grammar.
     * A possible definition in the grammar could look like this:
     * ```langium
     * terminal DEDENT: ':synthetic-dedent:';
     * ```
     *
     * @default 'DEDENT'
     */
    dedentTokenName: TerminalName;
    /**
     * The name of the token used to denote whitespace other than indentation and newlines in the grammar.
     * A possible definition in the grammar could look like this:
     * ```langium
     * hidden terminal WS: /[ \t]+/;
     * ```
     *
     * @default 'WS'
     */
    whitespaceTokenName: TerminalName;
    /**
     * The delimiter tokens inside of which indentation should be ignored and treated as normal whitespace.
     * For example, Python doesn't treat any whitespace between `(` and `)` as significant.
     *
     * Can be either terminal tokens or keyword tokens.
     *
     * @default []
     */
    ignoreIndentationDelimiters: Array<IndentationAwareDelimiter<TerminalName | KeywordName>>;
}

/**
 * The index manager is responsible for keeping metadata about symbols and cross-references
 * in the workspace. It is used to look up symbols in the global scope, mostly during linking
 * and completion. This service is shared between all languages of a language server.
 */
declare interface IndexManager {
    /**
     * Remove the specified document URI from the index.
     * Necessary when documents are deleted and not referenceable anymore.
     *
     * @param uri The URI of the document for which index data shall be removed
     */
    remove(uri: URI): void;
    /**
     * Remove only the information about the exportable content of a document.
     */
    removeContent(uri: URI): void;
    /**
     * Remove only the information about the cross-references of a document.
     */
    removeReferences(uri: URI): void;
    /**
     * Update the information about the exportable content of a document inside the index.
     *
     * @param document Document to be updated
     * @param cancelToken Indicates when to cancel the current operation.
     * @throws `OperationCanceled` if a user action occurs during execution
     */
    updateContent(document: LangiumDocument, cancelToken?: CancellationToken): Promise<void>;
    /**
     * Update the information about the cross-references of a document inside the index.
     *
     * @param document Document to be updated
     * @param cancelToken Indicates when to cancel the current operation.
     * @throws `OperationCanceled` if a user action occurs during execution
     */
    updateReferences(document: LangiumDocument, cancelToken?: CancellationToken): Promise<void>;
    /**
     * Determine whether the given document could be affected by changes of the documents
     * identified by the given URIs (second parameter). The document is typically regarded as
     * affected if it contains a reference to any of the changed files.
     *
     * @param document Document to check whether it's affected
     * @param changedUris URIs of the changed documents
     */
    isAffected(document: LangiumDocument, changedUris: Set<string>): boolean;
    /**
     * Compute a list of all exported elements, optionally filtered using a type identifier and document URIs.
     *
     * @param nodeType The type to filter with, or `undefined` to return descriptions of all types.
     * @param uris If specified, only returns elements from the given URIs.
     * @returns a `Stream` containing all globally visible nodes (of a given type).
     */
    allElements(nodeType?: string, uris?: Set<string>): Stream<AstNodeDescription>;
    /**
     * Returns all known references that are pointing to the given `targetNode`.
     *
     * @param targetNode the `AstNode` to look up references for
     * @param astNodePath the path that points to the `targetNode` inside the document. See also `AstNodeLocator`
     *
     * @returns a `Stream` of references that are targeting the `targetNode`
     */
    findAllReferences(targetNode: AstNode, astNodePath: string): Stream<ReferenceDescription>;
}

declare interface InferredType extends langium.AstNode {
    readonly $container: Action | InfixRule | ParserRule;
    readonly $type: 'InferredType';
    name: string;
}

declare const InferredType: {
    readonly $type: "InferredType";
    readonly name: "name";
};

declare interface InfixRule extends langium.AstNode {
    readonly $container: Grammar;
    readonly $type: 'InfixRule';
    call: RuleCall;
    dataType?: PrimitiveType;
    inferredType?: InferredType;
    name: string;
    operators: InfixRuleOperators;
    parameters: Array<Parameter>;
    returnType?: langium.Reference<AbstractType>;
}

declare const InfixRule: {
    readonly $type: "InfixRule";
    readonly call: "call";
    readonly dataType: "dataType";
    readonly inferredType: "inferredType";
    readonly name: "name";
    readonly operators: "operators";
    readonly parameters: "parameters";
    readonly returnType: "returnType";
};

declare interface InfixRuleOperatorList extends langium.AstNode {
    readonly $container: InfixRuleOperators;
    readonly $type: 'InfixRuleOperatorList';
    associativity?: Associativity;
    operators: Array<Keyword>;
}

declare const InfixRuleOperatorList: {
    readonly $type: "InfixRuleOperatorList";
    readonly associativity: "associativity";
    readonly operators: "operators";
};

declare interface InfixRuleOperators extends langium.AstNode {
    readonly $container: InfixRule;
    readonly $type: 'InfixRuleOperators';
    precedences: Array<InfixRuleOperatorList>;
}

declare const InfixRuleOperators: {
    readonly $type: "InfixRuleOperators";
    readonly precedences: "precedences";
};

export declare interface Info extends langium_2.AstNode {
    readonly $type: 'Info';
    accDescr?: string;
    accTitle?: string;
    title?: string;
}

export declare const Info: {
    readonly $type: "Info";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly title: "title";
};

/**
 * Declaration of `Info` services.
 */
declare interface InfoAddedServices {
    parser: {
        TokenBuilder: InfoTokenBuilder;
        ValueConverter: CommonValueConverter;
    };
}

export declare const InfoGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'InfoGrammar' language. */
declare namespace InfoGrammar {
    const Terminals: {
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        NEWLINE: RegExp;
        WHITESPACE: RegExp;
        YAML: RegExp;
        DIRECTIVE: RegExp;
        SINGLE_LINE_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "info" | "showInfo";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Info: Info;
    };
}

/**
 * Dependency injection module that overrides Langium default services and
 * contributes the declared `Info` services.
 */
export declare const InfoModule: Module<InfoServices, PartialLangiumCoreServices & InfoAddedServices>;

/**
 * Union of Langium default services and `Info` services.
 */
export declare type InfoServices = LangiumCoreServices & InfoAddedServices;

declare class InfoTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare interface InitializedParams {
}

declare type InitializeParams = _InitializeParams & WorkspaceFoldersInitializeParams;

/**
 * The initialize parameters
 */
declare interface _InitializeParams extends WorkDoneProgressParams {
    /**
     * The process Id of the parent process that started
     * the server.
     *
     * Is `null` if the process has not been started by another process.
     * If the parent process is not alive then the server should exit.
     */
    processId: integer_2 | null;
    /**
     * Information about the client
     *
     * @since 3.15.0
     */
    clientInfo?: {
        /**
         * The name of the client as defined by the client.
         */
        name: string;
        /**
         * The client's version as defined by the client.
         */
        version?: string;
    };
    /**
     * The locale the client is currently showing the user interface
     * in. This must not necessarily be the locale of the operating
     * system.
     *
     * Uses IETF language tags as the value's syntax
     * (See https://en.wikipedia.org/wiki/IETF_language_tag)
     *
     * @since 3.16.0
     */
    locale?: string;
    /**
     * The rootPath of the workspace. Is null
     * if no folder is open.
     *
     * @deprecated in favour of rootUri.
     */
    rootPath?: string | null;
    /**
     * The rootUri of the workspace. Is null if no
     * folder is open. If both `rootPath` and `rootUri` are set
     * `rootUri` wins.
     *
     * @deprecated in favour of workspaceFolders.
     */
    rootUri: DocumentUri_3 | null;
    /**
     * The capabilities provided by the client (editor or tool)
     */
    capabilities: ClientCapabilities;
    /**
     * User provided initialization options.
     */
    initializationOptions?: LSPAny_2;
    /**
     * The initial trace setting. If omitted trace is disabled ('off').
     */
    trace?: TraceValues;
}

/**
 * Given a set of modules, the inject function returns a lazily evaluated injector
 * that injects dependencies into the requested service when it is requested the
 * first time. Subsequent requests will return the same service.
 *
 * In the case of cyclic dependencies, an Error will be thrown. This can be fixed
 * by injecting a provider `() => T` instead of a `T`.
 *
 * Please note that the arguments may be objects or arrays. However, the result will
 * be an object. Using it with for..of will have no effect.
 *
 * @param module1 first Module
 * @param module2 (optional) second Module
 * @param module3 (optional) third Module
 * @param module4 (optional) fourth Module
 * @param module5 (optional) fifth Module
 * @param module6 (optional) sixth Module
 * @param module7 (optional) seventh Module
 * @param module8 (optional) eighth Module
 * @param module9 (optional) ninth Module
 * @returns a new object of type I
 */
declare function inject<I1, I2, I3, I4, I5, I6, I7, I8, I9, I extends I1 & I2 & I3 & I4 & I5 & I6 & I7 & I8 & I9>(module1: Module<I, I1>, module2?: Module<I, I2>, module3?: Module<I, I3>, module4?: Module<I, I4>, module5?: Module<I, I5>, module6?: Module<I, I6>, module7?: Module<I, I7>, module8?: Module<I, I8>, module9?: Module<I, I9>): I;

/**
 * Inlay hint client capabilities.
 *
 * @since 3.17.0
 */
declare type InlayHintClientCapabilities = {
    /**
     * Whether inlay hints support dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * Indicates which properties a client can resolve lazily on an inlay
     * hint.
     */
    resolveSupport?: {
        /**
         * The properties that a client can resolve lazily.
         */
        properties: string[];
    };
};

/**
 * Client workspace capabilities specific to inlay hints.
 *
 * @since 3.17.0
 */
declare type InlayHintWorkspaceClientCapabilities = {
    /**
     * Whether the client implementation supports a refresh request sent from
     * the server to the client.
     *
     * Note that this event is global and will force the client to refresh all
     * inlay hints currently shown. It should be used with absolute care and
     * is useful for situation where a server for example detects a project wide
     * change that requires such a calculation.
     */
    refreshSupport?: boolean;
};

/**
 * Client capabilities specific to inline completions.
 *
 * @since 3.18.0
 * @proposed
 */
declare type InlineCompletionClientCapabilities = {
    /**
     * Whether implementation supports dynamic registration for inline completion providers.
     */
    dynamicRegistration?: boolean;
};

/**
 * Client capabilities specific to inline values.
 *
 * @since 3.17.0
 */
declare type InlineValueClientCapabilities = {
    /**
     * Whether implementation supports dynamic registration for inline value providers.
     */
    dynamicRegistration?: boolean;
};

/**
 * Client workspace capabilities specific to inline values.
 *
 * @since 3.17.0
 */
declare type InlineValueWorkspaceClientCapabilities = {
    /**
     * Whether the client implementation supports a refresh request sent from the
     * server to the client.
     *
     * Note that this event is global and will force the client to refresh all
     * inline values currently shown. It should be used with absolute care and is
     * useful for situation where a server for example detects a project wide
     * change that requires such a calculation.
     */
    refreshSupport?: boolean;
};

declare function inRange(range: Range_2, to: Range_2): boolean;

/**
 * How whitespace and indentation is handled during completion
 * item insertion.
 *
 * @since 3.16.0
 */
declare namespace InsertTextMode {
    /**
     * The insertion or replace strings is taken as it is. If the
     * value is multi line the lines below the cursor will be
     * inserted using the indentation defined in the string value.
     * The client will not apply any kind of adjustments to the
     * string.
     */
    const asIs: 1;
    /**
     * The editor adjusts leading whitespace of new lines so that
     * they match the indentation up to the cursor of the line for
     * which the item is accepted.
     *
     * Consider a line like this: <2tabs><cursor><3tabs>foo. Accepting a
     * multi line completion item is indented using 2 tabs and all
     * following lines inserted will be indented using 2 tabs as well.
     */
    const adjustIndentation: 2;
}

declare type InsertTextMode = 1 | 2;

/**
 * Defines an integer in the range of -2^31 to 2^31 - 1.
 */
declare type integer = number;

declare namespace integer {
    const MIN_VALUE = -2147483648;
    const MAX_VALUE = 2147483647;
    function is(value: any): value is integer;
}

/**
 * Defines an integer in the range of -2^31 to 2^31 - 1.
 */
declare type integer_2 = number;

declare namespace integer_2 {
    const MIN_VALUE = -2147483648;
    const MAX_VALUE = 2147483647;
    function is(value: any): value is integer_2;
}

declare interface Interface extends langium.AstNode {
    readonly $container: Grammar;
    readonly $type: 'Interface';
    attributes: Array<TypeAttribute>;
    name: string;
    superTypes: Array<langium.Reference<AbstractType>>;
}

declare const Interface: {
    readonly $type: "Interface";
    readonly attributes: "attributes";
    readonly name: "name";
    readonly superTypes: "superTypes";
};

/**
 * A cross-reference in the serialized JSON representation of an AstNode.
 */
declare interface IntermediateReference {
    /** URI pointing to the target element. This is either `#${path}` if the target is in the same document, or `${documentURI}#${path}` otherwise. */
    $ref?: string;
    /** URI pointing to the target elements. This is the multi reference equivalent for {@link $ref}. */
    $refs?: string[];
    /** The actual text used to look up the reference target in the surrounding scope. */
    $refText?: string;
    /** If any problem occurred while resolving the reference, it is described by this property. */
    $error?: string;
}

declare interface InternalUriTrieNode<T> {
    name: string;
    children: Map<string, InternalUriTrieNode<T>>;
    parent?: InternalUriTrieNode<T>;
    element?: T;
}

/**
 * This function does two things:
 *  1. Check the elapsed time since the last call to this function or to `startCancelableOperation`. If the predefined
 *     period (configured with `setInterruptionPeriod`) is exceeded, execution is delayed with `delayNextTick`.
 *  2. If the predefined period is not met yet or execution is resumed after an interruption, the given cancellation
 *     token is checked, and if cancellation is requested, `OperationCanceled` is thrown.
 *
 * All services in Langium that receive a `CancellationToken` may potentially call this function, so the
 * `CancellationToken` must be caught (with an `async` try-catch block or a `catch` callback attached to
 * the promise) to avoid that event being exposed as an error.
 */
declare function interruptAndCheck(token: CancellationToken): Promise<void>;

declare interface IParserDefinitionError {
    message: string;
    type: number;
    ruleName?: string;
}

declare function isAbstractElement(item: unknown): item is AbstractElement;

declare function isAbstractParserRule(item: unknown): item is AbstractParserRule;

declare function isAbstractRule(item: unknown): item is AbstractRule;

declare function isAbstractType(item: unknown): item is AbstractType;

declare function isAction(item: unknown): item is Action;

declare function isAlternatives(item: unknown): item is Alternatives;

export declare function isArchitecture(item: unknown): item is Architecture;

declare function isArrayCardinality(cardinality?: Cardinality): boolean;

declare function isArrayLiteral(item: unknown): item is ArrayLiteral;

declare function isArrayOperator(operator?: Operator): boolean;

declare function isArrayType(item: unknown): item is ArrayType;

declare function isAssignment(item: unknown): item is Assignment;

declare function isAstNode(obj: unknown): obj is AstNode;

declare function isAstNodeDescription(obj: unknown): obj is AstNodeDescription;

declare function isAstNodeWithComment(node: AstNode): node is AstNodeWithComment;

declare function isBooleanLiteral(item: unknown): item is BooleanLiteral;

export declare function isBranch(item: unknown): item is Branch;

declare function isCharacterRange(item: unknown): item is CharacterRange;

/**
 * Determines whether the specified cst node is a child of the specified parent node.
 */
declare function isChildNode(child: CstNode, parent: CstNode): boolean;

declare function isCommentNode(cstNode: CstNode, commentNames: string[]): boolean;

/**
 * Determines whether the given terminal rule represents a comment. This is true if the rule is marked
 * as `hidden` and it does not match white space. This means every hidden token (i.e. excluded from the AST)
 * that contains visible characters is considered a comment.
 */
declare function isCommentTerminal(terminalRule: GrammarAST.TerminalRule): boolean;

export declare function isCommit(item: unknown): item is Commit;

declare function isCompositeCstNode(node: unknown): node is CompositeCstNode;

declare function isCondition(item: unknown): item is Condition;

declare function isConjunction(item: unknown): item is Conjunction;

declare function isCrossReference(item: unknown): item is CrossReference;

export declare function isCynefin(item: unknown): item is Cynefin;

declare function isDataType(type: GrammarAST.Type): boolean;

/**
 * Determines whether the given parser rule is a _data type rule_, meaning that it has a
 * primitive return type like `number`, `boolean`, etc.
 */
declare function isDataTypeRule(rule: GrammarAST.ParserRule): boolean;

declare function isDisjunction(item: unknown): item is Disjunction;

export declare function isDomainBlock(item: unknown): item is DomainBlock;

export declare function isDomainItem(item: unknown): item is DomainItem;

export declare function isEmModelEntityType(item: unknown): item is EmModelEntityType;

export declare function isEmResetFrame(item: unknown): item is EmResetFrame;

declare function isEndOfFile(item: unknown): item is EndOfFile;

export declare function isGitGraph(item: unknown): item is GitGraph;

declare function isGrammar(item: unknown): item is Grammar;

declare function isGrammarImport(item: unknown): item is GrammarImport;

declare function isGroup(item: unknown): item is Group;

/**
 * Returns a check whether the given TokenVocabulary is IMultiModeLexerDefinition
 */
declare function isIMultiModeLexerDefinition(tokenVocabulary: TokenVocabulary): tokenVocabulary is IMultiModeLexerDefinition;

declare function isInferredType(item: unknown): item is InferredType;

declare function isInfixRule(item: unknown): item is InfixRule;

declare function isInfixRuleOperatorList(item: unknown): item is InfixRuleOperatorList;

declare function isInfixRuleOperators(item: unknown): item is InfixRuleOperators;

export declare function isInfo(item: unknown): item is Info;

declare function isInterface(item: unknown): item is Interface;

declare function isJSDoc(node: CstNode | string, options?: JSDocParseOptions): boolean;

declare function isKeyword(item: unknown): item is Keyword;

declare function isLeafCstNode(node: unknown): node is LeafCstNode;

declare function isLinkingError(obj: unknown): obj is LinkingError;

export declare function isMerge(item: unknown): item is Merge;

declare function isMultilineComment(regexp: RegExp | string): boolean;

declare function isMultiReference(obj: unknown): obj is MultiReference;

declare function isNamed(node: AstNode): node is NamedAstNode;

declare function isNamedArgument(item: unknown): item is NamedArgument;

declare function isNegatedToken(item: unknown): item is NegatedToken;

declare function isNegation(item: unknown): item is Negation;

declare function isNumberLiteral(item: unknown): item is NumberLiteral;

/**
 * Use this in a `catch` block to check whether the thrown object indicates that the operation
 * has been cancelled.
 */
declare function isOperationCancelled(err: unknown): err is typeof OperationCancelled;

declare function isOptionalCardinality(cardinality?: Cardinality, element?: GrammarAST.AbstractElement): boolean;

export declare function isPacket(item: unknown): item is Packet;

export declare function isPacketBlock(item: unknown): item is PacketBlock;

declare function isParameter(item: unknown): item is Parameter;

declare function isParameterReference(item: unknown): item is ParameterReference;

declare function isParserRule(item: unknown): item is ParserRule;

export declare function isPie(item: unknown): item is Pie;

export declare function isPieSection(item: unknown): item is PieSection;

export declare function isRailroad(item: unknown): item is Railroad;

export declare function isRailroadAbnf(item: unknown): item is RailroadAbnf;

export declare function isRailroadEbnf(item: unknown): item is RailroadEbnf;

export declare function isRailroadPeg(item: unknown): item is RailroadPeg;

declare function isReference(obj: unknown): obj is Reference;

declare function isReferenceType(item: unknown): item is ReferenceType;

declare function isRegexToken(item: unknown): item is RegexToken;

declare function isReturnType(item: unknown): item is ReturnType_2;

declare function isRootCstNode(node: unknown): node is RootCstNode;

declare function isRuleCall(item: unknown): item is RuleCall;

declare function isSimpleType(item: unknown): item is SimpleType;

declare function isStringLiteral(item: unknown): item is StringLiteral;

declare function isTerminalAlternatives(item: unknown): item is TerminalAlternatives;

declare function isTerminalElement(item: unknown): item is TerminalElement;

declare function isTerminalGroup(item: unknown): item is TerminalGroup;

declare function isTerminalRule(item: unknown): item is TerminalRule;

declare function isTerminalRuleCall(item: unknown): item is TerminalRuleCall;

/**
 * Returns a check whether the given TokenVocabulary is TokenType array
 */
declare function isTokenTypeArray(tokenVocabulary: TokenVocabulary): tokenVocabulary is TokenType[];

/**
 * Returns a check whether the given TokenVocabulary is TokenTypeDictionary
 */
declare function isTokenTypeDictionary(tokenVocabulary: TokenVocabulary): tokenVocabulary is TokenTypeDictionary;

export declare function isTransition(item: unknown): item is Transition;

export declare function isTreemap(item: unknown): item is Treemap;

declare function isType(item: unknown): item is Type;

declare function isTypeAttribute(item: unknown): item is TypeAttribute;

declare function isTypeDefinition(item: unknown): item is TypeDefinition;

declare function isUnionType(item: unknown): item is UnionType;

declare function isUnorderedGroup(item: unknown): item is UnorderedGroup;

declare function isUntilToken(item: unknown): item is UntilToken;

declare function isValueLiteral(item: unknown): item is ValueLiteral;

export declare function isWardley(item: unknown): item is Wardley;

declare function isWhitespace(value: RegExp | string): boolean;

declare function isWildcard(item: unknown): item is Wildcard;

declare interface Item extends langium_2.AstNode {
    readonly $type: 'Item' | 'Leaf' | 'Section';
    classSelector?: string;
    name: string;
}

declare const Item: {
    readonly $type: "Item";
    readonly classSelector: "classSelector";
    readonly name: "name";
};

declare interface JSDocComment extends JSDocValue {
    readonly elements: JSDocElement[];
    getTag(name: string): JSDocTag | undefined;
    getTags(name: string): JSDocTag[];
}

declare class JSDocDocumentationProvider implements DocumentationProvider {
    protected readonly indexManager: IndexManager;
    protected readonly commentProvider: CommentProvider;
    constructor(services: LangiumCoreServices);
    getDocumentation(node: AstNode): string | undefined;
    protected documentationLinkRenderer(node: AstNode, name: string, display: string): string | undefined;
    protected documentationTagRenderer(_node: AstNode, _tag: JSDocTag): string | undefined;
    protected findNameInLocalSymbols(node: AstNode, name: string): AstNodeDescription | undefined;
    protected findNameInGlobalScope(node: AstNode, name: string): AstNodeDescription | undefined;
}

declare type JSDocElement = JSDocParagraph | JSDocTag;

declare type JSDocInline = JSDocTag | JSDocLine;

declare interface JSDocLine extends JSDocValue {
    readonly text: string;
}

declare interface JSDocParagraph extends JSDocValue {
    readonly inlines: JSDocInline[];
}

declare interface JSDocParseOptions {
    /**
     * The start symbol of your comment format. Defaults to `/**`.
     */
    readonly start?: RegExp | string;
    /**
     * The symbol that start a line of your comment format. Defaults to `*`.
     */
    readonly line?: RegExp | string;
    /**
     * The end symbol of your comment format. Defaults to `*\/`.
     */
    readonly end?: RegExp | string;
}

declare interface JSDocRenderOptions {
    /**
     * Determines the style for rendering tags. Defaults to `italic`.
     */
    tag?: 'plain' | 'italic' | 'bold' | 'bold-italic';
    /**
     * Determines the default for rendering `@link` tags. Defaults to `plain`.
     */
    link?: 'code' | 'plain';
    /**
     * Custom tag rendering function.
     * Return a markdown formatted tag or `undefined` to fall back to the default rendering.
     */
    renderTag?(tag: JSDocTag): string | undefined;
    /**
     * Custom link rendering function. Accepts a link target and a display value for the link.
     * Return a markdown formatted link with the format `[$display]($link)` or `undefined` if the link is not a valid target.
     */
    renderLink?(link: string, display: string): string | undefined;
}

declare interface JSDocTag extends JSDocValue {
    readonly name: string;
    readonly content: JSDocParagraph;
    readonly inline: boolean;
}

declare interface JSDocValue {
    /**
     * Represents the range that this JSDoc element occupies.
     * If the JSDoc was parsed from a `CstNode`, the range will represent the location in the source document.
     */
    readonly range: Range_2;
    /**
     * Renders this JSDoc element to a plain text representation.
     */
    toString(): string;
    /**
     * Renders this JSDoc element to a markdown representation.
     *
     * @param options Rendering options to customize the markdown result.
     */
    toMarkdown(options?: JSDocRenderOptions): string;
}

declare interface JsonDeserializeOptions {
    /** Used to parse and convert URIs when the target of a cross-reference is in a different document. */
    uriConverter?: (uri: string) => URI;
}

declare interface JsonSerializeOptions {
    /** The space parameter for `JSON.stringify`, controlling whether and how to pretty-print the output. */
    space?: string | number;
    /** Whether to include the `$refText` property for references (the name used to identify the target node). */
    refText?: boolean;
    /** Whether to include the `$sourceText` property, which holds the full source text from which an AST node was parsed. */
    sourceText?: boolean;
    /** Whether to include the `$textRegion` property, which holds information to trace AST node properties to their respective source text regions. */
    textRegions?: boolean;
    /** Whether to include the `$comment` property, which holds comments according to the CommentProvider service. */
    comments?: boolean;
    /** The replacer parameter for `JSON.stringify`; the default replacer given as parameter should be used to apply basic replacements. */
    replacer?: (key: string, value: unknown, defaultReplacer: (key: string, value: unknown) => unknown) => unknown;
    /** Used to convert and serialize URIs when the target of a cross-reference is in a different document. */
    uriConverter?: (uri: URI, node: AstNode) => string;
}

/**
 * Utility service for transforming an `AstNode` into a JSON string and vice versa.
 */
declare interface JsonSerializer {
    /**
     * Serialize an `AstNode` into a JSON `string`.
     * @param node The `AstNode` to be serialized.
     * @param options Serialization options
     */
    serialize(node: AstNode, options?: JsonSerializeOptions): string;
    /**
     * Deserialize (parse) a JSON `string` into an `AstNode`.
     */
    deserialize<T extends AstNode = AstNode>(content: string, options?: JsonDeserializeOptions): T;
}

declare interface Junction extends langium_2.AstNode {
    readonly $container: Architecture;
    readonly $type: 'EOL' | 'Junction';
    id: string;
    in: string;
}

declare const Junction: {
    readonly $type: "Junction";
    readonly id: "id";
    readonly in: "in";
};

declare interface Keyword extends AbstractElement {
    readonly $container: CharacterRange | InfixRuleOperatorList;
    readonly $type: 'Keyword';
    predicate?: '->' | '=>';
    value: string;
}

declare const Keyword: {
    readonly $type: "Keyword";
    readonly cardinality: "cardinality";
    readonly predicate: "predicate";
    readonly value: "value";
};

declare interface Label extends langium_2.AstNode {
    readonly $container: Component | PipelineComponent;
    readonly $type: 'Label';
    negX: boolean;
    negY: boolean;
    offsetX: number;
    offsetY: number;
}

declare const Label: {
    readonly $type: "Label";
    readonly negX: "negX";
    readonly negY: "negY";
    readonly offsetX: "offsetX";
    readonly offsetY: "offsetY";
};

declare namespace langium {
    export {
        isAstNode,
        isReference,
        isMultiReference,
        isAstNodeDescription,
        isLinkingError,
        isCompositeCstNode,
        isLeafCstNode,
        isRootCstNode,
        AstNode,
        GenericAstNode,
        Properties,
        Reference,
        MultiReference,
        MultiReferenceItem,
        ResolvedReference,
        AstNodeDescription,
        ReferenceInfo,
        LinkingError,
        AstReflection,
        AbstractAstReflection,
        AstMetaData,
        TypeMetaData,
        PropertyMetaData,
        PropertyType,
        CstNode,
        CompositeCstNode,
        LeafCstNode,
        RootCstNode,
        SingleCrossReferencesOfAstNodeType,
        MultiCrossReferencesOfAstNodeType,
        CrossReferencesOfAstNodeType,
        AstTypeList,
        AstNodeTypesWithCrossReferences,
        Mutable
    }
}

declare namespace langium_2 {
    export {
        Grammar,
        GrammarAST,
        createDefaultCoreModule,
        createDefaultSharedCoreModule,
        DefaultCoreModuleContext,
        DefaultSharedCoreModuleContext,
        inject,
        eagerLoad,
        Module,
        ServiceRegistry,
        DefaultServiceRegistry,
        LangiumGeneratedCoreServices,
        LangiumDefaultCoreServices,
        LangiumCoreServices,
        LangiumGeneratedSharedCoreServices,
        LangiumDefaultSharedCoreServices,
        LangiumSharedCoreServices,
        DeepPartial,
        PartialLangiumCoreServices,
        PartialLangiumSharedCoreServices,
        isAstNode,
        isReference,
        isMultiReference,
        isAstNodeDescription,
        isLinkingError,
        isCompositeCstNode,
        isLeafCstNode,
        isRootCstNode,
        AstNode,
        GenericAstNode,
        Properties,
        Reference,
        MultiReference,
        MultiReferenceItem,
        ResolvedReference,
        AstNodeDescription,
        ReferenceInfo,
        LinkingError,
        AstReflection,
        AbstractAstReflection,
        AstMetaData,
        TypeMetaData,
        PropertyMetaData,
        PropertyType,
        CstNode,
        CompositeCstNode,
        LeafCstNode,
        RootCstNode,
        SingleCrossReferencesOfAstNodeType,
        MultiCrossReferencesOfAstNodeType,
        CrossReferencesOfAstNodeType,
        AstTypeList,
        AstNodeTypesWithCrossReferences,
        Mutable,
        CommentProvider,
        DefaultCommentProvider,
        DocumentationProvider,
        JSDocDocumentationProvider,
        parseJSDoc,
        isJSDoc,
        JSDocComment,
        JSDocElement,
        JSDocInline,
        JSDocValue,
        JSDocParagraph,
        JSDocLine,
        JSDocTag,
        JSDocParseOptions,
        JSDocRenderOptions,
        createGrammarConfig,
        GrammarConfig,
        LanguageMetaData,
        AsyncParser,
        DefaultAsyncParser,
        AbstractThreadedAsyncParser,
        WorkerMessagePost,
        WorkerMessageCallback,
        ParserWorker,
        createCompletionParser,
        CstNodeBuilder,
        AbstractCstNode,
        LeafCstNodeImpl,
        CompositeCstNodeImpl,
        RootCstNodeImpl,
        IndentationTokenBuilderOptions,
        indentationBuilderDefaultOptions,
        LexingMode,
        IndentationLexingReport,
        IndentationAwareTokenBuilder,
        IndentationAwareLexer,
        createLangiumParser,
        prepareLangiumParser,
        ParseResult,
        DatatypeSymbol,
        BaseParser,
        AbstractLangiumParser,
        ParserOptions,
        LangiumParser,
        IParserDefinitionError,
        AbstractParserErrorMessageProvider,
        LangiumParserErrorMessageProvider,
        CompletionParserResult,
        LangiumCompletionParser,
        isTokenTypeArray,
        isIMultiModeLexerDefinition,
        isTokenTypeDictionary,
        DefaultLexerErrorMessageProvider,
        LexerResult,
        TokenizeMode,
        TokenizeOptions,
        DEFAULT_TOKENIZE_OPTIONS,
        Lexer,
        DefaultLexer,
        createParser,
        IParserConfig,
        TokenBuilderOptions,
        TokenBuilder,
        LexingReport,
        LexingDiagnosticSeverity,
        LexingDiagnostic,
        DefaultTokenBuilder,
        ValueConverter,
        ValueType,
        DefaultValueConverter,
        Linker,
        RefResolving,
        DefaultReference,
        DefaultMultiReference,
        DefaultLinker,
        isNamed,
        NamedAstNode,
        NameProvider,
        DefaultNameProvider,
        References,
        FindReferencesOptions,
        DefaultReferences,
        Scope,
        ScopeOptions,
        StreamScope,
        MapScope,
        MultiMapScope,
        EMPTY_SCOPE,
        ScopeComputation,
        DefaultScopeComputation,
        ScopeProvider,
        DefaultScopeProvider,
        Hydrator,
        DehydrateContext,
        HydrateContext,
        DefaultHydrator,
        isAstNodeWithComment,
        JsonSerializeOptions,
        JsonDeserializeOptions,
        AstNodeWithTextRegion,
        AstNodeWithComment,
        AstNodeRegionWithAssignments,
        JsonSerializer,
        DefaultJsonSerializer,
        AstUtils,
        Cancellation,
        CstUtils,
        GrammarUtils,
        RegExpUtils,
        DisposableCache,
        SimpleCache,
        ContextCache,
        DocumentCache,
        WorkspaceCache,
        Event_2 as Event,
        EmitterOptions,
        Emitter,
        MultiMap,
        BiMap,
        Disposable_3 as Disposable,
        AsyncDisposable_2 as AsyncDisposable,
        assertUnreachable,
        assertCondition,
        ErrorWithLocation,
        loadGrammarFromJson,
        delayNextTick,
        startCancelableOperation,
        setInterruptionPeriod,
        isOperationCancelled,
        interruptAndCheck,
        MaybePromise,
        OperationCancelled,
        Deferred,
        stream,
        Stream,
        FlatStream,
        MinusOne,
        StreamImpl,
        EMPTY_STREAM,
        DONE_RESULT,
        TreeIterator,
        TreeStream,
        TreeStreamImpl,
        Reduction,
        URI,
        UriUtils,
        UriTrieNode,
        UriTrie,
        getDiagnosticRange,
        toDiagnosticSeverity,
        toDiagnosticData,
        ValidationOptions,
        DocumentValidator,
        ValidateSingleNodeOptions,
        VALIDATE_EACH_NODE,
        DefaultDocumentValidator,
        LinkingErrorData,
        diagnosticData,
        DiagnosticInfo,
        DiagnosticData,
        ValidationSeverity,
        ValidationAcceptor,
        ValidationCheck,
        ValidationPreparation,
        ValidationChecks,
        ValidationCategory,
        ValidationRegistry,
        AstNodeDescriptionProvider,
        DefaultAstNodeDescriptionProvider,
        ReferenceDescription,
        ReferenceDescriptionProvider,
        DefaultReferenceDescriptionProvider,
        AstNodeLocator,
        DefaultAstNodeLocator,
        ConfigurationProvider,
        ConfigurationInitializedParams,
        ConfigurationSectionUpdate,
        ConfigurationSectionUpdateListener,
        DefaultConfigurationProvider,
        BuildOptions,
        DocumentBuildState,
        DocumentBuilder,
        DocumentUpdateListener,
        DocumentBuildListener,
        DocumentPhaseListener,
        DefaultDocumentBuilder,
        TextDocument,
        LangiumDocument,
        DocumentState,
        LocalSymbols,
        DocumentSegment,
        TextDocumentProvider,
        LangiumDocumentFactory,
        DefaultLangiumDocumentFactory,
        LangiumDocuments,
        DefaultLangiumDocuments,
        FileSystemNode,
        FileSystemFilter,
        FileSystemProvider,
        EmptyFileSystemProvider,
        EmptyFileSystem,
        IndexManager,
        DefaultIndexManager,
        WorkspaceLock,
        DefaultWorkspaceLock,
        WorkspaceFolder_2 as WorkspaceFolder,
        WorkspaceManager,
        FileSelector,
        DefaultWorkspaceManager,
        ProfilingCategory,
        LangiumProfiler,
        DefaultLangiumProfiler,
        ProfilingRecord,
        ProfilingTask
    }
}

declare class LangiumCompletionParser extends AbstractLangiumParser {
    private tokens;
    private elementStack;
    private lastElementStack;
    private nextTokenIndex;
    private stackSize;
    action(): void;
    construct(): unknown;
    parse(input: string): CompletionParserResult;
    rule(rule: ParserRule, impl: RuleImpl): RuleResult;
    private resetState;
    private startImplementation;
    private removeUnexpectedElements;
    keepStackSize(): number;
    resetStackSize(size: number): void;
    consume(idx: number, tokenType: TokenType, feature: AbstractElement): void;
    subrule(idx: number, rule: RuleResult, fragment: boolean, feature: AbstractElement, args: Args): void;
    before(element: AbstractElement): void;
    after(element: AbstractElement): void;
    get currIdx(): number;
}

/**
 * The core set of services available for a language. These are either generated by `langium-cli`
 * or provided as default implementations.
 */
declare type LangiumCoreServices = LangiumGeneratedCoreServices & LangiumDefaultCoreServices;

/**
 * Core services for a specific language of which Langium provides default implementations.
 */
declare type LangiumDefaultCoreServices = {
    readonly parser: {
        readonly AsyncParser: AsyncParser;
        readonly GrammarConfig: GrammarConfig;
        readonly ValueConverter: ValueConverter;
        readonly LangiumParser: LangiumParser;
        readonly ParserErrorMessageProvider: IParserErrorMessageProvider;
        readonly LexerErrorMessageProvider: ILexerErrorMessageProvider;
        readonly CompletionParser: LangiumCompletionParser;
        readonly TokenBuilder: TokenBuilder;
        readonly Lexer: Lexer;
    };
    readonly documentation: {
        readonly CommentProvider: CommentProvider;
        readonly DocumentationProvider: DocumentationProvider;
    };
    readonly references: {
        readonly Linker: Linker;
        readonly NameProvider: NameProvider;
        readonly References: References;
        readonly ScopeProvider: ScopeProvider;
        readonly ScopeComputation: ScopeComputation;
    };
    readonly serializer: {
        readonly Hydrator: Hydrator;
        readonly JsonSerializer: JsonSerializer;
    };
    readonly validation: {
        readonly DocumentValidator: DocumentValidator;
        readonly ValidationRegistry: ValidationRegistry;
    };
    readonly workspace: {
        readonly AstNodeLocator: AstNodeLocator;
        readonly AstNodeDescriptionProvider: AstNodeDescriptionProvider;
        readonly ReferenceDescriptionProvider: ReferenceDescriptionProvider;
    };
    readonly shared: LangiumSharedCoreServices;
};

/**
 * Core services shared between multiple languages where Langium provides default implementations.
 */
declare type LangiumDefaultSharedCoreServices = {
    readonly ServiceRegistry: ServiceRegistry;
    readonly workspace: {
        readonly ConfigurationProvider: ConfigurationProvider;
        readonly DocumentBuilder: DocumentBuilder;
        readonly FileSystemProvider: FileSystemProvider;
        readonly IndexManager: IndexManager;
        readonly LangiumDocuments: LangiumDocuments;
        readonly LangiumDocumentFactory: LangiumDocumentFactory;
        readonly TextDocuments?: TextDocumentProvider;
        readonly WorkspaceLock: WorkspaceLock;
        readonly WorkspaceManager: WorkspaceManager;
    };
    readonly profilers: {
        readonly LangiumProfiler?: LangiumProfiler;
    };
};

/**
 * A Langium document holds the parse result (AST and CST) and any additional state that is derived
 * from the AST, e.g. the result of scope precomputation.
 */
declare interface LangiumDocument<T extends AstNode = AstNode> {
    /** The Uniform Resource Identifier (URI) of the document */
    readonly uri: URI;
    /** The text document used to convert between offsets and positions */
    readonly textDocument: TextDocument;
    /** The current state of the document */
    state: DocumentState;
    /** The parse result holds the Abstract Syntax Tree (AST) and potentially also parser / lexer errors */
    parseResult: ParseResult<T>;
    /** Result of the scope precomputation phase */
    localSymbols?: LocalSymbols;
    /** An array of all cross-references found in the AST while linking */
    references: Array<Reference | MultiReference>;
    /** Result of the validation phase */
    diagnostics?: Diagnostic[];
}

/**
 * Shared service for creating `LangiumDocument` instances.
 *
 * Register a custom implementation if special (additional) behavior is required for your language(s).
 * Note: If you specialize {@link fromString} or {@link fromTextDocument} you probably might want to
 * specialize {@link update}, too!
 */
declare interface LangiumDocumentFactory {
    /**
     * Create a Langium document from a `TextDocument` (usually associated with a file).
     */
    fromTextDocument<T extends AstNode = AstNode>(textDocument: TextDocument, uri?: URI, options?: ParserOptions): LangiumDocument<T>;
    /**
     * Create a Langium document from a `TextDocument` asynchronously. This action can be cancelled if a cancellable parser implementation has been provided.
     */
    fromTextDocument<T extends AstNode = AstNode>(textDocument: TextDocument, uri: URI | undefined, cancellationToken: CancellationToken): Promise<LangiumDocument<T>>;
    /**
     * Create an Langium document from an in-memory string.
     */
    fromString<T extends AstNode = AstNode>(text: string, uri: URI, options?: ParserOptions): LangiumDocument<T>;
    /**
     * Create a Langium document from an in-memory string asynchronously. This action can be cancelled if a cancellable parser implementation has been provided.
     */
    fromString<T extends AstNode = AstNode>(text: string, uri: URI, cancellationToken: CancellationToken): Promise<LangiumDocument<T>>;
    /**
     * Create an Langium document from a model that has been constructed in memory.
     */
    fromModel<T extends AstNode = AstNode>(model: T, uri: URI): LangiumDocument<T>;
    /**
     * Create an Langium document from a specified `URI`. The factory will use the `FileSystemAccess` service to read the file.
     */
    fromUri<T extends AstNode = AstNode>(uri: URI, cancellationToken?: CancellationToken): Promise<LangiumDocument<T>>;
    /**
     * Update the given document after changes in the corresponding textual representation.
     * Method is called by the document builder after it has been requested to build an existing
     * document and the document's state is {@link DocumentState.Changed}.
     * The text parsing is expected to be done the same way as in {@link fromTextDocument}
     * and {@link fromString}.
     */
    update<T extends AstNode = AstNode>(document: LangiumDocument<T>, cancellationToken: CancellationToken): Promise<LangiumDocument<T>>;
}

/**
 * Shared service for managing Langium documents.
 */
declare interface LangiumDocuments {
    /**
     * A stream of all documents managed under this service.
     */
    readonly all: Stream<LangiumDocument>;
    /**
     * Manage a new document under this service.
     * @throws an error if a document with the same URI is already present.
     */
    addDocument(document: LangiumDocument): void;
    /**
     * Retrieve the document with the given URI, if present. Otherwise returns `undefined`.
     */
    getDocument(uri: URI): LangiumDocument | undefined;
    /**
     * If the given URI is a directory, all documents within this directory are retrieved.
     * If it is a file, just that single document is retrieved.
     */
    getDocuments(folder: URI): LangiumDocument[];
    /**
     * Retrieve the document with the given URI. If not present, a new one will be created using the file system access.
     * The new document will be added to the list of documents managed under this service.
     */
    getOrCreateDocument(uri: URI, cancellationToken?: CancellationToken): Promise<LangiumDocument>;
    /**
     * Creates a new document with the given URI and text content.
     * The new document is automatically added to this service and can be retrieved using {@link getDocument}.
     *
     * @throws an error if a document with the same URI is already present.
     */
    createDocument(uri: URI, text: string): LangiumDocument;
    /**
     * Creates a new document with the given URI and text content asynchronously.
     * The process can be interrupted with a cancellation token.
     * The new document is automatically added to this service and can be retrieved using {@link getDocument}.
     *
     * @throws an error if a document with the same URI is already present.
     */
    createDocument(uri: URI, text: string, cancellationToken: CancellationToken): Promise<LangiumDocument>;
    /**
     * Flag the document with the given URI as `Changed`, if present, meaning that its content
     * is no longer valid. The content (parseResult) stays untouched, while internal data may
     * be dropped to reduce memory footprint.
     *
     * @returns the affected {@link LangiumDocument} if existing for convenience
     *
     * @deprecated Since 4.2 use `DocumentBuilder.resetToState(DocumentState.Changed)` instead
     */
    invalidateDocument(uri: URI): LangiumDocument | undefined;
    /**
     * Returns `true` if a document with the given URI is managed under this service.
     */
    hasDocument(uri: URI): boolean;
    /**
     * Remove the document with the given URI, if present, and mark it as `Changed`, meaning
     * that its content is no longer valid. The next call to `getOrCreateDocument` with the same
     * URI will create a new document instance.
     *
     * @returns the affected {@link LangiumDocument} if existing for convenience
     */
    deleteDocument(uri: URI): LangiumDocument | undefined;
    /**
     * If the given URI is a directory, remove all documents within this directory.
     * If it is a file, just remove that single document from the documents.
     *
     * @returns the affected {@link LangiumDocument}s if existing for convenience
     */
    deleteDocuments(uri: URI): LangiumDocument[];
}

/**
 * The services generated by `langium-cli` for a specific language. These are derived from the
 * grammar definition and the language configuration.
 */
declare type LangiumGeneratedCoreServices = {
    readonly Grammar: Grammar;
    readonly LanguageMetaData: LanguageMetaData;
    readonly parser: {
        readonly ParserConfig?: IParserConfig;
    };
};

/**
 * The services generated by `langium-cli` that are shared between multiple languages. These are
 * derived from the grammar definition.
 */
declare type LangiumGeneratedSharedCoreServices = {
    readonly AstReflection: AstReflection;
};

declare class LangiumGrammarAstReflection extends langium.AbstractAstReflection {
    readonly types: {
        readonly AbstractElement: {
            readonly name: "AbstractElement";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
            };
            readonly superTypes: [];
        };
        readonly AbstractParserRule: {
            readonly name: "AbstractParserRule";
            readonly properties: {};
            readonly superTypes: ["AbstractRule", "AbstractType"];
        };
        readonly AbstractRule: {
            readonly name: "AbstractRule";
            readonly properties: {};
            readonly superTypes: [];
        };
        readonly AbstractType: {
            readonly name: "AbstractType";
            readonly properties: {};
            readonly superTypes: [];
        };
        readonly Action: {
            readonly name: "Action";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly feature: {
                    readonly name: "feature";
                };
                readonly inferredType: {
                    readonly name: "inferredType";
                };
                readonly operator: {
                    readonly name: "operator";
                };
                readonly type: {
                    readonly name: "type";
                    readonly referenceType: "AbstractType";
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly Alternatives: {
            readonly name: "Alternatives";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly elements: {
                    readonly name: "elements";
                    readonly defaultValue: [];
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly ArrayLiteral: {
            readonly name: "ArrayLiteral";
            readonly properties: {
                readonly elements: {
                    readonly name: "elements";
                    readonly defaultValue: [];
                };
            };
            readonly superTypes: ["ValueLiteral"];
        };
        readonly ArrayType: {
            readonly name: "ArrayType";
            readonly properties: {
                readonly elementType: {
                    readonly name: "elementType";
                };
            };
            readonly superTypes: ["TypeDefinition"];
        };
        readonly Assignment: {
            readonly name: "Assignment";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly feature: {
                    readonly name: "feature";
                };
                readonly operator: {
                    readonly name: "operator";
                };
                readonly predicate: {
                    readonly name: "predicate";
                };
                readonly terminal: {
                    readonly name: "terminal";
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly BooleanLiteral: {
            readonly name: "BooleanLiteral";
            readonly properties: {
                readonly true: {
                    readonly name: "true";
                    readonly defaultValue: false;
                };
            };
            readonly superTypes: ["Condition", "ValueLiteral"];
        };
        readonly CharacterRange: {
            readonly name: "CharacterRange";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly left: {
                    readonly name: "left";
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
                readonly right: {
                    readonly name: "right";
                };
            };
            readonly superTypes: ["TerminalElement"];
        };
        readonly Condition: {
            readonly name: "Condition";
            readonly properties: {};
            readonly superTypes: [];
        };
        readonly Conjunction: {
            readonly name: "Conjunction";
            readonly properties: {
                readonly left: {
                    readonly name: "left";
                };
                readonly right: {
                    readonly name: "right";
                };
            };
            readonly superTypes: ["Condition"];
        };
        readonly CrossReference: {
            readonly name: "CrossReference";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly deprecatedSyntax: {
                    readonly name: "deprecatedSyntax";
                    readonly defaultValue: false;
                };
                readonly isMulti: {
                    readonly name: "isMulti";
                    readonly defaultValue: false;
                };
                readonly terminal: {
                    readonly name: "terminal";
                };
                readonly type: {
                    readonly name: "type";
                    readonly referenceType: "AbstractType";
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly Disjunction: {
            readonly name: "Disjunction";
            readonly properties: {
                readonly left: {
                    readonly name: "left";
                };
                readonly right: {
                    readonly name: "right";
                };
            };
            readonly superTypes: ["Condition"];
        };
        readonly EndOfFile: {
            readonly name: "EndOfFile";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly Grammar: {
            readonly name: "Grammar";
            readonly properties: {
                readonly imports: {
                    readonly name: "imports";
                    readonly defaultValue: [];
                };
                readonly interfaces: {
                    readonly name: "interfaces";
                    readonly defaultValue: [];
                };
                readonly isDeclared: {
                    readonly name: "isDeclared";
                    readonly defaultValue: false;
                };
                readonly name: {
                    readonly name: "name";
                };
                readonly rules: {
                    readonly name: "rules";
                    readonly defaultValue: [];
                };
                readonly types: {
                    readonly name: "types";
                    readonly defaultValue: [];
                };
            };
            readonly superTypes: [];
        };
        readonly GrammarImport: {
            readonly name: "GrammarImport";
            readonly properties: {
                readonly path: {
                    readonly name: "path";
                };
            };
            readonly superTypes: [];
        };
        readonly Group: {
            readonly name: "Group";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly elements: {
                    readonly name: "elements";
                    readonly defaultValue: [];
                };
                readonly guardCondition: {
                    readonly name: "guardCondition";
                };
                readonly predicate: {
                    readonly name: "predicate";
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly InferredType: {
            readonly name: "InferredType";
            readonly properties: {
                readonly name: {
                    readonly name: "name";
                };
            };
            readonly superTypes: ["AbstractType"];
        };
        readonly InfixRule: {
            readonly name: "InfixRule";
            readonly properties: {
                readonly call: {
                    readonly name: "call";
                };
                readonly dataType: {
                    readonly name: "dataType";
                };
                readonly inferredType: {
                    readonly name: "inferredType";
                };
                readonly name: {
                    readonly name: "name";
                };
                readonly operators: {
                    readonly name: "operators";
                };
                readonly parameters: {
                    readonly name: "parameters";
                    readonly defaultValue: [];
                };
                readonly returnType: {
                    readonly name: "returnType";
                    readonly referenceType: "AbstractType";
                };
            };
            readonly superTypes: ["AbstractParserRule"];
        };
        readonly InfixRuleOperatorList: {
            readonly name: "InfixRuleOperatorList";
            readonly properties: {
                readonly associativity: {
                    readonly name: "associativity";
                };
                readonly operators: {
                    readonly name: "operators";
                    readonly defaultValue: [];
                };
            };
            readonly superTypes: [];
        };
        readonly InfixRuleOperators: {
            readonly name: "InfixRuleOperators";
            readonly properties: {
                readonly precedences: {
                    readonly name: "precedences";
                    readonly defaultValue: [];
                };
            };
            readonly superTypes: [];
        };
        readonly Interface: {
            readonly name: "Interface";
            readonly properties: {
                readonly attributes: {
                    readonly name: "attributes";
                    readonly defaultValue: [];
                };
                readonly name: {
                    readonly name: "name";
                };
                readonly superTypes: {
                    readonly name: "superTypes";
                    readonly defaultValue: [];
                    readonly referenceType: "AbstractType";
                };
            };
            readonly superTypes: ["AbstractType"];
        };
        readonly Keyword: {
            readonly name: "Keyword";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly predicate: {
                    readonly name: "predicate";
                };
                readonly value: {
                    readonly name: "value";
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly NamedArgument: {
            readonly name: "NamedArgument";
            readonly properties: {
                readonly calledByName: {
                    readonly name: "calledByName";
                    readonly defaultValue: false;
                };
                readonly parameter: {
                    readonly name: "parameter";
                    readonly referenceType: "Parameter";
                };
                readonly value: {
                    readonly name: "value";
                };
            };
            readonly superTypes: [];
        };
        readonly NegatedToken: {
            readonly name: "NegatedToken";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
                readonly terminal: {
                    readonly name: "terminal";
                };
            };
            readonly superTypes: ["TerminalElement"];
        };
        readonly Negation: {
            readonly name: "Negation";
            readonly properties: {
                readonly value: {
                    readonly name: "value";
                };
            };
            readonly superTypes: ["Condition"];
        };
        readonly NumberLiteral: {
            readonly name: "NumberLiteral";
            readonly properties: {
                readonly value: {
                    readonly name: "value";
                };
            };
            readonly superTypes: ["ValueLiteral"];
        };
        readonly Parameter: {
            readonly name: "Parameter";
            readonly properties: {
                readonly name: {
                    readonly name: "name";
                };
            };
            readonly superTypes: [];
        };
        readonly ParameterReference: {
            readonly name: "ParameterReference";
            readonly properties: {
                readonly parameter: {
                    readonly name: "parameter";
                    readonly referenceType: "Parameter";
                };
            };
            readonly superTypes: ["Condition"];
        };
        readonly ParserRule: {
            readonly name: "ParserRule";
            readonly properties: {
                readonly dataType: {
                    readonly name: "dataType";
                };
                readonly definition: {
                    readonly name: "definition";
                };
                readonly entry: {
                    readonly name: "entry";
                    readonly defaultValue: false;
                };
                readonly fragment: {
                    readonly name: "fragment";
                    readonly defaultValue: false;
                };
                readonly inferredType: {
                    readonly name: "inferredType";
                };
                readonly name: {
                    readonly name: "name";
                };
                readonly parameters: {
                    readonly name: "parameters";
                    readonly defaultValue: [];
                };
                readonly returnType: {
                    readonly name: "returnType";
                    readonly referenceType: "AbstractType";
                };
            };
            readonly superTypes: ["AbstractParserRule"];
        };
        readonly ReferenceType: {
            readonly name: "ReferenceType";
            readonly properties: {
                readonly isMulti: {
                    readonly name: "isMulti";
                    readonly defaultValue: false;
                };
                readonly referenceType: {
                    readonly name: "referenceType";
                };
            };
            readonly superTypes: ["TypeDefinition"];
        };
        readonly RegexToken: {
            readonly name: "RegexToken";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
                readonly regex: {
                    readonly name: "regex";
                };
            };
            readonly superTypes: ["TerminalElement"];
        };
        readonly ReturnType: {
            readonly name: "ReturnType";
            readonly properties: {
                readonly name: {
                    readonly name: "name";
                };
            };
            readonly superTypes: [];
        };
        readonly RuleCall: {
            readonly name: "RuleCall";
            readonly properties: {
                readonly arguments: {
                    readonly name: "arguments";
                    readonly defaultValue: [];
                };
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly predicate: {
                    readonly name: "predicate";
                };
                readonly rule: {
                    readonly name: "rule";
                    readonly referenceType: "AbstractRule";
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly SimpleType: {
            readonly name: "SimpleType";
            readonly properties: {
                readonly primitiveType: {
                    readonly name: "primitiveType";
                };
                readonly stringType: {
                    readonly name: "stringType";
                };
                readonly typeRef: {
                    readonly name: "typeRef";
                    readonly referenceType: "AbstractType";
                };
            };
            readonly superTypes: ["TypeDefinition"];
        };
        readonly StringLiteral: {
            readonly name: "StringLiteral";
            readonly properties: {
                readonly value: {
                    readonly name: "value";
                };
            };
            readonly superTypes: ["ValueLiteral"];
        };
        readonly TerminalAlternatives: {
            readonly name: "TerminalAlternatives";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly elements: {
                    readonly name: "elements";
                    readonly defaultValue: [];
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
            };
            readonly superTypes: ["TerminalElement"];
        };
        readonly TerminalElement: {
            readonly name: "TerminalElement";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly TerminalGroup: {
            readonly name: "TerminalGroup";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly elements: {
                    readonly name: "elements";
                    readonly defaultValue: [];
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
            };
            readonly superTypes: ["TerminalElement"];
        };
        readonly TerminalRule: {
            readonly name: "TerminalRule";
            readonly properties: {
                readonly definition: {
                    readonly name: "definition";
                };
                readonly fragment: {
                    readonly name: "fragment";
                    readonly defaultValue: false;
                };
                readonly hidden: {
                    readonly name: "hidden";
                    readonly defaultValue: false;
                };
                readonly name: {
                    readonly name: "name";
                };
                readonly type: {
                    readonly name: "type";
                };
            };
            readonly superTypes: ["AbstractRule"];
        };
        readonly TerminalRuleCall: {
            readonly name: "TerminalRuleCall";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
                readonly rule: {
                    readonly name: "rule";
                    readonly referenceType: "TerminalRule";
                };
            };
            readonly superTypes: ["TerminalElement"];
        };
        readonly Type: {
            readonly name: "Type";
            readonly properties: {
                readonly name: {
                    readonly name: "name";
                };
                readonly type: {
                    readonly name: "type";
                };
            };
            readonly superTypes: ["AbstractType"];
        };
        readonly TypeAttribute: {
            readonly name: "TypeAttribute";
            readonly properties: {
                readonly defaultValue: {
                    readonly name: "defaultValue";
                };
                readonly isOptional: {
                    readonly name: "isOptional";
                    readonly defaultValue: false;
                };
                readonly name: {
                    readonly name: "name";
                };
                readonly type: {
                    readonly name: "type";
                };
            };
            readonly superTypes: [];
        };
        readonly TypeDefinition: {
            readonly name: "TypeDefinition";
            readonly properties: {};
            readonly superTypes: [];
        };
        readonly UnionType: {
            readonly name: "UnionType";
            readonly properties: {
                readonly types: {
                    readonly name: "types";
                    readonly defaultValue: [];
                };
            };
            readonly superTypes: ["TypeDefinition"];
        };
        readonly UnorderedGroup: {
            readonly name: "UnorderedGroup";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly elements: {
                    readonly name: "elements";
                    readonly defaultValue: [];
                };
            };
            readonly superTypes: ["AbstractElement"];
        };
        readonly UntilToken: {
            readonly name: "UntilToken";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
                readonly terminal: {
                    readonly name: "terminal";
                };
            };
            readonly superTypes: ["TerminalElement"];
        };
        readonly ValueLiteral: {
            readonly name: "ValueLiteral";
            readonly properties: {};
            readonly superTypes: [];
        };
        readonly Wildcard: {
            readonly name: "Wildcard";
            readonly properties: {
                readonly cardinality: {
                    readonly name: "cardinality";
                };
                readonly lookahead: {
                    readonly name: "lookahead";
                };
                readonly parenthesized: {
                    readonly name: "parenthesized";
                    readonly defaultValue: false;
                };
            };
            readonly superTypes: ["TerminalElement"];
        };
    };
}

declare type LangiumGrammarAstType = {
    AbstractElement: AbstractElement;
    AbstractParserRule: AbstractParserRule;
    AbstractRule: AbstractRule;
    AbstractType: AbstractType;
    Action: Action;
    Alternatives: Alternatives;
    ArrayLiteral: ArrayLiteral;
    ArrayType: ArrayType;
    Assignment: Assignment;
    BooleanLiteral: BooleanLiteral;
    CharacterRange: CharacterRange;
    Condition: Condition;
    Conjunction: Conjunction;
    CrossReference: CrossReference;
    Disjunction: Disjunction;
    EndOfFile: EndOfFile;
    Grammar: Grammar;
    GrammarImport: GrammarImport;
    Group: Group;
    InferredType: InferredType;
    InfixRule: InfixRule;
    InfixRuleOperatorList: InfixRuleOperatorList;
    InfixRuleOperators: InfixRuleOperators;
    Interface: Interface;
    Keyword: Keyword;
    NamedArgument: NamedArgument;
    NegatedToken: NegatedToken;
    Negation: Negation;
    NumberLiteral: NumberLiteral;
    Parameter: Parameter;
    ParameterReference: ParameterReference;
    ParserRule: ParserRule;
    ReferenceType: ReferenceType;
    RegexToken: RegexToken;
    ReturnType: ReturnType_2;
    RuleCall: RuleCall;
    SimpleType: SimpleType;
    StringLiteral: StringLiteral;
    TerminalAlternatives: TerminalAlternatives;
    TerminalElement: TerminalElement;
    TerminalGroup: TerminalGroup;
    TerminalRule: TerminalRule;
    TerminalRuleCall: TerminalRuleCall;
    Type: Type;
    TypeAttribute: TypeAttribute;
    TypeDefinition: TypeDefinition;
    UnionType: UnionType;
    UnorderedGroup: UnorderedGroup;
    UntilToken: UntilToken;
    ValueLiteral: ValueLiteral;
    Wildcard: Wildcard;
};

declare type LangiumGrammarKeywordNames = "!" | "&" | "(" | ")" | "*" | "+" | "+=" | "," | "->" | "." | ".." | ":" | ";" | "<" | "=" | "=>" | ">" | "?" | "?!" | "?<!" | "?<=" | "?=" | "@" | "Date" | "EOF" | "[" | "]" | "assoc" | "bigint" | "boolean" | "current" | "entry" | "extends" | "false" | "fragment" | "grammar" | "hidden" | "import" | "infer" | "infers" | "infix" | "interface" | "left" | "number" | "on" | "returns" | "right" | "string" | "terminal" | "true" | "type" | "with" | "{" | "|" | "}";

declare type LangiumGrammarTerminalNames = keyof typeof LangiumGrammarTerminals;

declare const LangiumGrammarTerminals: {
    ID: RegExp;
    STRING: RegExp;
    NUMBER: RegExp;
    RegexLiteral: RegExp;
    WS: RegExp;
    ML_COMMENT: RegExp;
    SL_COMMENT: RegExp;
};

declare type LangiumGrammarTokenNames = LangiumGrammarTerminalNames | LangiumGrammarKeywordNames;

declare class LangiumParser extends AbstractLangiumParser {
    private readonly linker;
    private readonly converter;
    private readonly astReflection;
    private readonly nodeBuilder;
    private lexerResult?;
    private stack;
    private assignmentMap;
    private operatorPrecedence;
    private get current();
    constructor(services: LangiumCoreServices);
    rule(rule: ParserRule | InfixRule, impl: RuleImpl): RuleResult;
    private registerPrecedenceMap;
    private computeRuleType;
    parse<T extends AstNode = AstNode>(input: string, options?: ParserOptions): ParseResult<T>;
    private doParse;
    private startImplementation;
    private extractHiddenTokens;
    consume(idx: number, tokenType: TokenType, feature: AbstractElement): void;
    /**
     * Most consumed parser tokens are valid. However there are two cases in which they are not valid:
     *
     * 1. They were inserted during error recovery by the parser. These tokens don't really exist and should not be further processed
     * 2. They contain invalid token ranges. This might include the special EOF token, or other tokens produced by invalid token builders.
     */
    private isValidToken;
    subrule(idx: number, rule: RuleResult, fragment: boolean, feature: AbstractElement, args: Args): void;
    private performSubruleAssignment;
    action($type: string, action: Action): void;
    private construct;
    private constructInfix;
    private getAssignment;
    private assign;
    private assignWithoutOverride;
    get definitionErrors(): IParserDefinitionError[];
}

declare class LangiumParserErrorMessageProvider extends AbstractParserErrorMessageProvider {
    buildMismatchTokenMessage({ expected, actual }: {
        expected: TokenType;
        actual: IToken;
        previous: IToken;
        ruleName: string;
    }): string;
    buildNotAllInputParsedMessage({ firstRedundant }: {
        firstRedundant: IToken;
        ruleName: string;
    }): string;
}

declare interface LangiumProfiler {
    /**
     * Checks if the given category is active.
     * @param category The category to check.
     * @returns `true` if the category is active, `false` otherwise.
     */
    isActive(category: ProfilingCategory): boolean;
    /**
     * Starts the profiling for the given categories. If none are provided, all categories are started.
     * @param categories The categories to start profiling for.
     */
    start(...categories: ProfilingCategory[]): void;
    /**
     * Stops the profiling for the given categories. If none are provided, all categories are stopped.
     * @param categories The categories to stop profiling for.
     */
    stop(...categories: ProfilingCategory[]): void;
    /**
     * Creates a new {@link ProfilingTask} for the given category.
     * @param category The category to create the task for.
     * @param taskId The identifier of the task.
     */
    createTask(category: ProfilingCategory, taskId: string): ProfilingTask;
    /**
     * Gets the {@link ProfilingRecord}s for the given categories. If none are provided, all records are returned.
     * @param categories The categories to get the records for.
     * @returns A stream of profiling records.
     */
    getRecords(...categories: ProfilingCategory[]): Stream<ProfilingRecord>;
}

/**
 * The shared core services are a set of services that are used by every language within a Langium project (excluding LSP services)
 * This is necessary to enable features like cross references across different languages.
 */
declare type LangiumSharedCoreServices = LangiumDefaultSharedCoreServices & LangiumGeneratedSharedCoreServices;

/******************************************************************************
 * Copyright 2021 TypeFox GmbH
 * This program and the accompanying materials are made available under the
 * terms of the MIT License, which is available in the project root.
 ******************************************************************************/
/**
 * Metadata of a language.
 */
declare interface LanguageMetaData {
    languageId: string;
    fileExtensions: readonly string[];
    fileNames?: readonly string[];
    caseInsensitive: boolean;
    /**
     * Mode used to optimize code for development or production environments.
     *
     * In production mode, all Chevrotain lexer/parser validations are disabled.
     */
    mode: 'development' | 'production';
}

declare interface Leaf extends Item {
    readonly $type: 'Leaf';
    value: number;
}

declare const Leaf: {
    readonly $type: "Leaf";
    readonly classSelector: "classSelector";
    readonly name: "name";
    readonly value: "value";
};

/**
 * A leaf CST node corresponds to a token in the input token stream.
 */
declare interface LeafCstNode extends CstNode {
    readonly tokenType: TokenType;
}

declare class LeafCstNodeImpl extends AbstractCstNode implements LeafCstNode {
    get offset(): number;
    get length(): number;
    get end(): number;
    get hidden(): boolean;
    get tokenType(): TokenType;
    get range(): Range_2;
    private _hidden;
    private _offset;
    private _length;
    private _range;
    private _tokenType;
    constructor(offset: number, length: number, range: Range_2, tokenType: TokenType, hidden?: boolean);
}

declare interface Lexer {
    readonly definition: TokenTypeDictionary;
    tokenize(text: string, options?: TokenizeOptions): LexerResult;
}

declare interface LexerResult {
    /**
     * A list of all tokens that were lexed from the input.
     *
     * Note that Langium requires the optional properties
     * `startLine`, `startColumn`, `endOffset`, `endLine` and `endColumn` to be set on each token.
     */
    tokens: IToken[];
    /**
     * Contains hidden tokens, usually comments.
     */
    hidden: IToken[];
    errors: ILexingError[];
    report?: LexingReport;
}

declare interface LexingDiagnostic extends ILexingError {
    severity?: LexingDiagnosticSeverity;
}

declare type LexingDiagnosticSeverity = 'error' | 'warning' | 'info' | 'hint';

declare enum LexingMode {
    REGULAR = "indentation-sensitive",
    IGNORE_INDENTATION = "ignore-indentation"
}

/**
 * A custom lexing report that can be produced by the token builder during the lexing process.
 * Adopters need to ensure that the any custom fields are serializable so they can be sent across worker threads.
 */
declare interface LexingReport {
    diagnostics: LexingDiagnostic[];
}

declare interface Link extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'EOL' | 'Link';
    arrow?: string;
    from: string;
    fromPort?: string;
    linkLabel?: string;
    to: string;
    toPort?: string;
}

declare const Link: {
    readonly $type: "Link";
    readonly arrow: "arrow";
    readonly from: "from";
    readonly fromPort: "fromPort";
    readonly linkLabel: "linkLabel";
    readonly to: "to";
    readonly toPort: "toPort";
};

/**
 * Link the `$container` and other related properties of every AST node that is directly contained
 * in the given `node`.
 */
declare function linkContentToContainer(node: AstNode, options?: {
    /**
     * If true, the function will also link the content of the contained nodes.
     * Otherwise, only the immediate children of the given node are linked to their container.
     */
    deep?: boolean;
}): void;

/**
 * Client capabilities for the linked editing range request.
 *
 * @since 3.16.0
 */
declare interface LinkedEditingRangeClientCapabilities {
    /**
     * Whether implementation supports dynamic registration. If this is set to `true`
     * the client supports the new `(TextDocumentRegistrationOptions & StaticRegistrationOptions)`
     * return value for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
}

/**
 * Language-specific service for resolving cross-references in the AST.
 */
declare interface Linker {
    /**
     * Links all cross-references within the specified document. The default implementation loads only target
     * elements from documents that are present in the `LangiumDocuments` service. The linked references are
     * stored in the document's `references` property.
     *
     * @param document A LangiumDocument that shall be linked.
     * @param cancelToken A token for cancelling the operation.
     *
     * @throws `OperationCancelled` if a cancellation event is detected
     */
    link(document: LangiumDocument, cancelToken?: CancellationToken): Promise<void>;
    /**
     * Unlinks all references within the specified document and removes them from the list of `references`.
     *
     * @param document A LangiumDocument that shall be unlinked.
     */
    unlink(document: LangiumDocument): void;
    /**
     * Determines a candidate AST node description for linking the given reference.
     *
     * @param refInfo Information about the reference.
     */
    getCandidate(refInfo: ReferenceInfo): AstNodeDescription | LinkingError;
    /**
     * Determines a candidate AST node description for linking the given reference.
     *
     * @param node The AST node containing the reference.
     * @param refId The reference identifier used to build a scope.
     * @param reference The actual reference to resolve.
     */
    getCandidates(refInfo: ReferenceInfo): AstNodeDescription[] | LinkingError;
    /**
     * Creates a cross reference node being aware of its containing AstNode, the corresponding CstNode,
     * the cross reference text denoting the target AstNode being already extracted of the document text,
     * as well as the unique cross reference identifier.
     *
     * Default behavior:
     *  - The returned Reference's 'ref' property pointing to the target AstNode is populated lazily on its
     *    first visit.
     *  - If the target AstNode cannot be resolved on the first visit, an error indicator will be installed
     *    and further resolution attempts will *not* be performed.
     *
     * @param node The containing AST node
     * @param property The AST node property being referenced
     * @param refNode The corresponding CST node
     * @param refText The cross reference text denoting the target AstNode
     * @returns the desired Reference node, whose behavior wrt. resolving the cross reference is implementation specific.
     */
    buildReference(node: AstNode, property: string, refNode: CstNode | undefined, refText: string): Reference;
    buildMultiReference(node: AstNode, property: string, refNode: CstNode | undefined, refText: string): MultiReference;
}

/**
 * Used to collect information when the `Linker` service fails to resolve a cross-reference.
 */
declare interface LinkingError {
    message: string;
    info: ReferenceInfo;
    targetDescription?: AstNodeDescription;
}

declare interface LinkingErrorData extends DiagnosticData {
    containerType: string;
    property: string;
    refText: string;
}

/**
 * Load a Langium grammar for your language from a JSON string. This is used by several services,
 * most notably the parser builder which interprets the grammar to create a parser.
 */
declare function loadGrammarFromJson(json: string): GrammarAST.Grammar;

/**
 * Result of the scope pre-computation phase performed by the `ScopeComputation` service.
 * It maps AST nodes of a document to their corresponding sets of symbols that are accessible
 * by those nodes/subtrees, provided any symbols corresponding specifically to those nodes/subtrees exist.
 * The sets of symbols are assumed to be un-ordered. Hence, no assumptions about the order of
 * symbols in the sets should be made. The default `ScopeComputation` implementation uses an
 * instance of `MultiMap<AstNode, AstNodeDescription>`, which conforms to this interface.
 */
declare interface LocalSymbols {
    has(node: AstNode): boolean;
    getStream(key: AstNode): Stream<AstNodeDescription>;
}

/**
 * Represents a location inside a resource, such as a line
 * inside a text file.
 */
declare interface Location_2 {
    uri: DocumentUri_2;
    range: Range_2;
}

/**
 * The Location namespace provides helper functions to work with
 * {@link Location} literals.
 */
declare namespace Location_2 {
    /**
     * Creates a Location literal.
     * @param uri The location's uri.
     * @param range The location's range.
     */
    function create(uri: DocumentUri_2, range: Range_2): Location_2;
    /**
     * Checks whether the given literal conforms to the {@link Location} interface.
     */
    function is(value: any): value is Location_2;
}

/**
 * The LSP any type.
 *
 * In the current implementation we map LSPAny to any. This is due to the fact
 * that the TypeScript compilers can't infer string access signatures for
 * interface correctly (it can though for types). See the following issue for
 * details: https://github.com/microsoft/TypeScript/issues/15300.
 *
 * When the issue is addressed LSPAny can be defined as follows:
 *
 * ```ts
 * export type LSPAny = LSPObject | LSPArray | string | integer | uinteger | decimal | boolean | null | undefined;
 * export type LSPObject = { [key: string]: LSPAny };
 * export type LSPArray = LSPAny[];
 * ```
 *
 * Please note that strictly speaking a property with the value `undefined`
 * can't be converted into JSON preserving the property name. However for
 * convenience it is allowed and assumed that all these properties are
 * optional as well.
 *
 * @since 3.17.0
 */
declare type LSPAny = any;

/**
 * The LSP any type.
 *
 * In the current implementation we map LSPAny to any. This is due to the fact
 * that the TypeScript compilers can't infer string access signatures for
 * interface correctly (it can though for types). See the following issue for
 * details: https://github.com/microsoft/TypeScript/issues/15300.
 *
 * When the issue is addressed LSPAny can be defined as follows:
 *
 * ```ts
 * export type LSPAny = LSPObject | LSPArray | string | integer | uinteger | decimal | boolean | null | undefined;
 * export type LSPObject = { [key: string]: LSPAny };
 * export type LSPArray = LSPAny[];
 * ```
 *
 * Please note that strictly speaking a property with the value `undefined`
 * can't be converted into JSON preserving the property name. However for
 * convenience it is allowed and assumed that all these properties are
 * optional as well.
 *
 * @since 3.17.0
 */
declare type LSPAny_2 = any;

declare class MapScope implements Scope {
    readonly elements: Map<string, AstNodeDescription>;
    readonly outerScope?: Scope;
    readonly caseInsensitive: boolean;
    readonly concatOuterScope: boolean;
    constructor(elements: Iterable<AstNodeDescription>, outerScope?: Scope, options?: ScopeOptions);
    getElement(name: string): AstNodeDescription | undefined;
    getElements(name: string): Stream<AstNodeDescription>;
    getAllElements(): Stream<AstNodeDescription>;
}

/**
 * Client capabilities specific to the used markdown parser.
 *
 * @since 3.16.0
 */
declare interface MarkdownClientCapabilities {
    /**
     * The name of the parser.
     */
    parser: string;
    /**
     * The version of the parser.
     */
    version?: string;
    /**
     * A list of HTML tags that the client allows / supports in
     * Markdown.
     *
     * @since 3.17.0
     */
    allowedTags?: string[];
}

/**
 * Describes the content type that a client supports in various
 * result literals like `Hover`, `ParameterInfo` or `CompletionItem`.
 *
 * Please note that `MarkupKinds` must not start with a `$`. This kinds
 * are reserved for internal usage.
 */
declare namespace MarkupKind {
    /**
     * Plain text is supported as a content format
     */
    const PlainText: 'plaintext';
    /**
     * Markdown is supported as a content format
     */
    const Markdown: 'markdown';
    /**
     * Checks whether the given value is a value of the {@link MarkupKind} type.
     */
    function is(value: any): value is MarkupKind;
}

declare type MarkupKind = 'plaintext' | 'markdown';

declare type MaybePromise<T> = T | Promise<T>;

export declare interface Merge extends langium_2.AstNode {
    readonly $container: GitGraph;
    readonly $type: 'EOL' | 'Merge';
    branch: string;
    id: string;
    tags: Array<string>;
    type: 'HIGHLIGHT' | 'NORMAL' | 'REVERSE';
}

export declare const Merge: {
    readonly $type: "Merge";
    readonly branch: "branch";
    readonly id: "id";
    readonly tags: "tags";
    readonly type: "type";
};

export declare type MermaidAstType = ArchitectureGrammar.AstType & CynefinGrammar.AstType & EventModeling.AstType & GitGraphGrammar.AstType & InfoGrammar.AstType & PacketGrammar.AstType & PieGrammar.AstType & RadarGrammar.AstType & RailroadAbnfGrammar.AstType & RailroadEbnfGrammar.AstType & RailroadGrammar.AstType & RailroadPegGrammar.AstType & TreeViewGrammar.AstType & TreemapGrammar.AstType & WardleyGrammar.AstType;

export declare const MermaidGeneratedSharedModule: Module<LangiumSharedCoreServices, LangiumGeneratedSharedCoreServices>;

export declare class MermaidParseError extends Error {
    result: ParseResult<DiagramAST>;
    constructor(result: ParseResult<DiagramAST>);
}

declare type MinusOne<N extends number> = [-1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20][N];

/******************************************************************************
 * Copyright 2021 TypeFox GmbH
 * This program and the accompanying materials are made available under the
 * terms of the MIT License, which is available in the project root.
 ******************************************************************************/
/**
 * A `Module<I>` is a description of possibly grouped service factories.
 *
 * Given a type I = { group: { service: A } },
 * Module<I> := { group: { service: (injector: I) => A } }
 *
 * Making `I` available during the creation of `I` allows us to create cyclic
 * dependencies.
 */
declare type Module<I, T = I> = {
    [K in keyof T]: Module<I, T[K]> | ((injector: I) => T[K]);
};

declare namespace Module {
    /**
     * Merges two dependency injection modules into a new (third) one that is returned.
     * At that `m1` and `m2` stay unchanged. Therefore, `m1` is deep-copied first,
     * and m2 is merged onto the copy afterwards.
     *
     * Note that the leaf values of `m1` and `m2`, i.e. the service constructor functions,
     * cannot be copied generically, since they are functions. They are shared by the source and merged modules.
     *
     * @returns the merged module being a deep copy of `m1` with `m2` merged onto it.
     */
    const merge: <M1, M2, R extends M1 & M2>(m1: Module<R, M1>, m2: Module<R, M2>) => Module<R, M1 & M2>;
}

/**
 * Client capabilities specific to the moniker request.
 *
 * @since 3.16.0
 */
declare interface MonikerClientCapabilities {
    /**
     * Whether moniker supports dynamic registration. If this is set to `true`
     * the client supports the new `MonikerRegistrationOptions` return value
     * for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
}

declare type MultiCrossReferencesOfAstNodeType<N extends AstNode> = ExtractKeysOfValueType<N, MultiReference | MultiReference[]>;

/**
 * A multimap is a variation of a Map that has potentially multiple values for every key.
 */
declare class MultiMap<K, V> {
    private map;
    constructor();
    constructor(elements: Iterable<[K, V]>);
    /**
     * The total number of values in the multimap.
     */
    get size(): number;
    /**
     * Clear all entries in the multimap.
     */
    clear(): void;
    /**
     * Operates differently depending on whether a `value` is given:
     *  * With a value, this method deletes the specific key / value pair from the multimap.
     *  * Without a value, all values associated with the given key are deleted.
     *
     * @returns `true` if a value existed and has been removed, or `false` if the specified
     *     key / value does not exist.
     */
    delete(key: K, value?: V): boolean;
    /**
     * Returns an array of all values associated with the given key. If no value exists,
     * an empty array is returned.
     *
     * _Note:_ The returned array is assumed not to be modified. Use the `set` method to add a
     * value and `delete` to remove a value from the multimap.
     */
    get(key: K): readonly V[];
    /**
     * Returns a stream of all values associated with the given key. If no value exists,
     * {@link EMPTY_STREAM} is returned.
     */
    getStream(key: K): Stream<V>;
    /**
     * Operates differently depending on whether a `value` is given:
     *  * With a value, this method returns `true` if the specific key / value pair is present in the multimap.
     *  * Without a value, this method returns `true` if the given key is present in the multimap.
     */
    has(key: K, value?: V): boolean;
    /**
     * Add the given key / value pair to the multimap.
     */
    add(key: K, value: V): this;
    /**
     * Add the given set of key / value pairs to the multimap.
     */
    addAll(key: K, values: Iterable<V>): this;
    /**
     * Invokes the given callback function for every key / value pair in the multimap.
     */
    forEach(callbackfn: (value: V, key: K, map: this) => void): void;
    /**
     * Returns an iterator of key, value pairs for every entry in the map.
     */
    [Symbol.iterator](): Iterator<[K, V]>;
    /**
     * Returns a stream of key, value pairs for every entry in the map.
     */
    entries(): Stream<[K, V]>;
    /**
     * Returns a stream of keys in the map.
     */
    keys(): Stream<K>;
    /**
     * Returns a stream of values in the map.
     */
    values(): Stream<V>;
    /**
     * Returns a stream of key, value set pairs for every key in the map.
     */
    entriesGroupedByKey(): Stream<[K, V[]]>;
}

declare class MultiMapScope implements Scope {
    readonly elements: MultiMap<string, AstNodeDescription>;
    readonly outerScope?: Scope;
    readonly caseInsensitive: boolean;
    readonly concatOuterScope: boolean;
    constructor(elements: Iterable<AstNodeDescription>, outerScope?: Scope, options?: ScopeOptions);
    getElement(name: string): AstNodeDescription | undefined;
    getElements(name: string): Stream<AstNodeDescription>;
    getAllElements(): Stream<AstNodeDescription>;
}

declare interface MultiReference<T extends AstNode = AstNode> {
    /** The CST node from which the reference was parsed */
    readonly $refNode?: CstNode;
    /** The actual text used to look up in the surrounding scope */
    readonly $refText: string;
    /**
     * The resolved references. Accessing this property may trigger cross-reference
     * resolution by the `Linker` in case it has not been done yet.
     * If no references can be found, the array is empty (but not `undefined`)
     * and the `error` property is set.
     */
    readonly items: Array<MultiReferenceItem<T>>;
    /** If any problem occurred while resolving the reference, it is described by this property. */
    readonly error?: LinkingError;
}

/**
 * Represents a single resolved reference of a {@link MultiReference} instance.
 */
declare interface MultiReferenceItem<T extends AstNode = AstNode> {
    /** The node description for the AstNode returned by `ref`  */
    readonly $nodeDescription?: AstNodeDescription;
    /**
     * The target AST node of this reference.
     */
    readonly ref: T;
}

declare type Mutable<T> = {
    -readonly [P in keyof T]: T[P];
};

declare interface NamedArgument extends langium.AstNode {
    readonly $container: RuleCall;
    readonly $type: 'NamedArgument';
    calledByName: boolean;
    parameter?: langium.Reference<Parameter>;
    value: Condition;
}

declare const NamedArgument: {
    readonly $type: "NamedArgument";
    readonly calledByName: "calledByName";
    readonly parameter: "parameter";
    readonly value: "value";
};

declare interface NamedAstNode extends AstNode {
    name: string;
}

/**
 * Utility service for retrieving the `name` of an `AstNode` or the `CstNode` containing a `name`.
 */
declare interface NameProvider {
    /**
     * Returns the `name` of a given AstNode.
     * @param node Specified `AstNode` whose name node shall be retrieved.
     */
    getName(node: AstNode): string | undefined;
    /**
     * Returns the `CstNode` which contains the parsed value of the `name` assignment.
     * @param node Specified `AstNode` whose name node shall be retrieved.
     */
    getNameNode(node: AstNode): CstNode | undefined;
}

declare interface NegatedToken extends TerminalElement {
    readonly $type: 'NegatedToken';
    terminal: AbstractElement;
}

declare const NegatedToken: {
    readonly $type: "NegatedToken";
    readonly cardinality: "cardinality";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
    readonly terminal: "terminal";
};

declare interface Negation extends langium.AstNode {
    readonly $container: Conjunction | Disjunction | Group | NamedArgument | Negation;
    readonly $type: 'Negation';
    value: Condition;
}

declare const Negation: {
    readonly $type: "Negation";
    readonly value: "value";
};

/******************************************************************************
 * Copyright 2021 TypeFox GmbH
 * This program and the accompanying materials are made available under the
 * terms of the MIT License, which is available in the project root.
 ******************************************************************************/
declare const NEWLINE_REGEXP: RegExp;

declare interface Note extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'EOL' | 'Note';
    evolution: number;
    text: string;
    visibility: number;
}

declare const Note: {
    readonly $type: "Note";
    readonly evolution: "evolution";
    readonly text: "text";
    readonly visibility: "visibility";
};

/**
 * Capabilities specific to the notebook document support.
 *
 * @since 3.17.0
 */
declare interface NotebookDocumentClientCapabilities {
    /**
     * Capabilities specific to notebook document synchronization
     *
     * @since 3.17.0
     */
    synchronization: NotebookDocumentSyncClientCapabilities;
}

/**
 * Notebook specific client capabilities.
 *
 * @since 3.17.0
 */
declare type NotebookDocumentSyncClientCapabilities = {
    /**
     * Whether implementation supports dynamic registration. If this is
     * set to `true` the client supports the new
     * `(TextDocumentRegistrationOptions & StaticRegistrationOptions)`
     * return value for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
    /**
     * The client supports sending execution summary data per cell.
     */
    executionSummarySupport?: boolean;
};

declare interface NumberLiteral extends langium.AstNode {
    readonly $container: ArrayLiteral | TypeAttribute;
    readonly $type: 'NumberLiteral';
    value: number;
}

declare const NumberLiteral: {
    readonly $type: "NumberLiteral";
    readonly value: "value";
};

/**
 * This symbol may be thrown in an asynchronous context by any Langium service that receives
 * a `CancellationToken`. This means that the promise returned by such a service is rejected with
 * this symbol as rejection reason.
 */
declare const OperationCancelled: unique symbol;

declare type Operator = '=' | '+=' | '?=' | undefined;

declare interface Option_2 extends langium_2.AstNode {
    readonly $container: Radar;
    readonly $type: 'Option';
    name: 'graticule' | 'max' | 'min' | 'showLegend' | 'ticks';
    value: boolean | number | string;
}

declare const Option_2: {
    readonly $type: "Option";
    readonly name: "name";
    readonly value: "value";
};

export declare interface Packet extends langium_2.AstNode {
    readonly $type: 'Packet';
    accDescr?: string;
    accTitle?: string;
    blocks: Array<PacketBlock>;
    title?: string;
}

export declare const Packet: {
    readonly $type: "Packet";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly blocks: "blocks";
    readonly title: "title";
};

/**
 * Declaration of `Packet` services.
 */
declare interface PacketAddedServices {
    parser: {
        TokenBuilder: PacketTokenBuilder;
        ValueConverter: CommonValueConverter;
    };
}

export declare interface PacketBlock extends langium_2.AstNode {
    readonly $container: Packet;
    readonly $type: 'EOL' | 'PacketBlock';
    bits: number;
    end?: number;
    label: string;
    start: number;
}

export declare const PacketBlock: {
    readonly $type: "PacketBlock";
    readonly bits: "bits";
    readonly end: "end";
    readonly label: "label";
    readonly start: "start";
};

export declare const PacketGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'PacketGrammar' language. */
declare namespace PacketGrammar {
    const Terminals: {
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        INT: RegExp;
        STRING: RegExp;
        NEWLINE: RegExp;
        WHITESPACE: RegExp;
        YAML: RegExp;
        DIRECTIVE: RegExp;
        SINGLE_LINE_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "+" | "-" | ":" | "packet" | "packet-beta";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Packet: Packet;
        PacketBlock: PacketBlock;
    };
}

/**
 * Dependency injection module that overrides Langium default services and
 * contributes the declared `Packet` services.
 */
export declare const PacketModule: Module<PacketServices, PartialLangiumCoreServices & PacketAddedServices>;

/**
 * Union of Langium default services and `Packet` services.
 */
export declare type PacketServices = LangiumCoreServices & PacketAddedServices;

declare class PacketTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare interface Parameter extends langium.AstNode {
    readonly $container: InfixRule | ParserRule;
    readonly $type: 'Parameter';
    name: string;
}

declare const Parameter: {
    readonly $type: "Parameter";
    readonly name: "name";
};

declare interface ParameterReference extends langium.AstNode {
    readonly $container: Conjunction | Disjunction | Group | NamedArgument | Negation;
    readonly $type: 'ParameterReference';
    parameter: langium.Reference<Parameter>;
}

declare const ParameterReference: {
    readonly $type: "ParameterReference";
    readonly parameter: "parameter";
};

export declare function parse(diagramType: 'info', text: string): Promise<Info>;

export declare function parse(diagramType: 'packet', text: string): Promise<Packet>;

export declare function parse(diagramType: 'pie', text: string): Promise<Pie>;

export declare function parse(diagramType: 'treeView', text: string): Promise<TreeView>;

export declare function parse(diagramType: 'architecture', text: string): Promise<Architecture>;

export declare function parse(diagramType: 'gitGraph', text: string): Promise<GitGraph>;

export declare function parse(diagramType: 'eventmodeling', text: string): Promise<EventModel>;

export declare function parse(diagramType: 'radar', text: string): Promise<Radar>;

export declare function parse(diagramType: 'railroad', text: string): Promise<Railroad>;

export declare function parse(diagramType: 'railroadEbnf', text: string): Promise<RailroadEbnf>;

export declare function parse(diagramType: 'railroadAbnf', text: string): Promise<RailroadAbnf>;

export declare function parse(diagramType: 'railroadPeg', text: string): Promise<RailroadPeg>;

export declare function parse(diagramType: 'treemap', text: string): Promise<Treemap>;

export declare function parse(diagramType: 'wardley', text: string): Promise<Wardley>;

export declare function parse(diagramType: 'cynefin', text: string): Promise<Cynefin>;

/**
 * Parses a JSDoc from a `CstNode` containing a comment.
 *
 * @param node A `CstNode` from a parsed Langium document.
 * @param options Parsing options specialized to your language. See {@link JSDocParseOptions}.
 */
declare function parseJSDoc(node: CstNode, options?: JSDocParseOptions): JSDocComment;

/**
 * Parses a JSDoc from a string comment.
 *
 * @param content A string containing the source of the JSDoc comment.
 * @param start The start position the comment occupies in the source document.
 * @param options Parsing options specialized to your language. See {@link JSDocParseOptions}.
 */
declare function parseJSDoc(content: string, start?: Position, options?: JSDocParseOptions): JSDocComment;

declare type ParseResult<T = AstNode> = {
    value: T;
    parserErrors: IRecognitionException[];
    lexerErrors: ILexingError[];
    lexerReport?: LexingReport;
};

declare interface ParserOptions {
    rule?: string;
}

declare interface ParserRule extends langium.AstNode {
    readonly $container: Grammar;
    readonly $type: 'ParserRule';
    dataType?: PrimitiveType;
    definition: AbstractElement;
    entry: boolean;
    fragment: boolean;
    inferredType?: InferredType;
    name: string;
    parameters: Array<Parameter>;
    returnType?: langium.Reference<AbstractType>;
}

declare const ParserRule: {
    readonly $type: "ParserRule";
    readonly dataType: "dataType";
    readonly definition: "definition";
    readonly entry: "entry";
    readonly fragment: "fragment";
    readonly inferredType: "inferredType";
    readonly name: "name";
    readonly parameters: "parameters";
    readonly returnType: "returnType";
};

declare class ParserWorker {
    protected readonly sendMessage: WorkerMessagePost;
    protected readonly _terminate: () => void;
    protected readonly onReadyEmitter: Emitter<void>;
    protected deferred: Deferred<ParseResult>;
    protected _ready: boolean;
    protected _parsing: boolean;
    get ready(): boolean;
    get onReady(): Event_2<void>;
    constructor(sendMessage: WorkerMessagePost, onMessage: WorkerMessageCallback, onError: WorkerMessageCallback, terminate: () => void);
    terminate(): void;
    lock(): void;
    unlock(): void;
    parse(text: string): Promise<ParseResult>;
}

/**
 * Language-specific core services to be partially overridden via dependency injection.
 */
declare type PartialLangiumCoreServices = DeepPartial<LangiumCoreServices>;

/**
 * Shared core services to be partially overridden via dependency injection.
 */
declare type PartialLangiumSharedCoreServices = DeepPartial<LangiumSharedCoreServices>;

/**
 * Determines whether the given input has a partial match with the specified regex.
 * @param regex The regex to partially match against
 * @param input The input string
 * @returns Whether any match exists.
 */
declare function partialMatches(regex: RegExp | string, input: string): boolean;

/**
 * Builds a partial regex from the input regex. A partial regex is able to match incomplete input strings. E.g.
 * a partial regex constructed from `/ab/` is able to match the string `a` without needing a following `b` character. However it won't match `b` alone.
 * @param regex The input regex to be converted.
 * @returns A partial regex constructed from the input regex.
 */
declare function partialRegExp(regex: RegExp | string): RegExp;

export declare interface PegAny extends PegPrimary {
    readonly $type: 'PegAny';
    dot: string;
}

export declare const PegAny: {
    readonly $type: "PegAny";
    readonly dot: "dot";
};

export declare interface PegGroup extends PegPrimary {
    readonly $type: 'PegGroup';
    element: PegOrderedChoice;
}

export declare const PegGroup: {
    readonly $type: "PegGroup";
    readonly element: "element";
};

export declare interface PegIdentifier extends PegPrimary {
    readonly $type: 'PegIdentifier';
    name: string;
}

export declare const PegIdentifier: {
    readonly $type: "PegIdentifier";
    readonly name: "name";
};

export declare interface PegLiteral extends PegPrimary {
    readonly $type: 'PegLiteral';
    value: string;
}

export declare const PegLiteral: {
    readonly $type: "PegLiteral";
    readonly value: "value";
};

export declare interface PegOrderedChoice extends langium_2.AstNode {
    readonly $container: PegGroup | PegRule;
    readonly $type: 'PegOrderedChoice';
    alternatives: Array<PegSequence>;
}

export declare const PegOrderedChoice: {
    readonly $type: "PegOrderedChoice";
    readonly alternatives: "alternatives";
};

export declare interface PegPrefix extends langium_2.AstNode {
    readonly $container: PegSequence;
    readonly $type: 'PegPrefix';
    operator?: string;
    suffix: PegSuffix;
}

export declare const PegPrefix: {
    readonly $type: "PegPrefix";
    readonly operator: "operator";
    readonly suffix: "suffix";
};

export declare interface PegPrimary extends langium_2.AstNode {
    readonly $type: 'PegAny' | 'PegGroup' | 'PegIdentifier' | 'PegLiteral' | 'PegPrimary';
}

export declare const PegPrimary: {
    readonly $type: "PegPrimary";
};

export declare interface PegRule extends langium_2.AstNode {
    readonly $container: RailroadPeg;
    readonly $type: 'PegRule';
    definition: PegOrderedChoice;
    name: string;
}

export declare const PegRule: {
    readonly $type: "PegRule";
    readonly definition: "definition";
    readonly name: "name";
};

export declare interface PegSequence extends langium_2.AstNode {
    readonly $container: PegOrderedChoice;
    readonly $type: 'PegSequence';
    elements: Array<PegPrefix>;
}

export declare const PegSequence: {
    readonly $type: "PegSequence";
    readonly elements: "elements";
};

export declare interface PegSuffix extends langium_2.AstNode {
    readonly $container: PegPrefix;
    readonly $type: 'PegSuffix';
    operator?: string;
    primary: PegPrimary;
}

export declare const PegSuffix: {
    readonly $type: "PegSuffix";
    readonly operator: "operator";
    readonly primary: "primary";
};

export declare interface Pie extends langium_2.AstNode {
    readonly $type: 'Pie';
    accDescr?: string;
    accTitle?: string;
    sections: Array<PieSection>;
    showData: boolean;
    title?: string;
}

export declare const Pie: {
    readonly $type: "Pie";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly sections: "sections";
    readonly showData: "showData";
    readonly title: "title";
};

/**
 * Declaration of `Pie` services.
 */
declare interface PieAddedServices {
    parser: {
        TokenBuilder: PieTokenBuilder;
        ValueConverter: PieValueConverter;
    };
}

export declare const PieGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'PieGrammar' language. */
declare namespace PieGrammar {
    const Terminals: {
        NUMBER_PIE: RegExp;
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        STRING: RegExp;
        NEWLINE: RegExp;
        WHITESPACE: RegExp;
        YAML: RegExp;
        DIRECTIVE: RegExp;
        SINGLE_LINE_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = ":" | "pie" | "showData";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Pie: Pie;
        PieSection: PieSection;
    };
}

/**
 * Dependency injection module that overrides Langium default services and
 * contributes the declared `Pie` services.
 */
export declare const PieModule: Module<PieServices, PartialLangiumCoreServices & PieAddedServices>;

export declare interface PieSection extends langium_2.AstNode {
    readonly $container: Pie;
    readonly $type: 'EOL' | 'PieSection';
    label: string;
    value: number;
}

export declare const PieSection: {
    readonly $type: "PieSection";
    readonly label: "label";
    readonly value: "value";
};

/**
 * Union of Langium default services and `Pie` services.
 */
export declare type PieServices = LangiumCoreServices & PieAddedServices;

declare class PieTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare class PieValueConverter extends AbstractMermaidValueConverter {
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

declare interface Pipeline extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'EOL' | 'Pipeline';
    components: Array<PipelineComponent>;
    parent: string;
}

declare const Pipeline: {
    readonly $type: "Pipeline";
    readonly components: "components";
    readonly parent: "parent";
};

declare interface PipelineComponent extends langium_2.AstNode {
    readonly $container: Pipeline;
    readonly $type: 'EOL' | 'PipelineComponent';
    evolution: number;
    label?: Label;
    name: string;
}

declare const PipelineComponent: {
    readonly $type: "PipelineComponent";
    readonly evolution: "evolution";
    readonly label: "label";
    readonly name: "name";
};

/**
 * Position in a text document expressed as zero-based line and character
 * offset. Prior to 3.17 the offsets were always based on a UTF-16 string
 * representation. So a string of the form `a𐐀b` the character offset of the
 * character `a` is 0, the character offset of `𐐀` is 1 and the character
 * offset of b is 3 since `𐐀` is represented using two code units in UTF-16.
 * Since 3.17 clients and servers can agree on a different string encoding
 * representation (e.g. UTF-8). The client announces it's supported encoding
 * via the client capability [`general.positionEncodings`](https://microsoft.github.io/language-server-protocol/specifications/specification-current/#clientCapabilities).
 * The value is an array of position encodings the client supports, with
 * decreasing preference (e.g. the encoding at index `0` is the most preferred
 * one). To stay backwards compatible the only mandatory encoding is UTF-16
 * represented via the string `utf-16`. The server can pick one of the
 * encodings offered by the client and signals that encoding back to the
 * client via the initialize result's property
 * [`capabilities.positionEncoding`](https://microsoft.github.io/language-server-protocol/specifications/specification-current/#serverCapabilities). If the string value
 * `utf-16` is missing from the client's capability `general.positionEncodings`
 * servers can safely assume that the client supports UTF-16. If the server
 * omits the position encoding in its initialize result the encoding defaults
 * to the string value `utf-16`. Implementation considerations: since the
 * conversion from one encoding into another requires the content of the
 * file / line the conversion is best done where the file is read which is
 * usually on the server side.
 *
 * Positions are line end character agnostic. So you can not specify a position
 * that denotes `\r|\n` or `\n|` where `|` represents the character offset.
 *
 * @since 3.17.0 - support for negotiated position encoding.
 */
declare interface Position {
    /**
     * Line position in a document (zero-based).
     *
     * If a line number is greater than the number of lines in a document, it defaults back to the number of lines in the document.
     * If a line number is negative, it defaults to 0.
     */
    line: uinteger;
    /**
     * Character offset on a line in a document (zero-based).
     *
     * The meaning of this offset is determined by the negotiated
     * `PositionEncodingKind`.
     *
     * If the character value is greater than the line length it defaults back to the
     * line length.
     */
    character: uinteger;
}

/**
 * The Position namespace provides helper functions to work with
 * {@link Position} literals.
 */
declare namespace Position {
    /**
     * Creates a new Position literal from the given line and character.
     * @param line The position's line.
     * @param character The position's character.
     */
    function create(line: uinteger, character: uinteger): Position;
    /**
     * Checks whether the given literal conforms to the {@link Position} interface.
     */
    function is(value: any): value is Position;
}

/**
 * Position in a text document expressed as zero-based line and character offset.
 * The offsets are based on a UTF-16 string representation. So a string of the form
 * `a𐐀b` the character offset of the character `a` is 0, the character offset of `𐐀`
 * is 1 and the character offset of b is 3 since `𐐀` is represented using two code
 * units in UTF-16.
 *
 * Positions are line end character agnostic. So you can not specify a position that
 * denotes `\r|\n` or `\n|` where `|` represents the character offset.
 */
declare interface Position_2 {
    /**
     * Line position in a document (zero-based).
     *
     * If a line number is greater than the number of lines in a document, it
     * defaults back to the number of lines in the document.
     * If a line number is negative, it defaults to 0.
     *
     * The above two properties are implementation specific.
     */
    line: number;
    /**
     * Character offset on a line in a document (zero-based).
     *
     * The meaning of this offset is determined by the negotiated
     * `PositionEncodingKind`.
     *
     * If the character value is greater than the line length it defaults back
     * to the line length. This property is implementation specific.
     */
    character: number;
}

/**
 * A set of predefined position encoding kinds.
 *
 * @since 3.17.0
 */
declare namespace PositionEncodingKind {
    /**
     * Character offsets count UTF-8 code units (e.g. bytes).
     */
    const UTF8: PositionEncodingKind;
    /**
     * Character offsets count UTF-16 code units.
     *
     * This is the default and must always be supported
     * by servers
     */
    const UTF16: PositionEncodingKind;
    /**
     * Character offsets count UTF-32 code units.
     *
     * Implementation note: these are the same as Unicode codepoints,
     * so this `PositionEncodingKind` may also be used for an
     * encoding-agnostic representation of character offsets.
     */
    const UTF32: PositionEncodingKind;
}

/**
 * A type indicating how positions are encoded,
 * specifically what column offsets mean.
 *
 * @since 3.17.0
 */
declare type PositionEncodingKind = string;

/**
 * Create a Langium parser without finalizing it. This is used to extract more detailed error
 * information when the parser is initially validated.
 */
declare function prepareLangiumParser(services: LangiumCoreServices): LangiumParser;

declare namespace PrepareSupportDefaultBehavior {
    /**
     * The client's default behavior is to select the identifier
     * according the to language's syntax rule.
     */
    const Identifier: 1;
}

declare type PrepareSupportDefaultBehavior = 1;

declare type PrimitiveType = 'Date' | 'bigint' | 'boolean' | 'number' | 'string';

declare type ProfilingCategory = 'validating' | 'parsing' | 'linking';

declare interface ProfilingRecord {
    identifier: string;
    date: Date;
    duration: number;
    entries: MultiMap<string, number>;
}

declare class ProfilingTask {
    protected startTime?: number;
    protected readonly addRecord: (record: ProfilingRecord) => void;
    protected readonly identifier: string;
    protected readonly stack: Array<{
        id: string;
        start: number;
        content: number;
    }>;
    protected readonly entries: MultiMap<string, number>;
    constructor(addRecord: (record: ProfilingRecord) => void, identifier: string);
    start(): void;
    stop(): void;
    startSubTask(subTaskId: string): void;
    stopSubTask(subTaskId: string): void;
}

declare type ProgressToken = number | string;

declare namespace ProgressToken {
    function is(value: any): value is number | string;
}

/**
 * The property names of a given AST node type.
 */
declare type Properties<N extends AstNode> = SpecificNodeProperties<N> extends never ? string : SpecificNodeProperties<N>;

/**
 * Describes the meta data of a property of an AST node.
 */
declare interface PropertyMetaData {
    /** The name of this property. */
    name: string;
    /**
     * Indicates that the property is mandatory in the AST node.
     * For example, if an AST node contains an array, but no elements of this array have been parsed,
     * we still expect an empty array instead of `undefined`.
     */
    defaultValue?: PropertyType;
    /**
     * If the property is a reference, this is the type of the reference target.
     */
    referenceType?: string;
}

/**
 * Represents a default value for an AST property.
 */
declare type PropertyType = number | string | boolean | PropertyType[];

/**
 * The publish diagnostic client capabilities.
 */
declare interface PublishDiagnosticsClientCapabilities {
    /**
     * Whether the clients accepts diagnostics with related information.
     */
    relatedInformation?: boolean;
    /**
     * Client supports the tag property to provide meta data about a diagnostic.
     * Clients supporting tags have to handle unknown tags gracefully.
     *
     * @since 3.15.0
     */
    tagSupport?: {
        /**
         * The tags supported by the client.
         */
        valueSet: DiagnosticTag_2[];
    };
    /**
     * Whether the client interprets the version property of the
     * `textDocument/publishDiagnostics` notification's parameter.
     *
     * @since 3.15.0
     */
    versionSupport?: boolean;
    /**
     * Client supports a codeDescription property
     *
     * @since 3.16.0
     */
    codeDescriptionSupport?: boolean;
    /**
     * Whether code action supports the `data` property which is
     * preserved between a `textDocument/publishDiagnostics` and
     * `textDocument/codeAction` request.
     *
     * @since 3.16.0
     */
    dataSupport?: boolean;
}

declare type QualifiedName = string;

export declare interface Radar extends langium_2.AstNode {
    readonly $type: 'Radar';
    accDescr?: string;
    accTitle?: string;
    axes: Array<Axis>;
    curves: Array<Curve>;
    options: Array<Option_2>;
    title?: string;
}

export declare const Radar: {
    readonly $type: "Radar";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly axes: "axes";
    readonly curves: "curves";
    readonly options: "options";
    readonly title: "title";
};

/**
 * Declaration of `Radar` services.
 */
declare interface RadarAddedServices {
    parser: {
        TokenBuilder: RadarTokenBuilder;
        ValueConverter: CommonValueConverter;
    };
}

export declare const RadarGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'RadarGrammar' language. */
declare namespace RadarGrammar {
    const Terminals: {
        GRATICULE: RegExp;
        BOOLEAN: RegExp;
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        NUMBER: RegExp;
        STRING: RegExp;
        ID: RegExp;
        NEWLINE: RegExp;
        WHITESPACE: RegExp;
        YAML: RegExp;
        DIRECTIVE: RegExp;
        SINGLE_LINE_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "," | ":" | "[" | "]" | "axis" | "curve" | "graticule" | "max" | "min" | "radar-beta" | "radar-beta:" | "showLegend" | "ticks" | "{" | "}";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Axis: Axis;
        Curve: Curve;
        Entry: Entry;
        Option: Option_2;
        Radar: Radar;
    };
}

/**
 * Dependency injection module that overrides Langium default services and
 * contributes the declared `Radar` services.
 */
export declare const RadarModule: Module<RadarServices, PartialLangiumCoreServices & RadarAddedServices>;

/**
 * Union of Langium default services and `Radar` services.
 */
export declare type RadarServices = LangiumCoreServices & RadarAddedServices;

declare class RadarTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

export declare interface Railroad extends langium_2.AstNode {
    readonly $type: 'Railroad';
    accDescr?: string;
    accTitle?: string;
    rules: Array<RailroadRule>;
    title?: string;
}

export declare const Railroad: {
    readonly $type: "Railroad";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly rules: "rules";
    readonly title: "title";
};

export declare interface RailroadAbnf extends langium_2.AstNode {
    readonly $type: 'RailroadAbnf';
    accDescr?: string;
    accTitle?: string;
    rules: Array<AbnfRule>;
    title?: string;
}

export declare const RailroadAbnf: {
    readonly $type: "RailroadAbnf";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly rules: "rules";
    readonly title: "title";
};

declare interface RailroadAbnfAddedServices {
    parser: {
        TokenBuilder: RailroadAbnfTokenBuilder;
        ValueConverter: RailroadAbnfValueConverter;
    };
}

export declare const RailroadAbnfGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'RailroadAbnfGrammar' language. */
declare namespace RailroadAbnfGrammar {
    const Terminals: {
        TITLE: RegExp;
        ACC_TITLE: RegExp;
        ACC_DESCR: RegExp;
        ABNF_RULENAME: RegExp;
        ABNF_STRING: RegExp;
        ABNF_NUMVAL: RegExp;
        ABNF_REPEAT: RegExp;
        ABNF_EXACT_REPEAT: RegExp;
        ABNF_WHITESPACE: RegExp;
        ABNF_YAML: RegExp;
        ABNF_DIRECTIVE: RegExp;
        ABNF_SINGLE_LINE_COMMENT: RegExp;
        ABNF_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "(" | ")" | "/" | ";" | "=" | "[" | "]" | "railroad-abnf-beta";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        AbnfAlternation: AbnfAlternation;
        AbnfConcatenation: AbnfConcatenation;
        AbnfElement: AbnfElement;
        AbnfGroup: AbnfGroup;
        AbnfNumVal: AbnfNumVal;
        AbnfOptionalGroup: AbnfOptionalGroup;
        AbnfPrimary: AbnfPrimary;
        AbnfRule: AbnfRule;
        AbnfRuleName: AbnfRuleName;
        AbnfStringLiteral: AbnfStringLiteral;
        RailroadAbnf: RailroadAbnf;
    };
}

export declare const RailroadAbnfModule: Module<RailroadAbnfServices, PartialLangiumCoreServices & RailroadAbnfAddedServices>;

export declare type RailroadAbnfServices = LangiumCoreServices & RailroadAbnfAddedServices;

declare class RailroadAbnfTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare class RailroadAbnfValueConverter extends AbstractMermaidValueConverter {
    protected runConverter(rule: GrammarAST.AbstractRule, input: string, cstNode: CstNode): ValueType;
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

declare interface RailroadAddedServices {
    parser: {
        TokenBuilder: RailroadTokenBuilder;
        ValueConverter: RailroadValueConverter;
    };
}

export declare interface RailroadChoiceExpr extends RailroadExpression {
    readonly $type: 'RailroadChoiceExpr';
    alternatives: Array<RailroadExpression>;
}

export declare const RailroadChoiceExpr: {
    readonly $type: "RailroadChoiceExpr";
    readonly alternatives: "alternatives";
};

export declare interface RailroadEbnf extends langium_2.AstNode {
    readonly $type: 'RailroadEbnf';
    accDescr?: string;
    accTitle?: string;
    rules: Array<EbnfRule>;
    title?: string;
}

export declare const RailroadEbnf: {
    readonly $type: "RailroadEbnf";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly rules: "rules";
    readonly title: "title";
};

declare interface RailroadEbnfAddedServices {
    parser: {
        TokenBuilder: RailroadEbnfTokenBuilder;
        ValueConverter: RailroadEbnfValueConverter;
    };
}

export declare const RailroadEbnfGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'RailroadEbnfGrammar' language. */
declare namespace RailroadEbnfGrammar {
    const Terminals: {
        TITLE: RegExp;
        ACC_TITLE: RegExp;
        ACC_DESCR: RegExp;
        EBNF_ID: RegExp;
        EBNF_STRING: RegExp;
        EBNF_SPECIAL_SEQUENCE: RegExp;
        EBNF_WHITESPACE: RegExp;
        EBNF_YAML: RegExp;
        EBNF_DIRECTIVE: RegExp;
        EBNF_SINGLE_LINE_COMMENT: RegExp;
        EBNF_BLOCK_COMMENT: RegExp;
        EBNF_ISO_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "(" | ")" | "*" | "+" | "," | "-" | "::=" | ";" | "=" | "?" | "[" | "]" | "railroad-ebnf-beta" | "{" | "|" | "}";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        EbnfChoice: EbnfChoice;
        EbnfExceptionPostfix: EbnfExceptionPostfix;
        EbnfGroup: EbnfGroup;
        EbnfNonTerminal: EbnfNonTerminal;
        EbnfOneOrMorePostfix: EbnfOneOrMorePostfix;
        EbnfOptional: EbnfOptional;
        EbnfOptionalPostfix: EbnfOptionalPostfix;
        EbnfPostfix: EbnfPostfix;
        EbnfPrimary: EbnfPrimary;
        EbnfRepetition: EbnfRepetition;
        EbnfRule: EbnfRule;
        EbnfSequence: EbnfSequence;
        EbnfSpecial: EbnfSpecial;
        EbnfTerm: EbnfTerm;
        EbnfTerminal: EbnfTerminal;
        EbnfZeroOrMorePostfix: EbnfZeroOrMorePostfix;
        RailroadEbnf: RailroadEbnf;
    };
}

export declare const RailroadEbnfModule: Module<RailroadEbnfServices, PartialLangiumCoreServices & RailroadEbnfAddedServices>;

export declare type RailroadEbnfServices = LangiumCoreServices & RailroadEbnfAddedServices;

declare class RailroadEbnfTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare class RailroadEbnfValueConverter extends AbstractMermaidValueConverter {
    protected runConverter(rule: GrammarAST.AbstractRule, input: string, cstNode: CstNode): ValueType;
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

export declare interface RailroadExpression extends langium_2.AstNode {
    readonly $type: 'RailroadChoiceExpr' | 'RailroadExpression' | 'RailroadNonTerminalExpr' | 'RailroadOneOrMoreExpr' | 'RailroadOptionalExpr' | 'RailroadSequenceExpr' | 'RailroadSpecialExpr' | 'RailroadTerminalExpr' | 'RailroadZeroOrMoreExpr';
}

export declare const RailroadExpression: {
    readonly $type: "RailroadExpression";
};

export declare const RailroadGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'RailroadGrammar' language. */
declare namespace RailroadGrammar {
    const Terminals: {
        TITLE: RegExp;
        ACC_TITLE: RegExp;
        ACC_DESCR: RegExp;
        RR_ID: RegExp;
        RR_STRING: RegExp;
        RR_WHITESPACE: RegExp;
        RR_YAML: RegExp;
        RR_DIRECTIVE: RegExp;
        RR_SINGLE_LINE_COMMENT: RegExp;
        RR_BLOCK_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "(" | ")" | "," | ";" | "=" | "choice" | "nonterminal" | "oneOrMore" | "optional" | "railroad-beta" | "sequence" | "special" | "terminal" | "zeroOrMore";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Railroad: Railroad;
        RailroadChoiceExpr: RailroadChoiceExpr;
        RailroadExpression: RailroadExpression;
        RailroadNonTerminalExpr: RailroadNonTerminalExpr;
        RailroadOneOrMoreExpr: RailroadOneOrMoreExpr;
        RailroadOptionalExpr: RailroadOptionalExpr;
        RailroadRule: RailroadRule;
        RailroadSequenceExpr: RailroadSequenceExpr;
        RailroadSpecialExpr: RailroadSpecialExpr;
        RailroadTerminalExpr: RailroadTerminalExpr;
        RailroadZeroOrMoreExpr: RailroadZeroOrMoreExpr;
    };
}

export declare const RailroadModule: Module<RailroadServices, PartialLangiumCoreServices & RailroadAddedServices>;

export declare interface RailroadNonTerminalExpr extends RailroadExpression {
    readonly $type: 'RailroadNonTerminalExpr';
    name: string;
}

export declare const RailroadNonTerminalExpr: {
    readonly $type: "RailroadNonTerminalExpr";
    readonly name: "name";
};

export declare interface RailroadOneOrMoreExpr extends RailroadExpression {
    readonly $type: 'RailroadOneOrMoreExpr';
    element: RailroadExpression;
}

export declare const RailroadOneOrMoreExpr: {
    readonly $type: "RailroadOneOrMoreExpr";
    readonly element: "element";
};

export declare interface RailroadOptionalExpr extends RailroadExpression {
    readonly $type: 'RailroadOptionalExpr';
    element: RailroadExpression;
}

export declare const RailroadOptionalExpr: {
    readonly $type: "RailroadOptionalExpr";
    readonly element: "element";
};

export declare interface RailroadPeg extends langium_2.AstNode {
    readonly $type: 'RailroadPeg';
    accDescr?: string;
    accTitle?: string;
    rules: Array<PegRule>;
    title?: string;
}

export declare const RailroadPeg: {
    readonly $type: "RailroadPeg";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly rules: "rules";
    readonly title: "title";
};

declare interface RailroadPegAddedServices {
    parser: {
        TokenBuilder: RailroadPegTokenBuilder;
        ValueConverter: RailroadPegValueConverter;
    };
}

export declare const RailroadPegGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'RailroadPegGrammar' language. */
declare namespace RailroadPegGrammar {
    const Terminals: {
        TITLE: RegExp;
        ACC_TITLE: RegExp;
        ACC_DESCR: RegExp;
        PEG_ID: RegExp;
        PEG_STRING: RegExp;
        PEG_WHITESPACE: RegExp;
        PEG_YAML: RegExp;
        PEG_DIRECTIVE: RegExp;
        PEG_SINGLE_LINE_COMMENT: RegExp;
        PEG_LINE_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "!" | "&" | "(" | ")" | "*" | "+" | "." | "/" | ";" | "<-" | "?" | "railroad-peg-beta";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        PegAny: PegAny;
        PegGroup: PegGroup;
        PegIdentifier: PegIdentifier;
        PegLiteral: PegLiteral;
        PegOrderedChoice: PegOrderedChoice;
        PegPrefix: PegPrefix;
        PegPrimary: PegPrimary;
        PegRule: PegRule;
        PegSequence: PegSequence;
        PegSuffix: PegSuffix;
        RailroadPeg: RailroadPeg;
    };
}

export declare const RailroadPegModule: Module<RailroadPegServices, PartialLangiumCoreServices & RailroadPegAddedServices>;

export declare type RailroadPegServices = LangiumCoreServices & RailroadPegAddedServices;

declare class RailroadPegTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare class RailroadPegValueConverter extends AbstractMermaidValueConverter {
    protected runConverter(rule: GrammarAST.AbstractRule, input: string, cstNode: CstNode): ValueType;
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

export declare interface RailroadRule extends langium_2.AstNode {
    readonly $container: Railroad;
    readonly $type: 'RailroadRule';
    definition: RailroadExpression;
    name: string;
}

export declare const RailroadRule: {
    readonly $type: "RailroadRule";
    readonly definition: "definition";
    readonly name: "name";
};

export declare interface RailroadSequenceExpr extends RailroadExpression {
    readonly $type: 'RailroadSequenceExpr';
    elements: Array<RailroadExpression>;
}

export declare const RailroadSequenceExpr: {
    readonly $type: "RailroadSequenceExpr";
    readonly elements: "elements";
};

export declare type RailroadServices = LangiumCoreServices & RailroadAddedServices;

export declare interface RailroadSpecialExpr extends RailroadExpression {
    readonly $type: 'RailroadSpecialExpr';
    text: string;
}

export declare const RailroadSpecialExpr: {
    readonly $type: "RailroadSpecialExpr";
    readonly text: "text";
};

export declare interface RailroadTerminalExpr extends RailroadExpression {
    readonly $type: 'RailroadTerminalExpr';
    value: string;
}

export declare const RailroadTerminalExpr: {
    readonly $type: "RailroadTerminalExpr";
    readonly value: "value";
};

declare class RailroadTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare class RailroadValueConverter extends AbstractMermaidValueConverter {
    protected runConverter(rule: GrammarAST.AbstractRule, input: string, cstNode: CstNode): ValueType;
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

export declare interface RailroadZeroOrMoreExpr extends RailroadExpression {
    readonly $type: 'RailroadZeroOrMoreExpr';
    element: RailroadExpression;
}

export declare const RailroadZeroOrMoreExpr: {
    readonly $type: "RailroadZeroOrMoreExpr";
    readonly element: "element";
};

/**
 * A range in a text document expressed as (zero-based) start and end positions.
 *
 * If you want to specify a range that contains a line including the line ending
 * character(s) then use an end position denoting the start of the next line.
 * For example:
 * ```ts
 * {
 *     start: { line: 5, character: 23 }
 *     end : { line 6, character : 0 }
 * }
 * ```
 */
declare interface Range_2 {
    /**
     * The range's start position.
     */
    start: Position;
    /**
     * The range's end position.
     */
    end: Position;
}

/**
 * The Range namespace provides helper functions to work with
 * {@link Range} literals.
 */
declare namespace Range_2 {
    /**
     * Create a new Range literal.
     * @param start The range's start position.
     * @param end The range's end position.
     */
    function create(start: Position, end: Position): Range_2;
    /**
     * Create a new Range literal.
     * @param startLine The start line number.
     * @param startCharacter The start character.
     * @param endLine The end line number.
     * @param endCharacter The end character.
     */
    function create(startLine: uinteger, startCharacter: uinteger, endLine: uinteger, endCharacter: uinteger): Range_2;
    /**
     * Checks whether the given literal conforms to the {@link Range} interface.
     */
    function is(value: any): value is Range_2;
}

/**
 * A range in a text document expressed as (zero-based) start and end positions.
 *
 * If you want to specify a range that contains a line including the line ending
 * character(s) then use an end position denoting the start of the next line.
 * For example:
 * ```ts
 * {
 *     start: { line: 5, character: 23 }
 *     end : { line 6, character : 0 }
 * }
 * ```
 */
declare interface Range_3 {
    /**
     * The range's start position.
     */
    start: Position_2;
    /**
     * The range's end position.
     */
    end: Position_2;
}

declare enum RangeComparison {
    Before = 0,
    After = 1,
    OverlapFront = 2,
    OverlapBack = 3,
    Inside = 4,
    Outside = 5
}

/**
 * Exclude/omit all `AstNode` attributes recursively.
 */
export declare type RecursiveAstOmit<T> = T extends object ? {
    [P in keyof T as Exclude<P, keyof AstNode>]: RecursiveAstOmit<T[P]>;
} : T;

/**
 * A set of utility functions that reduce a stream to a single value.
 */
declare namespace Reduction {
    /**
     * Compute the sum of a number stream.
     */
    function sum(stream: Stream<number>): number;
    /**
     * Compute the product of a number stream.
     */
    function product(stream: Stream<number>): number;
    /**
     * Compute the minimum of a number stream. Returns `undefined` if the stream is empty.
     */
    function min(stream: Stream<number>): number | undefined;
    /**
     * Compute the maximum of a number stream. Returns `undefined` if the stream is empty.
     */
    function max(stream: Stream<number>): number | undefined;
}

/**
 * A cross-reference in the AST. Cross-references may or may not be successfully resolved.
 */
declare interface Reference<T extends AstNode = AstNode> {
    /**
     * The target AST node of this reference. Accessing this property may trigger cross-reference
     * resolution by the `Linker` in case it has not been done yet. If the reference cannot be resolved,
     * the value is `undefined`.
     */
    readonly ref: T | undefined;
    /** If any problem occurred while resolving the reference, it is described by this property. */
    readonly error?: LinkingError;
    /** The CST node from which the reference was parsed */
    readonly $refNode?: CstNode;
    /** The actual text used to look up in the surrounding scope */
    readonly $refText: string;
    /** The node description for the AstNode returned by `ref`  */
    readonly $nodeDescription?: AstNodeDescription;
}

/**
 * Client Capabilities for a {@link ReferencesRequest}.
 */
declare interface ReferenceClientCapabilities {
    /**
     * Whether references supports dynamic registration.
     */
    dynamicRegistration?: boolean;
}

/**
 * Describes a cross-reference within a document or between two documents.
 */
declare interface ReferenceDescription {
    /** URI of the document that holds a reference */
    sourceUri: URI;
    /** Path to AstNode that holds a reference */
    sourcePath: string;
    /** Target document uri */
    targetUri: URI;
    /** Path to the target AstNode inside the document */
    targetPath: string;
    /** Segment of the reference text. */
    segment: DocumentSegment;
    /** Marks a local reference i.e. a cross reference inside a document.   */
    local?: boolean;
}

/**
 * Language-specific service to create descriptions of all cross-references in a document. These are used by the `IndexManager`
 * to determine which documents are affected and should be rebuilt when a document is changed.
 */
declare interface ReferenceDescriptionProvider {
    /**
     * Create descriptions of all cross-references found in the given document. These descriptions are
     * gathered by the `IndexManager` and stored in the global index so they can be considered when
     * a document change is reported by the client.
     *
     * @param document The document in which to gather cross-references.
     * @param cancelToken Indicates when to cancel the current operation.
     * @throws `OperationCanceled` if a user action occurs during execution
     */
    createDescriptions(document: LangiumDocument, cancelToken?: CancellationToken): Promise<ReferenceDescription[]>;
}

/**
 * Information about a cross-reference. This is used when traversing references in an AST or to describe
 * unresolved references.
 */
declare interface ReferenceInfo {
    reference: Reference | MultiReference;
    container: AstNode;
    property: string;
    index?: number;
}

/**
 * Language-specific service for finding references and declaration of a given `CstNode`.
 */
declare interface References {
    /**
     * If the CstNode is a reference node the target AstNodes will be returned.
     * If the CstNode is a significant node of the CstNode this AstNode will be returned.
     *
     * @param sourceCstNode CstNode that points to a AstNode
     */
    findDeclarations(sourceCstNode: CstNode): AstNode[];
    /**
     * If the CstNode is a reference node the target CstNodes will be returned.
     * If the CstNode is a significant node of the CstNode this CstNode will be returned.
     *
     * @param sourceCstNode CstNode that points to a AstNode
     */
    findDeclarationNodes(sourceCstNode: CstNode): CstNode[];
    /**
     * Finds all references to the target node as references (local references) or reference descriptions.
     *
     * @param targetNode Specified target node whose references should be returned
     */
    findReferences(targetNode: AstNode, options: FindReferencesOptions): Stream<ReferenceDescription>;
}

declare interface ReferenceType extends langium.AstNode {
    readonly $container: ArrayType | ReferenceType | Type | TypeAttribute | UnionType;
    readonly $type: 'ReferenceType';
    isMulti: boolean;
    referenceType: TypeDefinition;
}

declare const ReferenceType: {
    readonly $type: "ReferenceType";
    readonly isMulti: "isMulti";
    readonly referenceType: "referenceType";
};

declare const reflection: LangiumGrammarAstReflection;

declare const RefResolving: unique symbol;

declare namespace RegExpUtils {
    export {
        getTerminalParts,
        isMultilineComment,
        isWhitespace,
        escapeRegExp,
        partialMatches,
        partialRegExp,
        NEWLINE_REGEXP,
        whitespaceCharacters
    }
}

declare interface RegexToken extends TerminalElement {
    readonly $type: 'RegexToken';
    regex: string;
}

declare const RegexToken: {
    readonly $type: "RegexToken";
    readonly cardinality: "cardinality";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
    readonly regex: "regex";
};

/**
 * Client capabilities specific to regular expressions.
 *
 * @since 3.16.0
 */
declare interface RegularExpressionsClientCapabilities {
    /**
     * The engine's name.
     */
    engine: string;
    /**
     * The engine's version.
     */
    version?: string;
}

declare interface RenameClientCapabilities {
    /**
     * Whether rename supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * Client supports testing for validity of rename operations
     * before execution.
     *
     * @since 3.12.0
     */
    prepareSupport?: boolean;
    /**
     * Client supports the default behavior result.
     *
     * The value indicates the default behavior used by the
     * client.
     *
     * @since 3.16.0
     */
    prepareSupportDefaultBehavior?: PrepareSupportDefaultBehavior;
    /**
     * Whether the client honors the change annotations in
     * text edits and resource operations returned via the
     * rename request's workspace edit by for example presenting
     * the workspace edit in the user interface and asking
     * for confirmation.
     *
     * @since 3.16.0
     */
    honorsChangeAnnotations?: boolean;
}

declare type ResolvedReference<T extends AstNode = AstNode> = Reference<T> & {
    readonly ref: T;
};

/**
 * The kind of resource operations supported by the client.
 */
declare type ResourceOperationKind = 'create' | 'rename' | 'delete';

declare namespace ResourceOperationKind {
    /**
     * Supports creating new files and folders.
     */
    const Create: ResourceOperationKind;
    /**
     * Supports renaming existing files and folders.
     */
    const Rename: ResourceOperationKind;
    /**
     * Supports deleting existing files and folders.
     */
    const Delete: ResourceOperationKind;
}

declare interface ReturnType_2 extends langium.AstNode {
    readonly $container: TerminalRule;
    readonly $type: 'ReturnType';
    name: PrimitiveType | string;
}

declare const ReturnType_2: {
    readonly $type: "ReturnType";
    readonly name: "name";
};

declare interface RootCstNode extends CompositeCstNode {
    readonly fullText: string;
}

declare class RootCstNodeImpl extends CompositeCstNodeImpl implements RootCstNode {
    private _text;
    get text(): string;
    get fullText(): string;
    constructor(input?: string);
}

declare interface RuleCall extends AbstractElement {
    readonly $container: InfixRule;
    readonly $type: 'RuleCall';
    arguments: Array<NamedArgument>;
    predicate?: '->' | '=>';
    rule: langium.Reference<AbstractRule>;
}

declare const RuleCall: {
    readonly $type: "RuleCall";
    readonly arguments: "arguments";
    readonly cardinality: "cardinality";
    readonly predicate: "predicate";
    readonly rule: "rule";
};

declare type RuleImpl = (args: Args) => any;

declare type RuleResult = (args: Args) => any;

/**
 * A scope describes what target elements are visible from a specific cross-reference context.
 */
declare interface Scope {
    /**
     * Find a target element matching the given name. If no element is found, `undefined` is returned.
     * If multiple matching elements are present, the selection of the returned element should be done
     * according to the semantics of your language. Usually it is the element that is most closely defined.
     *
     * @param name Name of the cross-reference target as it appears in the source text.
     */
    getElement(name: string): AstNodeDescription | undefined;
    /**
     * Finds all target elements matching the given name. If no element is found, an empty stream is returned.
     *
     * @param name Name of the cross-reference target as it appears in the source text.
     */
    getElements(name: string): Stream<AstNodeDescription>;
    /**
     * Create a stream of all elements in the scope. This is used to compute completion proposals to be
     * shown in the editor.
     */
    getAllElements(): Stream<AstNodeDescription>;
}

/**
 * Language-specific service for precomputing global and local scopes. The service methods are executed
 * as the first and second phase in the `DocumentBuilder`.
 */
declare interface ScopeComputation {
    /**
     * Creates descriptions of all AST nodes that shall be exported into the _global_ scope from the given
     * document. These descriptions are gathered by the `IndexManager` and stored in the global index so
     * they can be referenced from other documents.
     *
     * _Note:_ You should not resolve any cross-references in this service method. Cross-reference resolution
     * depends on the scope computation phase to be completed (`computeScope` method), which runs after the
     * initial indexing where this method is used.
     *
     * @param document The document from which to gather exported AST nodes.
     * @param cancelToken Indicates when to cancel the current operation.
     * @throws `OperationCanceled` if a user action occurs during execution
     */
    collectExportedSymbols(document: LangiumDocument, cancelToken?: CancellationToken): Promise<AstNodeDescription[]>;
    /**
     * Creates descriptions of the _local_ symbols being accessible within a document.
     * The result is a `LocalSymbols` table assigning sets of AST node descriptions to the corresponding
     * nodes/subtrees within the AST. The descriptions are considered in the default reference resolution
     * implementation, i.e. they are used by the `ScopeProvider` service to determine which symbols
     * are visible in the context of a specific cross-reference.
     *
     * _Note:_ You should not resolve any cross-references in this service method. Cross-reference
     * resolution depends on the scope computation phase to be completed.
     *
     * @param document The document for which to compute its local symbols.
     * @param cancelToken Indicates when to cancel the current operation.
     * @throws `OperationCanceled` if a user action occurs during execution
     */
    collectLocalSymbols(document: LangiumDocument, cancelToken?: CancellationToken): Promise<LocalSymbols>;
}

declare interface ScopeOptions {
    /**
     * Whether the scope should be case insensitive.
     * Defaults to `false`.
     */
    caseInsensitive?: boolean;
    /**
     * Whether the outer scope should be concatenated with the local scope when calling `getElements`.
     * Defaults to `true`.
     */
    concatOuterScope?: boolean;
}

/**
 * Language-specific service for determining the scope of target elements visible in a specific cross-reference context.
 */
declare interface ScopeProvider {
    /**
     * Return a scope describing what elements are visible for the given AST node and cross-reference
     * identifier.
     *
     * @param context Information about the reference for which a scope is requested.
     */
    getScope(context: ReferenceInfo): Scope;
}

declare interface Section extends Item {
    readonly $type: 'Section';
}

declare const Section: {
    readonly $type: "Section";
    readonly classSelector: "classSelector";
    readonly name: "name";
};

declare interface SelectionRangeClientCapabilities {
    /**
     * Whether implementation supports dynamic registration for selection range providers. If this is set to `true`
     * the client supports the new `SelectionRangeRegistrationOptions` return value for the corresponding server
     * capability as well.
     */
    dynamicRegistration?: boolean;
}

/**
 * @since 3.16.0
 */
declare interface SemanticTokensClientCapabilities {
    /**
     * Whether implementation supports dynamic registration. If this is set to `true`
     * the client supports the new `(TextDocumentRegistrationOptions & StaticRegistrationOptions)`
     * return value for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
    /**
     * Which requests the client supports and might send to the server
     * depending on the server's capability. Please note that clients might not
     * show semantic tokens or degrade some of the user experience if a range
     * or full request is advertised by the client but not provided by the
     * server. If for example the client capability `requests.full` and
     * `request.range` are both set to true but the server only provides a
     * range provider the client might not render a minimap correctly or might
     * even decide to not show any semantic tokens at all.
     */
    requests: {
        /**
         * The client will send the `textDocument/semanticTokens/range` request if
         * the server provides a corresponding handler.
         */
        range?: boolean | {};
        /**
         * The client will send the `textDocument/semanticTokens/full` request if
         * the server provides a corresponding handler.
         */
        full?: boolean | {
            /**
             * The client will send the `textDocument/semanticTokens/full/delta` request if
             * the server provides a corresponding handler.
             */
            delta?: boolean;
        };
    };
    /**
     * The token types that the client supports.
     */
    tokenTypes: string[];
    /**
     * The token modifiers that the client supports.
     */
    tokenModifiers: string[];
    /**
     * The token formats the clients supports.
     */
    formats: TokenFormat[];
    /**
     * Whether the client supports tokens that can overlap each other.
     */
    overlappingTokenSupport?: boolean;
    /**
     * Whether the client supports tokens that can span multiple lines.
     */
    multilineTokenSupport?: boolean;
    /**
     * Whether the client allows the server to actively cancel a
     * semantic token request, e.g. supports returning
     * LSPErrorCodes.ServerCancelled. If a server does the client
     * needs to retrigger the request.
     *
     * @since 3.17.0
     */
    serverCancelSupport?: boolean;
    /**
     * Whether the client uses semantic tokens to augment existing
     * syntax tokens. If set to `true` client side created syntax
     * tokens and semantic tokens are both used for colorization. If
     * set to `false` the client only uses the returned semantic tokens
     * for colorization.
     *
     * If the value is `undefined` then the client behavior is not
     * specified.
     *
     * @since 3.17.0
     */
    augmentsSyntaxTokens?: boolean;
}

/**
 * @since 3.16.0
 */
declare interface SemanticTokensWorkspaceClientCapabilities {
    /**
     * Whether the client implementation supports a refresh request sent from
     * the server to the client.
     *
     * Note that this event is global and will force the client to refresh all
     * semantic tokens currently shown. It should be used with absolute care
     * and is useful for situation where a server for example detects a project
     * wide change that requires such a calculation.
     */
    refreshSupport?: boolean;
}

declare interface Service extends langium_2.AstNode {
    readonly $container: Architecture;
    readonly $type: 'EOL' | 'Service';
    icon?: string;
    iconText?: string;
    id: string;
    in: string;
    title?: string;
}

declare const Service: {
    readonly $type: "Service";
    readonly icon: "icon";
    readonly iconText: "iconText";
    readonly id: "id";
    readonly in: "in";
    readonly title: "title";
};

/**
 * The service registry provides access to the language-specific {@link LangiumCoreServices} optionally including LSP-related services.
 * These are resolved via the URI of a text document.
 */
declare interface ServiceRegistry {
    /**
     * Register a language via its injected services.
     */
    register(language: LangiumCoreServices): void;
    /**
     * Retrieve the language-specific services for the given URI. In case only one language is
     * registered, it may be used regardless of the URI format.
     */
    getServices(uri: URI): LangiumCoreServices;
    /**
     * Check whether services are available for the given URI.
     */
    hasServices(uri: URI): boolean;
    /**
     * The full set of registered language services.
     */
    readonly all: readonly LangiumCoreServices[];
}

/**
 * Change the period duration for `interruptAndCheck` to the given number of milliseconds.
 * The default value is 10ms.
 */
declare function setInterruptionPeriod(period: number): void;

/**
 * Client capabilities for the showDocument request.
 *
 * @since 3.16.0
 */
declare interface ShowDocumentClientCapabilities {
    /**
     * The client has support for the showDocument
     * request.
     */
    support: boolean;
}

/**
 * Show message request client capabilities
 */
declare interface ShowMessageRequestClientCapabilities {
    /**
     * Capabilities specific to the `MessageActionItem` type.
     */
    messageActionItem?: {
        /**
         * Whether the client supports additional attributes which
         * are preserved and send back to the server in the
         * request's response.
         */
        additionalPropertiesSupport?: boolean;
    };
}

/**
 * Client Capabilities for a {@link SignatureHelpRequest}.
 */
declare interface SignatureHelpClientCapabilities {
    /**
     * Whether signature help supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * The client supports the following `SignatureInformation`
     * specific properties.
     */
    signatureInformation?: {
        /**
         * Client supports the following content formats for the documentation
         * property. The order describes the preferred format of the client.
         */
        documentationFormat?: MarkupKind[];
        /**
         * Client capabilities specific to parameter information.
         */
        parameterInformation?: {
            /**
             * The client supports processing label offsets instead of a
             * simple label string.
             *
             * @since 3.14.0
             */
            labelOffsetSupport?: boolean;
        };
        /**
         * The client supports the `activeParameter` property on `SignatureInformation`
         * literal.
         *
         * @since 3.16.0
         */
        activeParameterSupport?: boolean;
    };
    /**
     * The client supports to send additional context information for a
     * `textDocument/signatureHelp` request. A client that opts into
     * contextSupport will also support the `retriggerCharacters` on
     * `SignatureHelpOptions`.
     *
     * @since 3.15.0
     */
    contextSupport?: boolean;
}

declare class SimpleCache<K, V> extends DisposableCache {
    protected readonly cache: Map<K, V>;
    has(key: K): boolean;
    set(key: K, value: V): void;
    get(key: K): V | undefined;
    get(key: K, provider: () => V): V;
    delete(key: K): boolean;
    clear(): void;
}

declare interface SimpleType extends langium.AstNode {
    readonly $container: ArrayType | ReferenceType | Type | TypeAttribute | UnionType;
    readonly $type: 'SimpleType';
    primitiveType?: PrimitiveType;
    stringType?: string;
    typeRef?: langium.Reference<AbstractType>;
}

declare const SimpleType: {
    readonly $type: "SimpleType";
    readonly primitiveType: "primitiveType";
    readonly stringType: "stringType";
    readonly typeRef: "typeRef";
};

declare type SingleCrossReferencesOfAstNodeType<N extends AstNode> = ExtractKeysOfValueType<N, Reference | Reference[]>;

declare interface Size extends langium_2.AstNode {
    readonly $container: Wardley;
    readonly $type: 'EOL' | 'Size';
    height: number;
    width: number;
}

declare const Size: {
    readonly $type: "Size";
    readonly height: "height";
    readonly width: "width";
};

declare type SpecificNodeProperties<N extends AstNode> = keyof Omit<N, keyof AstNode | number | symbol>;

/**
 * Reset the global interruption period and create a cancellation token source.
 */
declare function startCancelableOperation(): AbstractCancellationTokenSource;

export declare type Statement = Branch | Checkout | CherryPicking | Commit | Merge;

export declare const Statement: {
    readonly $type: "Statement";
};

/******************************************************************************
 * Copyright 2021 TypeFox GmbH
 * This program and the accompanying materials are made available under the
 * terms of the MIT License, which is available in the project root.
 ******************************************************************************/
/**
 * A stream is a read-only sequence of values. While the contents of an array can be accessed
 * both sequentially and randomly (via index), a stream allows only sequential access.
 *
 * The advantage of this is that a stream can be evaluated lazily, so it does not require
 * to store intermediate values. This can boost performance when a large sequence is
 * processed via filtering, mapping etc. and accessed at most once. However, lazy
 * evaluation means that all processing is repeated when you access the sequence multiple
 * times; in such a case, it may be better to store the resulting sequence into an array.
 */
declare interface Stream<T> extends Iterable<T> {
    /**
     * Returns an iterator for this stream. This is the same as calling the `Symbol.iterator` function property.
     */
    iterator(): IterableIterator<T>;
    /**
     * Determines whether this stream contains no elements.
     */
    isEmpty(): boolean;
    /**
     * Determines the number of elements in this stream.
     */
    count(): number;
    /**
     * Collects all elements of this stream into an array.
     */
    toArray(): T[];
    /**
     * Collects all elements of this stream into a Set.
     */
    toSet(): Set<T>;
    /**
     * Collects all elements of this stream into a Map, applying the provided functions to determine keys and values.
     *
     * @param keyFn The function to derive map keys. If omitted, the stream elements are used as keys.
     * @param valueFn The function to derive map values. If omitted, the stream elements are used as values.
     */
    toMap<K = T, V = T>(keyFn?: (e: T) => K, valueFn?: (e: T) => V): Map<K, V>;
    /**
     * Returns a string representation of a stream.
     */
    toString(): string;
    /**
     * Combines two streams by returning a new stream that yields all elements of this stream and the other stream.
     *
     * @param other Stream to be concatenated with this one.
     */
    concat<T2>(other: Iterable<T2>): Stream<T | T2>;
    /**
     * Adds all elements of the stream into a string, separated by the specified separator string.
     *
     * @param separator A string used to separate one element of the stream from the next in the resulting string.
     *        If omitted, the steam elements are separated with a comma.
     */
    join(separator?: string): string;
    /**
     * Returns the index of the first occurrence of a value in the stream, or -1 if it is not present.
     *
     * @param searchElement The value to locate in the array.
     * @param fromIndex The stream index at which to begin the search. If fromIndex is omitted, the search
     *        starts at index 0.
     */
    indexOf(searchElement: T, fromIndex?: number): number;
    /**
     * Determines whether all members of the stream satisfy the specified test.
     *
     * @param predicate This method calls the predicate function for each element in the stream until the
     *        predicate returns a value which is coercible to the Boolean value `false`, or until the end
     *        of the stream.
     */
    every<S extends T>(predicate: (value: T) => value is S): this is Stream<S>;
    every(predicate: (value: T) => unknown): boolean;
    /**
     * Determines whether any member of the stream satisfies the specified test.
     *
     * @param predicate This method calls the predicate function for each element in the stream until the
     *        predicate returns a value which is coercible to the Boolean value `true`, or until the end
     *        of the stream.
     */
    some(predicate: (value: T) => unknown): boolean;
    /**
     * Performs the specified action for each element in the stream.
     *
     * @param callbackfn Function called once for each element in the stream.
     */
    forEach(callbackfn: (value: T, index: number) => void): void;
    /**
     * Returns a stream that yields the results of calling the specified callback function on each element
     * of the stream. The function is called when the resulting stream elements are actually accessed, so
     * accessing the resulting stream multiple times means the function is also called multiple times for
     * each element of the stream.
     *
     * @param callbackfn Lazily evaluated function mapping stream elements.
     */
    map<U>(callbackfn: (value: T) => U): Stream<U>;
    /**
     * Returns the elements of the stream that meet the condition specified in a callback function.
     * The function is called when the resulting stream elements are actually accessed, so accessing the
     * resulting stream multiple times means the function is also called multiple times for each element
     * of the stream.
     *
     * @param predicate Lazily evaluated function checking a condition on stream elements.
     */
    filter<S extends T>(predicate: (value: T) => value is S): Stream<S>;
    filter(predicate: (value: T) => unknown): Stream<T>;
    /**
     * Returns the elements of the stream that are _non-nullable_, which means they are neither `undefined`
     * nor `null`.
     */
    nonNullable(): Stream<NonNullable<T>>;
    /**
     * Calls the specified callback function for all elements in the stream. The return value of the
     * callback function is the accumulated result, and is provided as an argument in the next call to
     * the callback function.
     *
     * @param callbackfn This method calls the function once for each element in the stream, providing
     *        the previous and current values of the reduction.
     * @param initialValue If specified, `initialValue` is used as the initial value to start the
     *        accumulation. The first call to the function provides this value as an argument instead
     *        of a stream value.
     */
    reduce(callbackfn: (previousValue: T, currentValue: T) => T): T | undefined;
    reduce<U = T>(callbackfn: (previousValue: U, currentValue: T) => U, initialValue: U): U;
    /**
     * Calls the specified callback function for all elements in the stream, in descending order.
     * The return value of the callback function is the accumulated result, and is provided as an
     * argument in the next call to the callback function.
     *
     * @param callbackfn This method calls the function once for each element in the stream, providing
     *        the previous and current values of the reduction.
     * @param initialValue If specified, `initialValue` is used as the initial value to start the
     *        accumulation. The first call to the function provides this value as an argument instead
     *        of an array value.
     */
    reduceRight(callbackfn: (previousValue: T, currentValue: T) => T): T | undefined;
    reduceRight<U = T>(callbackfn: (previousValue: U, currentValue: T) => U, initialValue: U): U;
    /**
     * Returns the value of the first element in the stream that meets the condition, or `undefined`
     * if there is no such element.
     *
     * @param predicate This method calls `predicate` once for each element of the stream, in ascending
     *        order, until it finds one where `predicate` returns a value which is coercible to the
     *        Boolean value `true`.
     */
    find<S extends T>(predicate: (value: T) => value is S): S | undefined;
    find(predicate: (value: T) => unknown): T | undefined;
    /**
     * Returns the index of the first element in the stream that meets the condition, or `-1`
     * if there is no such element.
     *
     * @param predicate This method calls `predicate` once for each element of the stream, in ascending
     *        order, until it finds one where `predicate` returns a value which is coercible to the
     *        Boolean value `true`.
     */
    findIndex(predicate: (value: T) => unknown): number;
    /**
     * Determines whether the stream includes a certain element, returning `true` or `false` as appropriate.
     *
     * @param searchElement The element to search for.
     */
    includes(searchElement: T): boolean;
    /**
     * Calls a defined callback function on each element of the stream and then flattens the result into
     * a new stream. This is identical to a `map` followed by `flat` with depth 1.
     *
     * @param callbackfn Lazily evaluated function mapping stream elements.
     */
    flatMap<U>(callbackfn: (value: T) => U | Iterable<U>): Stream<U>;
    /**
     * Returns a new stream with all sub-stream or sub-array elements concatenated into it recursively up
     * to the specified depth.
     *
     * @param depth The maximum recursion depth. Defaults to 1.
     */
    flat<D extends number = 1>(depth?: D): FlatStream<T, D>;
    /**
     * Returns the first element in the stream, or `undefined` if the stream is empty.
     */
    head(): T | undefined;
    /**
     * Returns a stream that skips the first `skipCount` elements from this stream.
     *
     * @param skipCount The number of elements to skip. If this is larger than the number of elements in
     *        the stream, an empty stream is returned. Defaults to 1.
     */
    tail(skipCount?: number): Stream<T>;
    /**
     * Returns a stream consisting of the elements of this stream, truncated to be no longer than `maxSize`
     * in length.
     *
     * @param maxSize The number of elements the stream should be limited to
     */
    limit(maxSize: number): Stream<T>;
    /**
     * Returns a stream containing only the distinct elements from this stream.
     * Equality is determined with the same rules as a standard `Set`.
     *
     * @param by A function returning the key used to check equality with a previous stream element.
     *        If omitted, the stream elements themselves are used for comparison.
     */
    distinct<Key = T>(by?: (element: T) => Key): Stream<T>;
    /**
     * Returns a stream that contains all elements that don't exist in the {@link other} iterable.
     * Equality is determined with the same rules as a standard `Set`.
     * @param other The elements that should be exluded from this stream.
     * @param key A function returning the key used to check quality.
     *        If omitted, the stream elements themselves are used for comparison.
     */
    exclude<Key = T>(other: Iterable<T>, key?: (element: T) => Key): Stream<T>;
}

/**
 * Create a stream from one or more iterables or array-likes.
 */
declare function stream<T>(...collections: Array<Iterable<T> | ArrayLike<T>>): Stream<T>;

/**
 * Create a stream of all AST nodes that are directly and indirectly contained in the given root node.
 * This does not include the root node itself.
 */
declare function streamAllContents(root: AstNode, options?: AstStreamOptions): TreeStream<AstNode>;

/**
 * Create a stream of all AST nodes that are directly and indirectly contained in the given root node,
 * including the root node itself.
 */
declare function streamAst(root: AstNode, options?: AstStreamOptions): TreeStream<AstNode>;

/**
 * Create a stream of all AST nodes that are directly contained in the given node. This includes
 * single-valued as well as multi-valued (array) properties.
 */
declare function streamContents(node: AstNode, options?: AstStreamOptions): Stream<AstNode>;

/**
 * Create a stream of all CST nodes that are directly and indirectly contained in the given root node,
 * including the root node itself.
 */
declare function streamCst(node: CstNode): TreeStream<CstNode>;

/**
 * The default implementation of `Stream` works with two input functions:
 *  - The first function creates the initial state of an iteration.
 *  - The second function gets the current state as argument and returns an `IteratorResult`.
 */
declare class StreamImpl<S, T> implements Stream<T> {
    protected readonly startFn: () => S;
    protected readonly nextFn: (state: S) => IteratorResult<T>;
    constructor(startFn: () => S, nextFn: (state: S) => IteratorResult<T, undefined>);
    iterator(): IterableIterator<T>;
    [Symbol.iterator](): Iterator<T>;
    isEmpty(): boolean;
    count(): number;
    toArray(): T[];
    toSet(): Set<T>;
    toMap<K = T, V = T>(keyFn?: (e: T) => K, valueFn?: (e: T) => V): Map<K, V>;
    toString(): string;
    concat<T2>(other: Iterable<T2>): Stream<T | T2>;
    join(separator?: string): string;
    indexOf(searchElement: T, fromIndex?: number): number;
    every<U extends T>(predicate: (value: T) => value is U): this is Stream<U> & this;
    every(predicate: (value: T) => unknown): boolean;
    some(predicate: (value: T) => unknown): boolean;
    forEach(callbackfn: (value: T, index: number) => void): void;
    map<U>(callbackfn: (value: T) => U): Stream<U>;
    filter<U extends T>(predicate: (value: T) => value is U): Stream<U> & this;
    filter(predicate: (value: T) => unknown): Stream<T> & this;
    nonNullable(): Stream<NonNullable<T>>;
    reduce(callbackfn: (previousValue: T, currentValue: T) => T): T | undefined;
    reduce<U = T>(callbackfn: (previousValue: U, currentValue: T) => U, initialValue: U): U;
    reduceRight(callbackfn: (previousValue: T, currentValue: T) => T): T | undefined;
    reduceRight<U = T>(callbackfn: (previousValue: U, currentValue: T) => U, initialValue: U): U;
    protected recursiveReduce<U>(iterator: Iterator<T>, callbackfn: (previousValue: U | T, currentValue: T) => U, initialValue?: U): U | T | undefined;
    find<S extends T>(predicate: (value: T) => value is S): S | undefined;
    find(predicate: (value: T) => unknown): T | undefined;
    findIndex(predicate: (value: T) => unknown): number;
    includes(searchElement: T): boolean;
    flatMap<U>(callbackfn: (value: T) => U | Iterable<U>): Stream<U>;
    flat<D extends number = 1>(depth?: D): FlatStream<T, D>;
    head(): T | undefined;
    tail(skipCount?: number): Stream<T>;
    limit(maxSize: number): Stream<T>;
    distinct<Key = T>(by?: (element: T) => Key): Stream<T>;
    exclude<Key = T>(other: Iterable<T>, key?: (element: T) => Key): Stream<T>;
}

/**
 * Create a stream of all cross-references that are held by the given AST node. This includes
 * single-valued as well as multi-valued (array) properties.
 */
declare function streamReferences(node: AstNode): Stream<ReferenceInfo>;

/**
 * The default scope implementation is based on a `Stream`. It has an optional _outer scope_ describing
 * the next level of elements, which are queried when a target element is not found in the stream provided
 * to this scope.
 */
declare class StreamScope implements Scope {
    readonly elements: Stream<AstNodeDescription>;
    readonly outerScope?: Scope;
    readonly caseInsensitive: boolean;
    readonly concatOuterScope: boolean;
    constructor(elements: Stream<AstNodeDescription>, outerScope?: Scope, options?: ScopeOptions);
    getAllElements(): Stream<AstNodeDescription>;
    getElement(name: string): AstNodeDescription | undefined;
    getElements(name: string): Stream<AstNodeDescription>;
}

declare interface StringLiteral extends langium.AstNode {
    readonly $container: ArrayLiteral | TypeAttribute;
    readonly $type: 'StringLiteral';
    value: string;
}

declare const StringLiteral: {
    readonly $type: "StringLiteral";
    readonly value: "value";
};

/**
 * A symbol kind.
 */
declare namespace SymbolKind {
    const File: 1;
    const Module: 2;
    const Namespace: 3;
    const Package: 4;
    const Class: 5;
    const Method: 6;
    const Property: 7;
    const Field: 8;
    const Constructor: 9;
    const Enum: 10;
    const Interface: 11;
    const Function: 12;
    const Variable: 13;
    const Constant: 14;
    const String: 15;
    const Number: 16;
    const Boolean: 17;
    const Array: 18;
    const Object: 19;
    const Key: 20;
    const Null: 21;
    const EnumMember: 22;
    const Struct: 23;
    const Event: 24;
    const Operator: 25;
    const TypeParameter: 26;
}

declare type SymbolKind = 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12 | 13 | 14 | 15 | 16 | 17 | 18 | 19 | 20 | 21 | 22 | 23 | 24 | 25 | 26;

/**
 * Symbol tags are extra annotations that tweak the rendering of a symbol.
 *
 * @since 3.16
 */
declare namespace SymbolTag {
    /**
     * Render a symbol as obsolete, usually using a strike-out.
     */
    const Deprecated: 1;
}

declare type SymbolTag = 1;

declare interface TerminalAlternatives extends TerminalElement {
    readonly $type: 'TerminalAlternatives';
    elements: Array<AbstractElement>;
}

declare const TerminalAlternatives: {
    readonly $type: "TerminalAlternatives";
    readonly cardinality: "cardinality";
    readonly elements: "elements";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
};

declare interface TerminalElement extends AbstractElement {
    readonly $type: 'CharacterRange' | 'NegatedToken' | 'RegexToken' | 'TerminalAlternatives' | 'TerminalElement' | 'TerminalGroup' | 'TerminalRuleCall' | 'UntilToken' | 'Wildcard';
    lookahead?: '?!' | '?<!' | '?<=' | '?=';
    parenthesized: boolean;
}

declare const TerminalElement: {
    readonly $type: "TerminalElement";
    readonly cardinality: "cardinality";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
};

declare interface TerminalGroup extends TerminalElement {
    readonly $type: 'TerminalGroup';
    elements: Array<AbstractElement>;
}

declare const TerminalGroup: {
    readonly $type: "TerminalGroup";
    readonly cardinality: "cardinality";
    readonly elements: "elements";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
};

declare function terminalRegex(terminalRule: GrammarAST.TerminalRule): RegExp;

declare interface TerminalRule extends langium.AstNode {
    readonly $container: Grammar;
    readonly $type: 'TerminalRule';
    definition: TerminalElement;
    fragment: boolean;
    hidden: boolean;
    name: string;
    type?: ReturnType_2;
}

declare const TerminalRule: {
    readonly $type: "TerminalRule";
    readonly definition: "definition";
    readonly fragment: "fragment";
    readonly hidden: "hidden";
    readonly name: "name";
    readonly type: "type";
};

declare interface TerminalRuleCall extends TerminalElement {
    readonly $type: 'TerminalRuleCall';
    rule: langium.Reference<TerminalRule>;
}

declare const TerminalRuleCall: {
    readonly $type: "TerminalRuleCall";
    readonly cardinality: "cardinality";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
    readonly rule: "rule";
};

/**
 * A simple text document. Not to be implemented. The document keeps the content
 * as string.
 */
declare interface TextDocument {
    /**
     * The associated URI for this document. Most documents have the __file__-scheme, indicating that they
     * represent files on disk. However, some documents may have other schemes indicating that they are not
     * available on disk.
     *
     * @readonly
     */
    readonly uri: DocumentUri;
    /**
     * The identifier of the language associated with this document.
     *
     * @readonly
     */
    readonly languageId: string;
    /**
     * The version number of this document (it will increase after each
     * change, including undo/redo).
     *
     * @readonly
     */
    readonly version: number;
    /**
     * Get the text of this document. A substring can be retrieved by
     * providing a range.
     *
     * @param range (optional) An range within the document to return.
     * If no range is passed, the full content is returned.
     * Invalid range positions are adjusted as described in {@link Position.line}
     * and {@link Position.character}.
     * If the start range position is greater than the end range position,
     * then the effect of getText is as if the two positions were swapped.

     * @return The text of this document or a substring of the text if a
     *         range is provided.
     */
    getText(range?: Range_3): string;
    /**
     * Converts a zero-based offset to a position.
     *
     * @param offset A zero-based offset.
     * @return A valid {@link Position position}.
     * @example The text document "ab\ncd" produces:
     * * position { line: 0, character: 0 } for `offset` 0.
     * * position { line: 0, character: 1 } for `offset` 1.
     * * position { line: 0, character: 2 } for `offset` 2.
     * * position { line: 1, character: 0 } for `offset` 3.
     * * position { line: 1, character: 1 } for `offset` 4.
     */
    positionAt(offset: number): Position_2;
    /**
     * Converts the position to a zero-based offset.
     * Invalid positions are adjusted as described in {@link Position.line}
     * and {@link Position.character}.
     *
     * @param position A position.
     * @return A valid zero-based offset.
     */
    offsetAt(position: Position_2): number;
    /**
     * The number of lines in this document.
     *
     * @readonly
     */
    readonly lineCount: number;
}

declare namespace TextDocument {
    /**
     * Creates a new text document.
     *
     * @param uri The document's uri.
     * @param languageId  The document's language Id.
     * @param version The document's initial version number.
     * @param content The document's content.
     */
    function create(uri: DocumentUri, languageId: string, version: number, content: string): TextDocument;
    /**
     * Updates a TextDocument by modifying its content.
     *
     * @param document the document to update. Only documents created by TextDocument.create are valid inputs.
     * @param changes the changes to apply to the document.
     * @param version the changes version for the document.
     * @returns The updated TextDocument. Note: That's the same document instance passed in as first parameter.
     *
     */
    function update(document: TextDocument, changes: TextDocumentContentChangeEvent[], version: number): TextDocument;
    function applyEdits(document: TextDocument, edits: TextEdit[]): string;
}

/**
 * Text document specific client capabilities.
 */
declare interface TextDocumentClientCapabilities {
    /**
     * Defines which synchronization capabilities the client supports.
     */
    synchronization?: TextDocumentSyncClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/completion` request.
     */
    completion?: CompletionClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/hover` request.
     */
    hover?: HoverClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/signatureHelp` request.
     */
    signatureHelp?: SignatureHelpClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/declaration` request.
     *
     * @since 3.14.0
     */
    declaration?: DeclarationClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/definition` request.
     */
    definition?: DefinitionClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/typeDefinition` request.
     *
     * @since 3.6.0
     */
    typeDefinition?: TypeDefinitionClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/implementation` request.
     *
     * @since 3.6.0
     */
    implementation?: ImplementationClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/references` request.
     */
    references?: ReferenceClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/documentHighlight` request.
     */
    documentHighlight?: DocumentHighlightClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/documentSymbol` request.
     */
    documentSymbol?: DocumentSymbolClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/codeAction` request.
     */
    codeAction?: CodeActionClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/codeLens` request.
     */
    codeLens?: CodeLensClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/documentLink` request.
     */
    documentLink?: DocumentLinkClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/documentColor` and the
     * `textDocument/colorPresentation` request.
     *
     * @since 3.6.0
     */
    colorProvider?: DocumentColorClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/formatting` request.
     */
    formatting?: DocumentFormattingClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/rangeFormatting` request.
     */
    rangeFormatting?: DocumentRangeFormattingClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/onTypeFormatting` request.
     */
    onTypeFormatting?: DocumentOnTypeFormattingClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/rename` request.
     */
    rename?: RenameClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/foldingRange` request.
     *
     * @since 3.10.0
     */
    foldingRange?: FoldingRangeClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/selectionRange` request.
     *
     * @since 3.15.0
     */
    selectionRange?: SelectionRangeClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/publishDiagnostics` notification.
     */
    publishDiagnostics?: PublishDiagnosticsClientCapabilities;
    /**
     * Capabilities specific to the various call hierarchy requests.
     *
     * @since 3.16.0
     */
    callHierarchy?: CallHierarchyClientCapabilities;
    /**
     * Capabilities specific to the various semantic token request.
     *
     * @since 3.16.0
     */
    semanticTokens?: SemanticTokensClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/linkedEditingRange` request.
     *
     * @since 3.16.0
     */
    linkedEditingRange?: LinkedEditingRangeClientCapabilities;
    /**
     * Client capabilities specific to the `textDocument/moniker` request.
     *
     * @since 3.16.0
     */
    moniker?: MonikerClientCapabilities;
    /**
     * Capabilities specific to the various type hierarchy requests.
     *
     * @since 3.17.0
     */
    typeHierarchy?: TypeHierarchyClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/inlineValue` request.
     *
     * @since 3.17.0
     */
    inlineValue?: InlineValueClientCapabilities;
    /**
     * Capabilities specific to the `textDocument/inlayHint` request.
     *
     * @since 3.17.0
     */
    inlayHint?: InlayHintClientCapabilities;
    /**
     * Capabilities specific to the diagnostic pull model.
     *
     * @since 3.17.0
     */
    diagnostic?: DiagnosticClientCapabilities;
    /**
     * Client capabilities specific to inline completions.
     *
     * @since 3.18.0
     * @proposed
     */
    inlineCompletion?: InlineCompletionClientCapabilities;
}

/**
 * An event describing a change to a text document. If range and rangeLength are omitted
 * the new text is considered to be the full content of the document.
 */
declare type TextDocumentContentChangeEvent = {
    /**
     * The range of the document that changed.
     */
    range: Range_3;
    /**
     * The optional length of the range that got replaced.
     *
     * @deprecated use range instead.
     */
    rangeLength?: number;
    /**
     * The new text for the provided range.
     */
    text: string;
} | {
    /**
     * The new text of the whole document.
     */
    text: string;
};

/**
 * Surrogate definition of the `TextDocuments` interface from the `vscode-languageserver` package.
 * No implementation object is expected to be offered by `LangiumCoreServices`, but only by `LangiumLSPServices`.
 */
declare type TextDocumentProvider = {
    get(uri: string | URI): TextDocument | undefined;
};

declare interface TextDocumentSyncClientCapabilities {
    /**
     * Whether text document synchronization supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * The client supports sending will save notifications.
     */
    willSave?: boolean;
    /**
     * The client supports sending a will save request and
     * waits for a response providing text edits which will
     * be applied to the document before it is saved.
     */
    willSaveWaitUntil?: boolean;
    /**
     * The client supports did save notifications.
     */
    didSave?: boolean;
}

/**
 * A text edit applicable to a text document.
 */
declare interface TextEdit {
    /**
     * The range of the text document to be manipulated. To insert
     * text into a document create a range where start === end.
     */
    range: Range_3;
    /**
     * The string to be inserted. For delete operations use an
     * empty string.
     */
    newText: string;
}

declare function toDiagnosticData(severity: LexingDiagnosticSeverity): DiagnosticData;

/**
 * Transforms the diagnostic severity from the {@link LexingDiagnosticSeverity} format to LSP's `DiagnosticSeverity` format.
 *
 * @param severity The lexing diagnostic severity
 * @returns Diagnostic severity according to `vscode-languageserver-types/lib/esm/main.js#DiagnosticSeverity`
 */
declare function toDiagnosticSeverity(severity: LexingDiagnosticSeverity): DiagnosticSeverity;

declare function toDocumentSegment(node: CstNode): DocumentSegment;

declare function toDocumentSegment(node?: CstNode): DocumentSegment | undefined;

declare interface TokenBuilder {
    buildTokens(grammar: Grammar, options?: TokenBuilderOptions): TokenVocabulary;
    /**
     * Produces a lexing report for the given text that was just tokenized using the tokens provided by this builder.
     *
     * @param text The text that was tokenized.
     */
    flushLexingReport?(text: string): LexingReport;
}

declare interface TokenBuilderOptions {
    caseInsensitive?: boolean;
}

declare namespace TokenFormat {
    const Relative: 'relative';
}

declare type TokenFormat = 'relative';

declare type TokenizeMode = 'full' | 'partial';

declare interface TokenizeOptions {
    mode?: TokenizeMode;
}

declare function tokenToRange(token: IToken): Range_2;

declare namespace TraceValues {
    /**
     * Turn tracing off.
     */
    const Off: 'off';
    /**
     * Trace messages only.
     */
    const Messages: 'messages';
    /**
     * Compact message tracing.
     */
    const Compact: 'compact';
    /**
     * Verbose message tracing.
     */
    const Verbose: 'verbose';
}

declare type TraceValues = 'off' | 'messages' | 'compact' | 'verbose';

export declare interface Transition extends langium_2.AstNode {
    readonly $container: Cynefin;
    readonly $type: 'EOL' | 'Transition';
    from: string;
    label: string;
    to: string;
}

export declare const Transition: {
    readonly $type: "Transition";
    readonly from: "from";
    readonly label: "label";
    readonly to: "to";
};

/**
 * A tree iterator adds the ability to prune the current iteration.
 */
declare interface TreeIterator<T> extends IterableIterator<T> {
    /**
     * Skip the whole subtree below the last returned element. The iteration continues as if that
     * element had no children.
     */
    prune(): void;
}

export declare interface Treemap extends langium_2.AstNode {
    readonly $type: 'Treemap';
    accDescr?: string;
    accTitle?: string;
    title?: string;
    TreemapRows: Array<TreemapRow>;
}

export declare const Treemap: {
    readonly $type: "Treemap";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly title: "title";
    readonly TreemapRows: "TreemapRows";
};

/**
 * Declaration of `Treemap` services.
 */
declare interface TreemapAddedServices {
    parser: {
        TokenBuilder: TreemapTokenBuilder;
        ValueConverter: TreemapValueConverter;
    };
    validation: {
        TreemapValidator: TreemapValidator;
    };
}

export declare const TreemapGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'TreemapGrammar' language. */
declare namespace TreemapGrammar {
    const Terminals: {
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        TREEMAP_KEYWORD: RegExp;
        CLASS_DEF: RegExp;
        STYLE_SEPARATOR: RegExp;
        SEPARATOR: RegExp;
        COMMA: RegExp;
        INDENTATION: RegExp;
        WS: RegExp;
        ML_COMMENT: RegExp;
        NL: RegExp;
        ID2: RegExp;
        NUMBER2: RegExp;
        STRING2: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = never;
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        ClassDefStatement: ClassDefStatement;
        Item: Item;
        Leaf: Leaf;
        Section: Section;
        Treemap: Treemap;
        TreemapRow: TreemapRow;
    };
}

/**
 * Dependency injection module that overrides Langium default services and
 * contributes the declared `Treemap` services.
 */
export declare const TreemapModule: Module<TreemapServices, PartialLangiumCoreServices & TreemapAddedServices>;

declare interface TreemapRow extends langium_2.AstNode {
    readonly $container: Treemap;
    readonly $type: 'ClassDef' | 'TreemapRow';
    indent?: string;
    item: Item;
}

declare const TreemapRow: {
    readonly $type: "TreemapRow";
    readonly indent: "indent";
    readonly item: "item";
};

/**
 * Union of Langium default services and `Treemap` services.
 */
export declare type TreemapServices = LangiumCoreServices & TreemapAddedServices;

declare class TreemapTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

/**
 * Implementation of custom validations.
 */
declare class TreemapValidator {
    /**
     * Validates that a treemap has only one root node.
     * A root node is defined as a node that has no indentation.
     */
    checkSingleRoot(doc: Treemap, accept: ValidationAcceptor): void;
}

declare class TreemapValueConverter extends AbstractMermaidValueConverter {
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

export declare interface TreeNode extends langium_2.AstNode {
    readonly $container: TreeView;
    readonly $type: 'TreeNode';
    classAnnotation?: string;
    descAnnotation?: string;
    iconAnnotation?: string;
    indent?: string;
    name: string;
}

export declare const TreeNode: {
    readonly $type: "TreeNode";
    readonly classAnnotation: "classAnnotation";
    readonly descAnnotation: "descAnnotation";
    readonly iconAnnotation: "iconAnnotation";
    readonly indent: "indent";
    readonly name: "name";
};

/**
 * A tree stream is used to stream the elements of a tree, for example an AST or CST.
 */
declare interface TreeStream<T> extends Stream<T> {
    iterator(): TreeIterator<T>;
}

/**
 * The default implementation of `TreeStream` takes a root element and a function that computes the
 * children of its argument. Whether the root node included in the stream is controlled with the
 * `includeRoot` option, which defaults to `false`.
 */
declare class TreeStreamImpl<T> extends StreamImpl<{
    iterators: Array<Iterator<T>>;
    pruned: boolean;
}, T> implements TreeStream<T> {
    constructor(root: T, children: (node: T) => Iterable<T>, options?: {
        includeRoot?: boolean;
    });
    iterator(): TreeIterator<T>;
}

export declare interface TreeView extends langium_2.AstNode {
    readonly $type: 'TreeView';
    accDescr?: string;
    accTitle?: string;
    nodes: Array<TreeNode>;
    title?: string;
}

export declare const TreeView: {
    readonly $type: "TreeView";
    readonly accDescr: "accDescr";
    readonly accTitle: "accTitle";
    readonly nodes: "nodes";
    readonly title: "title";
};

/**
 * Declaration of `TreeView` services.
 */
declare interface TreeViewAddedServices {
    parser: {
        TokenBuilder: TreeViewTokenBuilder;
        ValueConverter: TreeViewValueConverter;
    };
}

export declare const TreeViewGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'TreeViewGrammar' language. */
declare namespace TreeViewGrammar {
    const Terminals: {
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        CLASS_ANNOTATION: RegExp;
        ICON_ANNOTATION: RegExp;
        DESC_ANNOTATION: RegExp;
        INDENTATION: RegExp;
        QUOTED_NAME: RegExp;
        WS: RegExp;
        ML_COMMENT: RegExp;
        NL: RegExp;
        BARE_NAME: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "treeView-beta";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        TreeNode: TreeNode;
        TreeView: TreeView;
    };
}

/**
 * Dependency injection module that overrides Langium default services and
 * contributes the declared `TreeView` services.
 */
export declare const TreeViewModule: Module<TreeViewServices, PartialLangiumCoreServices & TreeViewAddedServices>;

/**
 * Union of Langium default services and `TreeView` services.
 */
export declare type TreeViewServices = LangiumCoreServices & TreeViewAddedServices;

declare class TreeViewTokenBuilder extends AbstractMermaidTokenBuilder {
    constructor();
}

declare class TreeViewValueConverter extends AbstractMermaidValueConverter {
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

declare interface Type extends langium.AstNode {
    readonly $container: Grammar;
    readonly $type: 'Type';
    name: string;
    type: TypeDefinition;
}

declare const Type: {
    readonly $type: "Type";
    readonly name: "name";
    readonly type: "type";
};

declare interface TypeAttribute extends langium.AstNode {
    readonly $container: Interface;
    readonly $type: 'TypeAttribute';
    defaultValue?: ValueLiteral;
    isOptional: boolean;
    name: FeatureName;
    type: TypeDefinition;
}

declare const TypeAttribute: {
    readonly $type: "TypeAttribute";
    readonly defaultValue: "defaultValue";
    readonly isOptional: "isOptional";
    readonly name: "name";
    readonly type: "type";
};

declare type TypeDefinition = ArrayType | ReferenceType | SimpleType | UnionType;

declare const TypeDefinition: {
    readonly $type: "TypeDefinition";
};

/**
 * Since 3.6.0
 */
declare interface TypeDefinitionClientCapabilities {
    /**
     * Whether implementation supports dynamic registration. If this is set to `true`
     * the client supports the new `TypeDefinitionRegistrationOptions` return value
     * for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
    /**
     * The client supports additional metadata in the form of definition links.
     *
     * Since 3.14.0
     */
    linkSupport?: boolean;
}

/**
 * @since 3.17.0
 */
declare type TypeHierarchyClientCapabilities = {
    /**
     * Whether implementation supports dynamic registration. If this is set to `true`
     * the client supports the new `(TextDocumentRegistrationOptions & StaticRegistrationOptions)`
     * return value for the corresponding server capability as well.
     */
    dynamicRegistration?: boolean;
};

/**
 * Represents runtime meta data about an AST node type.
 */
declare interface TypeMetaData {
    /** The name of this AST node type. Corresponds to the `AstNode.$type` value. */
    name: string;
    /** A list of properties with their relevant meta data. */
    properties: {
        [name: string]: PropertyMetaData;
    };
    /** The super types of this AST node type. */
    superTypes: string[];
}

/**
 * Defines an unsigned integer in the range of 0 to 2^31 - 1.
 */
declare type uinteger = number;

declare namespace uinteger {
    const MIN_VALUE = 0;
    const MAX_VALUE = 2147483647;
    function is(value: any): value is uinteger;
}

/**
 * Defines an unsigned integer in the range of 0 to 2^31 - 1.
 */
declare type uinteger_2 = number;

declare namespace uinteger_2 {
    const MIN_VALUE = 0;
    const MAX_VALUE = 2147483647;
    function is(value: any): value is uinteger_2;
}

declare interface UnionType extends langium.AstNode {
    readonly $container: ArrayType | ReferenceType | Type | TypeAttribute | UnionType;
    readonly $type: 'UnionType';
    types: Array<TypeDefinition>;
}

declare const UnionType: {
    readonly $type: "UnionType";
    readonly types: "types";
};

declare interface UnorderedGroup extends AbstractElement {
    readonly $type: 'UnorderedGroup';
    elements: Array<AbstractElement>;
}

declare const UnorderedGroup: {
    readonly $type: "UnorderedGroup";
    readonly cardinality: "cardinality";
    readonly elements: "elements";
};

declare interface UntilToken extends TerminalElement {
    readonly $type: 'UntilToken';
    terminal: AbstractElement;
}

declare const UntilToken: {
    readonly $type: "UntilToken";
    readonly cardinality: "cardinality";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
    readonly terminal: "terminal";
};

/**
 * Uniform Resource Identifier (URI) http://tools.ietf.org/html/rfc3986.
 * This class is a simple parser which creates the basic component parts
 * (http://tools.ietf.org/html/rfc3986#section-3) with minimal validation
 * and encoding.
 *
 * ```txt
 *       foo://example.com:8042/over/there?name=ferret#nose
 *       \_/   \______________/\_________/ \_________/ \__/
 *        |           |            |            |        |
 *     scheme     authority       path        query   fragment
 *        |   _____________________|__
 *       / \ /                        \
 *       urn:example:animal:ferret:nose
 * ```
 */
declare class URI implements UriComponents {
    static isUri(thing: any): thing is URI;
    /**
     * scheme is the 'http' part of 'http://www.example.com/some/path?query#fragment'.
     * The part before the first colon.
     */
    readonly scheme: string;
    /**
     * authority is the 'www.example.com' part of 'http://www.example.com/some/path?query#fragment'.
     * The part between the first double slashes and the next slash.
     */
    readonly authority: string;
    /**
     * path is the '/some/path' part of 'http://www.example.com/some/path?query#fragment'.
     */
    readonly path: string;
    /**
     * query is the 'query' part of 'http://www.example.com/some/path?query#fragment'.
     */
    readonly query: string;
    /**
     * fragment is the 'fragment' part of 'http://www.example.com/some/path?query#fragment'.
     */
    readonly fragment: string;
    /**
     * @internal
     */
    protected constructor(scheme: string, authority?: string, path?: string, query?: string, fragment?: string, _strict?: boolean);
    /**
     * @internal
     */
    protected constructor(components: UriComponents);
    /**
     * Returns a string representing the corresponding file system path of this URI.
     * Will handle UNC paths, normalizes windows drive letters to lower-case, and uses the
     * platform specific path separator.
     *
     * * Will *not* validate the path for invalid characters and semantics.
     * * Will *not* look at the scheme of this URI.
     * * The result shall *not* be used for display purposes but for accessing a file on disk.
     *
     *
     * The *difference* to `URI#path` is the use of the platform specific separator and the handling
     * of UNC paths. See the below sample of a file-uri with an authority (UNC path).
     *
     * ```ts
     const u = URI.parse('file://server/c$/folder/file.txt')
     u.authority === 'server'
     u.path === '/shares/c$/file.txt'
     u.fsPath === '\\server\c$\folder\file.txt'
     ```
     *
     * Using `URI#path` to read a file (using fs-apis) would not be enough because parts of the path,
     * namely the server name, would be missing. Therefore `URI#fsPath` exists - it's sugar to ease working
     * with URIs that represent files on disk (`file` scheme).
     */
    get fsPath(): string;
    with(change: {
        scheme?: string;
        authority?: string | null;
        path?: string | null;
        query?: string | null;
        fragment?: string | null;
    }): URI;
    /**
     * Creates a new URI from a string, e.g. `http://www.example.com/some/path`,
     * `file:///usr/home`, or `scheme:with/path`.
     *
     * @param value A string which represents an URI (see `URI#toString`).
     */
    static parse(value: string, _strict?: boolean): URI;
    /**
     * Creates a new URI from a file system path, e.g. `c:\my\files`,
     * `/usr/home`, or `\\server\share\some\path`.
     *
     * The *difference* between `URI#parse` and `URI#file` is that the latter treats the argument
     * as path, not as stringified-uri. E.g. `URI.file(path)` is **not the same as**
     * `URI.parse('file://' + path)` because the path might contain characters that are
     * interpreted (# and ?). See the following sample:
     * ```ts
     const good = URI.file('/coding/c#/project1');
     good.scheme === 'file';
     good.path === '/coding/c#/project1';
     good.fragment === '';
     const bad = URI.parse('file://' + '/coding/c#/project1');
     bad.scheme === 'file';
     bad.path === '/coding/c'; // path is now broken
     bad.fragment === '/project1';
     ```
     *
     * @param path A file system path (see `URI#fsPath`)
     */
    static file(path: string): URI;
    static from(components: {
        scheme: string;
        authority?: string;
        path?: string;
        query?: string;
        fragment?: string;
    }): URI;
    /**
     * Creates a string representation for this URI. It's guaranteed that calling
     * `URI.parse` with the result of this function creates an URI which is equal
     * to this URI.
     *
     * * The result shall *not* be used for display purposes but for externalization or transport.
     * * The result will be encoded using the percentage encoding and encoding happens mostly
     * ignore the scheme-specific encoding rules.
     *
     * @param skipEncoding Do not encode the result, default is `false`
     */
    toString(skipEncoding?: boolean): string;
    toJSON(): UriComponents;
    static revive(data: UriComponents | URI): URI;
    static revive(data: UriComponents | URI | undefined): URI | undefined;
    static revive(data: UriComponents | URI | null): URI | null;
    static revive(data: UriComponents | URI | undefined | null): URI | undefined | null;
}

/**
 * A tagging type for string properties that are actually URIs
 *
 * @since 3.16.0
 */
declare type URI_2 = string;

declare namespace URI_2 {
    function is(value: any): value is URI_2;
}

/**
 * A tagging type for string properties that are actually URIs
 *
 * @since 3.16.0
 */
declare type URI_3 = string;

declare namespace URI_3 {
    function is(value: any): value is URI_3;
}

declare interface UriComponents {
    scheme: string;
    authority: string;
    path: string;
    query: string;
    fragment: string;
}

/**
 * A trie structure for URIs. It allows to insert, delete and find elements by their URI.
 * More specifically, it allows to efficiently find all elements that are children of a given URI.
 *
 * Unlike a regular trie, this implementation uses the name of the URI segments as keys.
 *
 * @see {@link https://en.wikipedia.org/wiki/Trie}
 */
declare class UriTrie<T> {
    protected readonly root: InternalUriTrieNode<T>;
    protected normalizeUri(uri: URI | string): string;
    clear(): void;
    insert(uri: URI | string, element: T): void;
    delete(uri: URI | string): void;
    has(uri: URI | string): boolean;
    hasNode(uri: URI | string): boolean;
    find(uri: URI | string): T | undefined;
    findNode(uri: URI | string): UriTrieNode<T> | undefined;
    findChildren(uri: URI | string): Array<UriTrieNode<T>>;
    all(): T[];
    findAll(prefix: URI | string): T[];
    protected getNode(uri: string, create: true): InternalUriTrieNode<T>;
    protected getNode(uri: string, create: false): InternalUriTrieNode<T> | undefined;
    protected collectValues(node: InternalUriTrieNode<T>): T[];
}

declare interface UriTrieNode<T> {
    name: string;
    uri: string;
    element?: T;
}

declare namespace UriUtils {
    const basename: typeof Utils.basename;
    const dirname: typeof Utils.dirname;
    const extname: typeof Utils.extname;
    const joinPath: typeof Utils.joinPath;
    const resolvePath: typeof Utils.resolvePath;
    function equals(a?: URI | string, b?: URI | string): boolean;
    function relative(from: URI | string, to: URI | string): string;
    function normalize(uri: URI | string): string;
    function contains(parent: URI | string, child: URI | string): boolean;
}

declare namespace Utils {
    /**
     * Joins one or more input paths to the path of URI.
     * '/' is used as the directory separation character.
     *
     * The resolved path will be normalized. That means:
     *  - all '..' and '.' segments are resolved.
     *  - multiple, sequential occurences of '/' are replaced by a single instance of '/'.
     *  - trailing separators are preserved.
     *
     * @param uri The input URI.
     * @param paths The paths to be joined with the path of URI.
     * @returns A URI with the joined path. All other properties of the URI (scheme, authority, query, fragments, ...) will be taken from the input URI.
     */
    function joinPath(uri: URI, ...paths: string[]): URI;
    /**
     * Resolves one or more paths against the path of a URI.
     * '/' is used as the directory separation character.
     *
     * The resolved path will be normalized. That means:
     *  - all '..' and '.' segments are resolved.
     *  - multiple, sequential occurences of '/' are replaced by a single instance of '/'.
     *  - trailing separators are removed.
     *
     * @param uri The input URI.
     * @param paths The paths to resolve against the path of URI.
     * @returns A URI with the resolved path. All other properties of the URI (scheme, authority, query, fragments, ...) will be taken from the input URI.
     */
    function resolvePath(uri: URI, ...paths: string[]): URI;
    /**
     * Returns a URI where the path is the directory name of the input uri, similar to the Unix dirname command.
     * In the path, '/' is recognized as the directory separation character. Trailing directory separators are ignored.
     * The orignal URI is returned if the URIs path is empty or does not contain any path segments.
     *
     * @param uri The input URI.
     * @return The last segment of the URIs path.
     */
    function dirname(uri: URI): URI;
    /**
     * Returns the last segment of the path of a URI, similar to the Unix basename command.
     * In the path, '/' is recognized as the directory separation character. Trailing directory separators are ignored.
     * The empty string is returned if the URIs path is empty or does not contain any path segments.
     *
     * @param uri The input URI.
     * @return The base name of the URIs path.
     */
    function basename(uri: URI): string;
    /**
     * Returns the extension name of the path of a URI, similar to the Unix extname command.
     * In the path, '/' is recognized as the directory separation character. Trailing directory separators are ignored.
     * The empty string is returned if the URIs path is empty or does not contain any path segments.
     *
     * @param uri The input URI.
     * @return The extension name of the URIs path.
     */
    function extname(uri: URI): string;
}

declare const VALIDATE_EACH_NODE: ValidateSingleNodeOptions;

declare interface ValidateSingleNodeOptions {
    validateNode: boolean;
    validateChildren: boolean;
}

declare type ValidationAcceptor = <N extends AstNode>(severity: ValidationSeverity, message: string, info: DiagnosticInfo<N>) => void;

/**
 * There are 3 pre-defined categories: `fast`, `slow` and `built-in`.
 *
 * `fast` checks can be executed after every document change (i.e. as the user is typing). If a check
 * is too slow it can delay the response to document changes, yielding bad user experience. By marking
 * it as `slow`, it will be skipped for normal as-you-type validation. Then it's up to you when to
 * schedule these long-running checks: after the fast checks are done, or after saving a document,
 * or with an explicit command, etc.
 *
 * `built-in` checks are errors produced by the lexer, the parser, or the linker. They cannot be used
 * for custom validation checks.
 *
 * You can also provide user-defined categories. These check will be skipped by default. Then it's up
 * to you to schedule these checks: after the fast checks are done, or after saving a document,
 * or with an explicit command, etc.
 */
declare type ValidationCategory = 'fast' | 'slow' | 'built-in' | (string & {});

declare namespace ValidationCategory {
    const defaults: readonly ValidationCategory[];
    /**
     * @deprecated since 4.2 Use `ValidationCategory.defaults` instead,
     * since "all" does not include user-defined, custom validation categories.
     */
    const all: readonly ValidationCategory[];
}

declare type ValidationCheck<T extends AstNode = AstNode> = (node: T, accept: ValidationAcceptor, cancelToken: CancellationToken) => MaybePromise<void>;

declare type ValidationCheckEntry = {
    check: ValidationCheck;
    category: ValidationCategory;
};

/**
 * A utility type for associating non-primitive AST types to corresponding validation checks. For example:
 *
 * ```ts
 *   const checks: ValidationChecks<StatemachineAstType> = {
 *       State: validator.checkStateNameStartsWithCapital
 *    };
 * ```
 *
 * If an AST type does not extend AstNode, e.g. if it describes a union of string literals, that type's name must not occur as a key in objects of type `ValidationCheck<...>`.
 *
 * @param T a type definition mapping language specific type names (keys) to the corresponding types (values)
 */
declare type ValidationChecks<T> = {
    [K in keyof T]?: T[K] extends AstNode ? ValidationCheck<T[K]> | Array<ValidationCheck<T[K]>> : never;
} & {
    AstNode?: ValidationCheck<AstNode> | Array<ValidationCheck<AstNode>>;
};

declare interface ValidationOptions {
    /**
     * If this is set, only the checks associated with these categories are executed; otherwise
     * all checks are executed. The default category if not specified to the registry is `'fast'`.
     */
    categories?: ValidationCategory[];
    /** If true, no further diagnostics are reported if there are lexing errors. */
    stopAfterLexingErrors?: boolean;
    /** If true, no further diagnostics are reported if there are parsing errors. Lexing errors are reported first. */
    stopAfterParsingErrors?: boolean;
    /** If true, no further diagnostics are reported if there are linking errors. Lexing and parsing errors are reported first. */
    stopAfterLinkingErrors?: boolean;
}

/**
 * A utility type for describing functions which will be called once before or after all the AstNodes of an AST/Langium document are validated.
 *
 * The AST is represented by its root AstNode.
 *
 * The given validation acceptor helps to report some early or lately detected issues.
 *
 * The 'categories' indicate, which validation categories are executed for all the AstNodes.
 * This helps to tailor the preparations/tear-down logic to the actually executed checks on the nodes.
 *
 * It is recommended to support interrupts during long-running logic with 'interruptAndCheck(cancelToken)'.
 */
declare type ValidationPreparation = (rootNode: AstNode, accept: ValidationAcceptor, categories: ValidationCategory[], cancelToken: CancellationToken) => MaybePromise<void>;

/**
 * Manages a set of `ValidationCheck`s to be applied when documents are validated.
 */
declare class ValidationRegistry {
    protected readonly entries: MultiMap<string, ValidationCheckEntry>;
    protected readonly knownCategories: Set<ValidationCategory>;
    protected readonly reflection: AstReflection;
    protected entriesBefore: ValidationPreparation[];
    protected entriesAfter: ValidationPreparation[];
    constructor(services: LangiumCoreServices);
    /**
     * Register a set of validation checks. Each value in the record can be either a single validation check (i.e. a function)
     * or an array of validation checks.
     *
     * @param checksRecord Set of validation checks to register.
     * @param thisObj Optional object to be used as `this` when calling the validation check functions.
     * @param category Optional category for the validation checks (defaults to `'fast'`).
     */
    register<T>(checksRecord: ValidationChecks<T>, thisObj?: ThisParameterType<unknown>, category?: ValidationCategory): void;
    protected wrapValidationException(check: ValidationCheck, thisObj: unknown): ValidationCheck;
    protected handleException(functionality: () => MaybePromise<void>, messageContext: string, accept: ValidationAcceptor, node: AstNode): Promise<void>;
    protected addEntry(type: string, entry: ValidationCheckEntry): void;
    getChecks(type: string, categories?: ValidationCategory[]): Stream<ValidationCheck>;
    /**
     * Register logic which will be executed once before validating all the nodes of an AST/Langium document.
     * This helps to prepare or initialize some information which are required or reusable for the following checks on the AstNodes.
     *
     * As an example, for validating unique fully-qualified names of nodes in the AST,
     * here the map for mapping names to nodes could be established.
     * During the usual checks on the nodes, they are put into this map with their name.
     *
     * Note that this approach makes validations stateful, which is relevant e.g. when cancelling the validation.
     * Therefore it is recommended to clear stored information
     * _before_ validating an AST to validate each AST unaffected from other ASTs
     * AND _after_ validating the AST to free memory by information which are no longer used.
     *
     * @param checkBefore a set-up function which will be called once before actually validating an AST
     * @param thisObj Optional object to be used as `this` when calling the validation check functions.
     */
    registerBeforeDocument(checkBefore: ValidationPreparation, thisObj?: ThisParameterType<unknown>): void;
    /**
     * Register logic which will be executed once after validating all the nodes of an AST/Langium document.
     * This helps to finally evaluate information which are collected during the checks on the AstNodes.
     *
     * As an example, for validating unique fully-qualified names of nodes in the AST,
     * here the map with all the collected nodes and their names is checked
     * and validation hints are created for all nodes with the same name.
     *
     * Note that this approach makes validations stateful, which is relevant e.g. when cancelling the validation.
     * Therefore it is recommended to clear stored information
     * _before_ validating an AST to validate each AST unaffected from other ASTs
     * AND _after_ validating the AST to free memory by information which are no longer used.
     *
     * @param checkBefore a set-up function which will be called once before actually validating an AST
     * @param thisObj Optional object to be used as `this` when calling the validation check functions.
     */
    registerAfterDocument(checkAfter: ValidationPreparation, thisObj?: ThisParameterType<unknown>): void;
    protected wrapPreparationException(check: ValidationPreparation, messageContext: string, thisObj: unknown): ValidationPreparation;
    get checksBefore(): ValidationPreparation[];
    get checksAfter(): ValidationPreparation[];
    getAllValidationCategories(_document: LangiumDocument): ReadonlySet<ValidationCategory>;
}

declare type ValidationSeverity = 'error' | 'warning' | 'info' | 'hint';

/**
 * Language-specific service for converting string values from the source text format into a value to be held in the AST.
 */
declare interface ValueConverter {
    /**
     * Converts a string value from the source text format into a value to be held in the AST.
     */
    convert(input: string, cstNode: CstNode): ValueType;
}

declare namespace ValueConverter {
    function convertString(input: string): string;
    function convertID(input: string): string;
    function convertInt(input: string): number;
    function convertBigint(input: string): bigint;
    function convertDate(input: string): Date;
    function convertNumber(input: string): number;
    function convertBoolean(input: string): boolean;
}

declare type ValueLiteral = ArrayLiteral | BooleanLiteral | NumberLiteral | StringLiteral;

declare const ValueLiteral: {
    readonly $type: "ValueLiteral";
};

declare type ValueType = string | number | boolean | bigint | Date;

export declare interface Wardley extends langium_2.AstNode {
    readonly $type: 'Wardley';
    accDescr?: string;
    accelerators: Array<Accelerator>;
    accTitle?: string;
    anchors: Array<Anchor>;
    annotation: Array<Annotation>;
    annotations: Array<Annotations>;
    components: Array<Component>;
    deaccelerators: Array<Deaccelerator>;
    evolution?: Evolution;
    evolves: Array<Evolve>;
    links: Array<Link>;
    notes: Array<Note>;
    pipelines: Array<Pipeline>;
    size?: Size;
    title?: string;
}

export declare const Wardley: {
    readonly $type: "Wardley";
    readonly accDescr: "accDescr";
    readonly accelerators: "accelerators";
    readonly accTitle: "accTitle";
    readonly anchors: "anchors";
    readonly annotation: "annotation";
    readonly annotations: "annotations";
    readonly components: "components";
    readonly deaccelerators: "deaccelerators";
    readonly evolution: "evolution";
    readonly evolves: "evolves";
    readonly links: "links";
    readonly notes: "notes";
    readonly pipelines: "pipelines";
    readonly size: "size";
    readonly title: "title";
};

/**
 * Declaration of `Wardley` services.
 */
declare interface WardleyAddedServices {
    parser: {
        ValueConverter: WardleyValueConverter;
    };
}

export declare const WardleyGeneratedModule: Module<LangiumCoreServices, LangiumGeneratedCoreServices>;

/** Contains the reachable terminals & keywords and all available types of the 'WardleyGrammar' language. */
declare namespace WardleyGrammar {
    const Terminals: {
        WARDLEY_NUMBER: RegExp;
        ARROW: RegExp;
        LINK_PORT: RegExp;
        LINK_ARROW: RegExp;
        LINK_LABEL: RegExp;
        STRATEGY: RegExp;
        KW_WARDLEY: RegExp;
        KW_SIZE: RegExp;
        KW_EVOLUTION: RegExp;
        KW_ANCHOR: RegExp;
        KW_COMPONENT: RegExp;
        KW_LABEL: RegExp;
        KW_INERTIA: RegExp;
        KW_EVOLVE: RegExp;
        KW_PIPELINE: RegExp;
        KW_NOTE: RegExp;
        KW_ANNOTATIONS: RegExp;
        KW_ANNOTATION: RegExp;
        KW_ACCELERATOR: RegExp;
        KW_DEACCELERATOR: RegExp;
        NAME_WITH_SPACES: RegExp;
        WS: RegExp;
        ACC_DESCR: RegExp;
        ACC_TITLE: RegExp;
        TITLE: RegExp;
        INT: RegExp;
        STRING: RegExp;
        ID: RegExp;
        NEWLINE: RegExp;
        WHITESPACE: RegExp;
        YAML: RegExp;
        DIRECTIVE: RegExp;
        SINGLE_LINE_COMMENT: RegExp;
    };
    type TerminalNames = keyof typeof Terminals;
    type KeywordNames = "(" | ")" | "," | "-" | "/" | "@" | "[" | "]" | "{" | "}";
    type TokenNames = TerminalNames | KeywordNames;
    type AstType = {
        Accelerator: Accelerator;
        Anchor: Anchor;
        Annotation: Annotation;
        Annotations: Annotations;
        Component: Component;
        Deaccelerator: Deaccelerator;
        Decorator: Decorator;
        Evolution: Evolution;
        EvolutionStage: EvolutionStage;
        Evolve: Evolve;
        Label: Label;
        Link: Link;
        Note: Note;
        Pipeline: Pipeline;
        PipelineComponent: PipelineComponent;
        Size: Size;
        Wardley: Wardley;
    };
}

/**
 * Dependency injection module that overrides Langium default services and
 * contributes the declared `Wardley` services.
 */
export declare const WardleyModule: Module<WardleyServices, PartialLangiumCoreServices & WardleyAddedServices>;

/**
 * Union of Langium default services and `Wardley` services.
 */
export declare type WardleyServices = LangiumCoreServices & WardleyAddedServices;

declare class WardleyValueConverter extends AbstractMermaidValueConverter {
    protected runCustomConverter(rule: GrammarAST.AbstractRule, input: string, _cstNode: CstNode): ValueType | undefined;
}

/**
 * A set of all characters that are considered whitespace by the '\s' RegExp character class.
 * Taken from [MDN](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Regular_expressions/Character_classes).
 */
declare const whitespaceCharacters: string[];

declare interface Wildcard extends TerminalElement {
    readonly $type: 'Wildcard';
}

declare const Wildcard: {
    readonly $type: "Wildcard";
    readonly cardinality: "cardinality";
    readonly lookahead: "lookahead";
    readonly parenthesized: "parenthesized";
};

declare interface WindowClientCapabilities {
    /**
     * It indicates whether the client supports server initiated
     * progress using the `window/workDoneProgress/create` request.
     *
     * The capability also controls Whether client supports handling
     * of progress notifications. If set servers are allowed to report a
     * `workDoneProgress` property in the request specific server
     * capabilities.
     *
     * @since 3.15.0
     */
    workDoneProgress?: boolean;
    /**
     * Capabilities specific to the showMessage request.
     *
     * @since 3.16.0
     */
    showMessage?: ShowMessageRequestClientCapabilities;
    /**
     * Capabilities specific to the showDocument request.
     *
     * @since 3.16.0
     */
    showDocument?: ShowDocumentClientCapabilities;
}

declare interface WorkDoneProgressParams {
    /**
     * An optional token that a server can use to report work done progress.
     */
    workDoneToken?: ProgressToken;
}

declare type WorkerMessageCallback = (cb: (message: unknown) => void) => void;

declare type WorkerMessagePost = (message: unknown) => void;

/**
 * Every key/value pair in this cache is scoped to the whole workspace.
 * If any document in the workspace is added, changed or deleted, the whole cache is evicted.
 */
declare class WorkspaceCache<K, V> extends SimpleCache<K, V> {
    /**
     * Creates a new workspace cache.
     *
     * @param sharedServices Service container instance to hook into document lifecycle events.
     * @param state Optional document state on which the cache should evict.
     * If not provided, the cache will evict on `DocumentBuilder#onUpdate`.
     * *Deleted* documents are considered in both cases.
     */
    constructor(sharedServices: LangiumSharedCoreServices, state?: DocumentState);
}

/**
 * Workspace specific client capabilities.
 */
declare interface WorkspaceClientCapabilities {
    /**
     * The client supports applying batch edits
     * to the workspace by supporting the request
     * 'workspace/applyEdit'
     */
    applyEdit?: boolean;
    /**
     * Capabilities specific to `WorkspaceEdit`s.
     */
    workspaceEdit?: WorkspaceEditClientCapabilities;
    /**
     * Capabilities specific to the `workspace/didChangeConfiguration` notification.
     */
    didChangeConfiguration?: DidChangeConfigurationClientCapabilities;
    /**
     * Capabilities specific to the `workspace/didChangeWatchedFiles` notification.
     */
    didChangeWatchedFiles?: DidChangeWatchedFilesClientCapabilities;
    /**
     * Capabilities specific to the `workspace/symbol` request.
     */
    symbol?: WorkspaceSymbolClientCapabilities;
    /**
     * Capabilities specific to the `workspace/executeCommand` request.
     */
    executeCommand?: ExecuteCommandClientCapabilities;
    /**
     * The client has support for workspace folders.
     *
     * @since 3.6.0
     */
    workspaceFolders?: boolean;
    /**
     * The client supports `workspace/configuration` requests.
     *
     * @since 3.6.0
     */
    configuration?: boolean;
    /**
     * Capabilities specific to the semantic token requests scoped to the
     * workspace.
     *
     * @since 3.16.0.
     */
    semanticTokens?: SemanticTokensWorkspaceClientCapabilities;
    /**
     * Capabilities specific to the code lens requests scoped to the
     * workspace.
     *
     * @since 3.16.0.
     */
    codeLens?: CodeLensWorkspaceClientCapabilities;
    /**
     * The client has support for file notifications/requests for user operations on files.
     *
     * Since 3.16.0
     */
    fileOperations?: FileOperationClientCapabilities;
    /**
     * Capabilities specific to the inline values requests scoped to the
     * workspace.
     *
     * @since 3.17.0.
     */
    inlineValue?: InlineValueWorkspaceClientCapabilities;
    /**
     * Capabilities specific to the inlay hint requests scoped to the
     * workspace.
     *
     * @since 3.17.0.
     */
    inlayHint?: InlayHintWorkspaceClientCapabilities;
    /**
     * Capabilities specific to the diagnostic requests scoped to the
     * workspace.
     *
     * @since 3.17.0.
     */
    diagnostics?: DiagnosticWorkspaceClientCapabilities;
    /**
     * Capabilities specific to the folding range requests scoped to the workspace.
     *
     * @since 3.18.0
     * @proposed
     */
    foldingRange?: FoldingRangeWorkspaceClientCapabilities;
}

declare interface WorkspaceEditClientCapabilities {
    /**
     * The client supports versioned document changes in `WorkspaceEdit`s
     */
    documentChanges?: boolean;
    /**
     * The resource operations the client supports. Clients should at least
     * support 'create', 'rename' and 'delete' files and folders.
     *
     * @since 3.13.0
     */
    resourceOperations?: ResourceOperationKind[];
    /**
     * The failure handling strategy of a client if applying the workspace edit
     * fails.
     *
     * @since 3.13.0
     */
    failureHandling?: FailureHandlingKind;
    /**
     * Whether the client normalizes line endings to the client specific
     * setting.
     * If set to `true` the client will normalize line ending characters
     * in a workspace edit to the client-specified new line
     * character.
     *
     * @since 3.16.0
     */
    normalizesLineEndings?: boolean;
    /**
     * Whether the client in general supports change annotations on text edits,
     * create file, rename file and delete file changes.
     *
     * @since 3.16.0
     */
    changeAnnotationSupport?: {
        /**
         * Whether the client groups edits with equal labels into tree nodes,
         * for instance all edits labelled with "Changes in Strings" would
         * be a tree node.
         */
        groupsOnLabel?: boolean;
    };
}

/**
 * A workspace folder inside a client.
 */
declare interface WorkspaceFolder {
    /**
     * The associated URI for this workspace folder.
     */
    uri: URI_3;
    /**
     * The name of the workspace folder. Used to refer to this
     * workspace folder in the user interface.
     */
    name: string;
}

declare namespace WorkspaceFolder {
    function is(value: any): value is WorkspaceFolder;
}

/**
 * A workspace folder inside a client.
 */
declare interface WorkspaceFolder_2 {
    /**
     * The associated URI for this workspace folder.
     */
    uri: URI_2;
    /**
     * The name of the workspace folder. Used to refer to this
     * workspace folder in the user interface.
     */
    name: string;
}

declare namespace WorkspaceFolder_2 {
    function is(value: any): value is WorkspaceFolder_2;
}

declare interface WorkspaceFoldersInitializeParams {
    /**
     * The workspace folders configured in the client when the server starts.
     *
     * This property is only available if the client supports workspace folders.
     * It can be `null` if the client supports workspace folders but none are
     * configured.
     *
     * @since 3.6.0
     */
    workspaceFolders?: WorkspaceFolder[] | null;
}

/**
 * Utility service to execute mutually exclusive actions.
 */
declare interface WorkspaceLock {
    /**
     * Performs a single async action, like initializing the workspace or processing document changes.
     * Only one action will be executed at a time.
     *
     * When another action is queued up, the token provided for the action will be cancelled.
     * Assuming the action makes use of this token, the next action only has to wait for the current action to finish cancellation.
     */
    write(action: (token: CancellationToken) => MaybePromise<void>): Promise<void>;
    /**
     * Performs a single action, like computing completion results or providing workspace symbols.
     * Read actions will only be executed after all write actions have finished. They will be executed in parallel if possible.
     *
     * If a write action is currently running, the read action will be queued up and executed afterwards.
     * If a new write action is queued up while a read action is waiting, the write action will receive priority and will be handled before the read action.
     *
     * Note that read actions are not allowed to modify anything in the workspace. Please use {@link write} instead.
     */
    read<T>(action: () => MaybePromise<T>): Promise<T>;
    /**
     * Cancels the last queued write action. All previous write actions already have been cancelled.
     */
    cancelWrite(): void;
}

/**
 * The workspace manager is responsible for finding source files in the workspace.
 * This service is shared between all languages of a language server.
 */
declare interface WorkspaceManager {
    /** The options used for the initial workspace build. */
    initialBuildOptions: BuildOptions | undefined;
    /**
     * A promise that resolves when the workspace manager is ready to be used.
     * Use this to ensure that the workspace manager has finished its initialization.
     */
    readonly ready: Promise<void>;
    /**
     * The workspace folders of the current workspace.
     * Available only after the `ready` promise resolves.
     */
    get workspaceFolders(): readonly WorkspaceFolder_2[] | undefined;
    /**
     * When used in a language server context, this method is called when the server receives
     * the `initialize` request.
     */
    initialize(params: InitializeParams): void;
    /**
     * When used in a language server context, this method is called when the server receives
     * the `initialized` notification.
     */
    initialized(params: InitializedParams): Promise<void>;
    /**
     * Does the initial indexing of workspace folders.
     * Collects information about exported and referenced AstNodes in
     * each language file and stores it locally.
     *
     * @param folders The set of workspace folders to be indexed.
     * @param cancelToken A cancellation token that can be used to cancel the operation.
     *
     * @throws OperationCancelled if a cancellation event has been detected
     */
    initializeWorkspace(folders: WorkspaceFolder_2[], cancelToken?: CancellationToken): Promise<void>;
    /**
     * Searches for workspace files in the given folder and its subdirectories.
     * Note that this method does not create documents for the found files.
     * @param uri The URI of the folder to search in.
     * @returns A promise that resolves to an array of URIs of the found files.
     */
    searchFolder(uri: URI): Promise<URI[]>;
    /**
     * Determine whether the given file system node shall be included in the workspace.
     * @param entry The file system node to check.
     * @returns `true` if the entry shall be included, `false` otherwise.
     */
    shouldIncludeEntry(entry: FileSystemNode): boolean;
}

/**
 * Client capabilities for a {@link WorkspaceSymbolRequest}.
 */
declare interface WorkspaceSymbolClientCapabilities {
    /**
     * Symbol request supports dynamic registration.
     */
    dynamicRegistration?: boolean;
    /**
     * Specific capabilities for the `SymbolKind` in the `workspace/symbol` request.
     */
    symbolKind?: {
        /**
         * The symbol kind values the client supports. When this
         * property exists the client also guarantees that it will
         * handle values outside its set gracefully and falls back
         * to a default value when unknown.
         *
         * If this property is not present the client only supports
         * the symbol kinds from `File` to `Array` as defined in
         * the initial version of the protocol.
         */
        valueSet?: SymbolKind[];
    };
    /**
     * The client supports tags on `SymbolInformation`.
     * Clients supporting tags have to handle unknown tags gracefully.
     *
     * @since 3.16.0
     */
    tagSupport?: {
        /**
         * The tags supported by the client.
         */
        valueSet: SymbolTag[];
    };
    /**
     * The client support partial workspace symbols. The client will send the
     * request `workspaceSymbol/resolve` to the server to resolve additional
     * properties.
     *
     * @since 3.17.0
     */
    resolveSupport?: {
        /**
         * The properties that a client can resolve lazily. Usually
         * `location.range`
         */
        properties: string[];
    };
}

export { }
