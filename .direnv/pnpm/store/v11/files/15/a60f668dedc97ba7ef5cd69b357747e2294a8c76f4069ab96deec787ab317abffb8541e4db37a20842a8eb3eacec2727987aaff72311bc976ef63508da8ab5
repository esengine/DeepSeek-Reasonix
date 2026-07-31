export class DOMSelector {
    constructor(window: Window, document: Document, opt?: object);
    clear: () => void;
    extractSubjects: (selector: string) => Array<{
        id: string | null;
        className: string | null;
        tag: string | null;
    }>;
    check: (selector: string, node: Element, opt?: object) => CheckResult;
    matches: (selector: string, node: Element, opt?: object) => boolean;
    closest: (selector: string, node: Element, opt?: object) => Element | null;
    querySelector: (selector: string, node: Document | DocumentFragment | Element, opt?: object) => Element | null;
    querySelectorAll: (selector: string, node: Document | DocumentFragment | Element, opt?: object) => Array<Element>;
    #private;
}
export type CheckResult = {
    match: boolean;
    pseudoElement: string | null;
    ast: object | null;
};
