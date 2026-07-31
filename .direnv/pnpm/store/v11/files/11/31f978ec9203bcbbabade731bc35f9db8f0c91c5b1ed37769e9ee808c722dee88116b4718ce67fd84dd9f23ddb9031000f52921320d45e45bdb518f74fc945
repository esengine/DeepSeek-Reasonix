/* eslint no-console:0 */
/**
 * This is a module for storing settings passed into KaTeX. It correctly handles
 * default settings.
 */

import {protocolFromUrl} from "./utils";
import ParseError from "./ParseError";
import {Token} from "./Token";

import type {AnyParseNode} from "./parseNode";
import type {MacroMap} from "./defineMacro";

export type StrictFunction =
    (errorCode: string, errorMsg: string, token?: Token | AnyParseNode) =>
    (boolean | string) | null | undefined;

export type TrustContextTypes = {
    "\\href": {
        command: "\\href";
        url: string;
        protocol?: string;
    };
    "\\includegraphics": {
        command: "\\includegraphics";
        url: string;
        protocol?: string;
    };
    "\\url": {
        command: "\\url";
        url: string;
        protocol?: string;
    };
    "\\htmlClass": {
        command: "\\htmlClass";
        class: string;
    };
    "\\htmlId": {
        command: "\\htmlId";
        id: string;
    };
    "\\htmlStyle": {
        command: "\\htmlStyle";
        style: string;
    };
    "\\htmlData": {
        command: "\\htmlData";
        attributes: Record<string, string>;
    };
};
export type AnyTrustContext = TrustContextTypes[keyof TrustContextTypes];
export type TrustFunction = (context: AnyTrustContext) => boolean | null | undefined;
export type SettingsOptions = Partial<Settings>;

type EnumType<T extends string = string> = {
    enum: T[];
};

type Type = "boolean" | "string" | "number" | "object" | "function" | EnumType;
/**
 * Union of all values that appear as schema defaults, cliDefaults, or
 * cliProcessor return values.  StrictFunction / TrustFunction are
 * option-value types, not default/schema values, so they are excluded.
 */
type SettingsValue = boolean | string | number | MacroMap | string[];
type DefaultValue = Exclude<SettingsValue, string[]>;

type SchemaMetadata<K extends keyof SettingsOptions> = {
    /**
     * The description.
     */
    description?: string;
    /**
     * The function to process the option.
     */
    processor?: (value: Settings[K]) => Settings[K];
    /**
     * The command line argument. See Commander.js docs for more information.
     * If not specified, the name prefixed with -- will be used. Set false not
     * to add to the CLI.
     */
    cli?: string | false;
    /**
     * The default value for the CLI.
     */
    cliDefault?: SettingsValue;
    /**
     * The description for the CLI. If not specified, the description for the
     * option will be used.
     */
    cliDescription?: string;
    /**
     * The custom argument processor for the CLI. See Commander.js docs for
     * more information. Signature varies per setting (e.g. parseFloat,
     * or (def, defs) => defs.push(def) for macros).
     */
    cliProcessor?: (...args: any[]) => SettingsValue;
};

type BooleanSchema<K extends keyof SettingsOptions> = SchemaMetadata<K> & {
    /**
     * Allowed type(s) of the value.
     */
    type: "boolean";
    /**
     * The default value. If not specified, false will be used.
     */
    default?: Extract<Settings[K], boolean>;
};

type StringSchema<K extends keyof SettingsOptions> = SchemaMetadata<K> & {
    /**
     * Allowed type(s) of the value.
     */
    type: "string";
    /**
     * The default value. If not specified, an empty string will be used.
     */
    default?: Extract<Settings[K], string>;
};

type NumberSchema<K extends keyof SettingsOptions> = SchemaMetadata<K> & {
    /**
     * Allowed type(s) of the value.
     */
    type: "number";
    /**
     * The default value. If not specified, 0 will be used.
     */
    default?: Extract<Settings[K], number>;
};

type ObjectSchema<K extends keyof SettingsOptions> = SchemaMetadata<K> & {
    /**
     * Allowed type(s) of the value.
     */
    type: "object";
    /**
     * The default value. If not specified, an empty object will be used.
     */
    default?: Extract<Settings[K], MacroMap>;
};

type FunctionSchema<K extends keyof SettingsOptions> = SchemaMetadata<K> & {
    /**
     * Allowed type(s) of the value.
     */
    type: "function";
    /**
     * Settings do not currently use function-valued defaults.
     */
    default?: never;
};

type EnumSchema<K extends keyof SettingsOptions> = SchemaMetadata<K> & {
    /**
     * Allowed type(s) of the value.
     */
    type: EnumType<Extract<Settings[K], string>>;
    /**
     * The default value. If not specified, the first enum value will be used.
     */
    default?: Extract<Settings[K], string>;
};

type SingleTypeSchema<K extends keyof SettingsOptions> =
    | BooleanSchema<K>
    | StringSchema<K>
    | NumberSchema<K>
    | ObjectSchema<K>
    | FunctionSchema<K>
    | EnumSchema<K>;

type MultiTypeSchema<K extends keyof SettingsOptions> = SchemaMetadata<K> & {
    /**
     * Allowed type(s) of the value.
     */
    type: [SingleTypeSchema<K>["type"], ...Array<SingleTypeSchema<K>["type"]>];
    /**
     * The default value. If not specified, the first allowed type determines
     * the default value.
     */
    default?: Extract<Settings[K], DefaultValue>;
};

type SchemaEntry<K extends keyof SettingsOptions> =
    | SingleTypeSchema<K>
    | MultiTypeSchema<K>;

type Schema = {
    [key in keyof SettingsOptions]?: SchemaEntry<key>;
};

// TODO: automatically generate documentation
// TODO: check all properties on Settings exist
// TODO: check the type of a property on Settings matches
export const SETTINGS_SCHEMA: Schema = {
    displayMode: {
        type: "boolean",
        description: "Render math in display mode, which puts the math in " +
            "display style (so \\int and \\sum are large, for example), and " +
            "centers the math on the page on its own line.",
        cli: "-d, --display-mode",
    },
    output: {
        type: {enum: ["htmlAndMathml", "html", "mathml"]},
        description: "Determines the markup language of the output.",
        cli: "-F, --format <type>",
    },
    leqno: {
        type: "boolean",
        description: "Render display math in leqno style (left-justified tags).",
    },
    fleqn: {
        type: "boolean",
        description: "Render display math flush left.",
    },
    throwOnError: {
        type: "boolean",
        default: true,
        cli: "-t, --no-throw-on-error",
        cliDescription: "Render errors (in the color given by --error-color) ins" +
            "tead of throwing a ParseError exception when encountering an error.",
    },
    errorColor: {
        type: "string",
        default: "#cc0000",
        cli: "-c, --error-color <color>",
        cliDescription: "A color string given in the format 'rgb' or 'rrggbb' " +
            "(no #). This option determines the color of errors rendered by the " +
            "-t option.",
        cliProcessor: (color) => "#" + color,
    },
    macros: {
        type: "object",
        cli: "-m, --macro <def>",
        cliDescription: "Define custom macro of the form '\\foo:expansion' (use " +
            "multiple -m arguments for multiple macros).",
        cliDefault: [],
        cliProcessor: (def, defs) => {
            defs.push(def);
            return defs;
        },
    },
    minRuleThickness: {
        type: "number",
        description: "Specifies a minimum thickness, in ems, for fraction lines," +
            " `\\sqrt` top lines, `{array}` vertical lines, `\\hline`, " +
            "`\\hdashline`, `\\underline`, `\\overline`, and the borders of " +
            "`\\fbox`, `\\boxed`, and `\\fcolorbox`.",
        processor: (t) => Math.max(0, t),
        cli: "--min-rule-thickness <size>",
        cliProcessor: parseFloat,
    },
    colorIsTextColor: {
        type: "boolean",
        description: "Makes \\color behave like LaTeX's 2-argument \\textcolor, " +
            "instead of LaTeX's one-argument \\color mode change.",
        cli: "-b, --color-is-text-color",
    },
    strict: {
        type: [{enum: ["warn", "ignore", "error"]}, "boolean", "function"],
        description: "Turn on strict / LaTeX faithfulness mode, which throws an " +
            "error if the input uses features that are not supported by LaTeX.",
        cli: "-S, --strict",
        cliDefault: false,
    },
    trust: {
        type: ["boolean", "function"],
        description: "Trust the input, enabling all HTML features such as \\url.",
        cli: "-T, --trust",
    },
    maxSize: {
        type: "number",
        default: Infinity,
        description: "If non-zero, all user-specified sizes, e.g. in " +
            "\\rule{500em}{500em}, will be capped to maxSize ems. Otherwise, " +
            "elements and spaces can be arbitrarily large",
        processor: (s) => Math.max(0, s),
        cli: "-s, --max-size <n>",
        cliProcessor: parseInt,
    },
    maxExpand: {
        type: "number",
        default: 1000,
        description: "Limit the number of macro expansions to the specified " +
            "number, to prevent e.g. infinite macro loops. If set to Infinity, " +
            "the macro expander will try to fully expand as in LaTeX.",
        processor: (n) => Math.max(0, n),
        cli: "-e, --max-expand <n>",
        cliProcessor: (n) => (n === "Infinity" ? Infinity : parseInt(n)),
    },
    globalGroup: {
        type: "boolean",
        cli: false,
    },
};

function getImplicitDefault(type: "boolean"): boolean;
function getImplicitDefault(type: "string"): string;
function getImplicitDefault(type: "number"): number;
function getImplicitDefault(type: "object"): MacroMap;
function getImplicitDefault(type: "function"): never;
function getImplicitDefault<T extends string>(type: EnumType<T>): T;
function getImplicitDefault(type: Type): DefaultValue;
function getImplicitDefault(type: Type): DefaultValue {
    if (typeof type !== 'string') {
        return type.enum[0];
    }
    switch (type) {
        case 'boolean':
            return false;
        case 'string':
            return '';
        case 'number':
            return 0;
        case 'object':
            return {};
        default:
            throw new Error(
                "Unexpected schema type; settings must declare an explicit default.");
    }
}

function getDefaultValue<K extends keyof SettingsOptions>(
    schema: SchemaEntry<K>,
): Settings[K];
function getDefaultValue(schema: SchemaEntry<keyof SettingsOptions>): DefaultValue {
    if (schema.default !== undefined) {
        return schema.default;
    }
    const type = Array.isArray(schema.type) ? schema.type[0] : schema.type;
    return getImplicitDefault(type);
}

function applySetting<K extends keyof SettingsOptions>(
    target: Settings,
    prop: K,
    options: SettingsOptions,
    schema: SchemaEntry<K>,
) {
    const optionValue = options[prop];
    target[prop] = optionValue !== undefined
        ? (schema.processor
            ? schema.processor(optionValue)
            : optionValue)
        : getDefaultValue(schema);
}

/**
 * The main Settings object
 *
 * The current options stored are:
 *  - displayMode: Whether the expression should be typeset as inline math
 *                 (false, the default), meaning that the math starts in
 *                 \textstyle and is placed in an inline-block); or as display
 *                 math (true), meaning that the math starts in \displaystyle
 *                 and is placed in a block with vertical margin.
 */
export default class Settings {
    displayMode!: boolean;
    output!: "html" | "mathml" | "htmlAndMathml";
    leqno!: boolean;
    fleqn!: boolean;
    throwOnError!: boolean;
    errorColor!: string;
    macros!: MacroMap;
    minRuleThickness!: number;
    colorIsTextColor!: boolean;
    strict!: boolean | "ignore" | "warn" | "error" | StrictFunction;
    trust!: boolean | TrustFunction;
    maxSize!: number;
    maxExpand!: number;
    globalGroup!: boolean;

    constructor(options: SettingsOptions = {}) {
        // allow null options
        options = options || {};
        for (const prop of Object.keys(SETTINGS_SCHEMA) as Array<keyof SettingsOptions>) {
            const schema = SETTINGS_SCHEMA[prop] as SchemaEntry<typeof prop> | undefined;
            if (schema) {
                // TODO: validate options
                applySetting(this, prop, options, schema);
            }
        }
    }

    /**
     * Report nonstrict (non-LaTeX-compatible) input.
     * Can safely not be called if `this.strict` is false in JavaScript.
     */
    reportNonstrict(errorCode: string, errorMsg: string,
                    token?: Token | AnyParseNode) {
        let strict: Settings["strict"] | ReturnType<StrictFunction> = this.strict;
        if (typeof strict === "function") {
            // Allow return value of strict function to be boolean or string
            // (or null/undefined, meaning no further processing).
            strict = strict(errorCode, errorMsg, token);
        }
        if (!strict || strict === "ignore") {
            return;
        } else if (strict === true || strict === "error") {
            throw new ParseError(
                "LaTeX-incompatible input and strict mode is set to 'error': " +
                `${errorMsg} [${errorCode}]`, token);
        } else if (strict === "warn") {
            typeof console !== "undefined" && console.warn(
                "LaTeX-incompatible input and strict mode is set to 'warn': " +
                `${errorMsg} [${errorCode}]`);
        } else {  // won't happen in type-safe code
            typeof console !== "undefined" && console.warn(
                "LaTeX-incompatible input and strict mode is set to " +
                `unrecognized '${strict}': ${errorMsg} [${errorCode}]`);
        }
    }

    /**
     * Check whether to apply strict (LaTeX-adhering) behavior for unusual
     * input (like `\\`).  Unlike `nonstrict`, will not throw an error;
     * instead, "error" translates to a return value of `true`, while "ignore"
     * translates to a return value of `false`.  May still print a warning:
     * "warn" prints a warning and returns `false`.
     * This is for the second category of `errorCode`s listed in the README.
     */
    useStrictBehavior(errorCode: string, errorMsg: string,
                      token?: Token | AnyParseNode): boolean {
        let strict: Settings["strict"] | ReturnType<StrictFunction> = this.strict;
        if (typeof strict === "function") {
            // Allow return value of strict function to be boolean or string
            // (or null/undefined, meaning no further processing).
            // But catch any exceptions thrown by function, treating them
            // like "error".
            try {
                strict = strict(errorCode, errorMsg, token);
            } catch (error) {
                strict = "error";
            }
        }
        if (!strict || strict === "ignore") {
            return false;
        } else if (strict === true || strict === "error") {
            return true;
        } else if (strict === "warn") {
            typeof console !== "undefined" && console.warn(
                "LaTeX-incompatible input and strict mode is set to 'warn': " +
                `${errorMsg} [${errorCode}]`);
            return false;
        } else {  // won't happen in type-safe code
            typeof console !== "undefined" && console.warn(
                "LaTeX-incompatible input and strict mode is set to " +
                `unrecognized '${strict}': ${errorMsg} [${errorCode}]`);
            return false;
        }
    }

    /**
     * Check whether to test potentially dangerous input, and return
     * `true` (trusted) or `false` (untrusted).  The sole argument `context`
     * should be an object with `command` field specifying the relevant LaTeX
     * command (as a string starting with `\`), and any other arguments, etc.
     * If `context` has a `url` field, a `protocol` field will automatically
     * get added by this function (changing the specified object).
     */
    isTrusted(context: AnyTrustContext): boolean {
        if ("url" in context && context.url && !context.protocol) {
            const protocol = protocolFromUrl(context.url);
            if (protocol == null) {
                return false;
            }
            context.protocol = protocol;
        }
        const trust = typeof this.trust === "function"
            ? this.trust(context)
            : this.trust;
        return Boolean(trust);
    }
}
