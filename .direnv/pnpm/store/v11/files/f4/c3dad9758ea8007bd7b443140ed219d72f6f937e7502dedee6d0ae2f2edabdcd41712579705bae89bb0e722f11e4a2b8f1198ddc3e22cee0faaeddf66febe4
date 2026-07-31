import {NON_ATOMS} from "./atoms";
import type {AnyParseNode, NodeType, ParseNode, SymbolParseNode} from "./types/nodes";

/**
 * Asserts that the node is of the given type and returns it with stricter
 * typing. Throws if the node's type does not match.
 */
export function assertNodeType<NODETYPE extends NodeType>(
    node: AnyParseNode | null | undefined,
    type: NODETYPE,
): ParseNode<NODETYPE> {
    if (!node || node.type !== type) {
        throw new Error(
            `Expected node of type ${type}, but got ` +
            (node ? `node of type ${node.type}` : String(node)));
    }

    return node as ParseNode<NODETYPE>;
}

/**
 * Returns the node more strictly typed iff it is of the given type. Otherwise,
 * returns null.
 */
export function assertSymbolNodeType(node: AnyParseNode | null | undefined): SymbolParseNode {
    const typedNode = checkSymbolNodeType(node);
    if (!typedNode) {
        throw new Error(
            `Expected node of symbol group type, but got ` +
            (node ? `node of type ${node.type}` : String(node)));
    }
    return typedNode;
}

/**
 * Returns the node more strictly typed if it is of the given type. Otherwise,
 * returns null.
 */
export function checkSymbolNodeType(node: AnyParseNode | null | undefined): SymbolParseNode | null | undefined {
    if (node && (node.type === "atom" || NON_ATOMS.hasOwnProperty(node.type))) {
        return node as SymbolParseNode;
    }
    return null;
}
