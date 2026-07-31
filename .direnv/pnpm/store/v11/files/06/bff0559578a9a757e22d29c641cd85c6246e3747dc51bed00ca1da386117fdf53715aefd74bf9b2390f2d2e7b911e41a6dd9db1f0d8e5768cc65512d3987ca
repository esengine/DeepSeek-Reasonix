import type {Data, Literal} from 'mdast'

export {mathFromMarkdown, mathToMarkdown} from './lib/index.js'

export type {ToOptions} from './lib/index.js'

/**
 * Math (flow).
 */
export interface Math extends Literal {
  /**
   * Node type of math (flow).
   */
  type: 'math'

  /**
   * Custom information relating to the node.
   */
  meta?: string | null | undefined

  /**
   * Data associated with the mdast math (flow).
   */
  data?: MathData | undefined
}

/**
 * Info associated with mdast math (flow) nodes by the ecosystem.
 */
export interface MathData extends Data {}

/**
 * Math (text).
 */
export interface InlineMath extends Literal {
  /**
   * Node type of math (text).
   */
  type: 'inlineMath'

  /**
   * Data associated with the mdast math (text).
   */
  data?: InlineMathData | undefined
}

/**
 * Info associated with mdast math (text) nodes by the ecosystem.
 */
export interface InlineMathData extends Data {}

// Add custom data tracked to turn markdown into a tree.
declare module 'mdast-util-from-markdown' {
  interface CompileData {
    /**
     * Whether we’re in math (flow).
     */
    mathFlowInside?: boolean | undefined
  }
}

// Add custom data tracked to turn a tree into markdown.
declare module 'mdast-util-to-markdown' {
  interface ConstructNameMap {
    /**
     * Math (flow).
     *
     * ```markdown
     * > | $$
     *     ^^
     * > | a
     *     ^
     * > | $$
     *     ^^
     * ```
     */
    mathFlow: 'mathFlow'

    /**
     * Math (flow) meta flag.
     *
     * ```markdown
     * > | $$a
     *       ^
     *   | b
     *   | $$
     * ```
     */
    mathFlowMeta: 'mathFlowMeta'
  }
}

// Add nodes to tree.
declare module 'mdast' {
  interface BlockContentMap {
    math: Math
  }

  interface PhrasingContentMap {
    inlineMath: InlineMath
  }

  interface RootContentMap {
    inlineMath: InlineMath
    math: Math
  }
}
