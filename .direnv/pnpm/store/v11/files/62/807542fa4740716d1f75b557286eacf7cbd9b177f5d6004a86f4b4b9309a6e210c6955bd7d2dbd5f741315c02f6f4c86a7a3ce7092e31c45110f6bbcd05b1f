import generate from 'css-tree/generator';

import { calculate, calculateForAST } from './core/index.js';
import { compare, equals, greaterThan, lessThan } from './util/compare.js';
import { min, max } from './util/filter.js';
import { sortAsc, sortDesc } from './util/sort.js';

class NotAllowedError extends Error {
    constructor() {
        super('Manipulating a Specificity instance is not allowed. Instead, create a new Specificity()');
    }
}

class Specificity {
    constructor(value, selector = null) {
        this.value = value;
        this.selector = selector;
    }

    get a() {
        return this.value.a;
    }

    set a(val) {
        throw new NotAllowedError();
    }

    get b() {
        return this.value.b;
    }

    set b(val) {
        throw new NotAllowedError();
    }

    get c() {
        return this.value.c;
    }

    set c(val) {
        throw new NotAllowedError();
    }

    selectorString() {
        // this.selector already is a String
        if (typeof this.selector === 'string' || this.selector instanceof String) {
            return this.selector;
        }

        // this.selector is a Selector as parsed by CSSTree
        if (this.selector instanceof Object) {
            if (this.selector.type === 'Selector') {
                return generate(this.selector);
            }
        }

        // this.selector is something else …
        return '';
    }

    toObject() {
        return this.value;
    }

    toArray() {
        return [this.value.a, this.value.b, this.value.c];
    }

    toString() {
        return `(${this.value.a},${this.value.b},${this.value.c})`;
    }

    toJSON() {
        return {
            selector: this.selectorString(),
            asObject: this.toObject(),
            asArray: this.toArray(),
            asString: this.toString(),
        };
    }

    isEqualTo(otherSpecificity) {
        return equals(this, otherSpecificity);
    }

    isGreaterThan(otherSpecificity) {
        return greaterThan(this, otherSpecificity);
    }

    isLessThan(otherSpecificity) {
        return lessThan(this, otherSpecificity);
    }

    static calculate(selector) {
        return calculate(selector);
    }

    static calculateForAST(selector) {
        return calculateForAST(selector);
    }

    static compare(s1, s2) {
        return compare(s1, s2);
    }

    static equals(s1, s2) {
        return equals(s1, s2);
    }

    static lessThan(s1, s2) {
        return lessThan(s1, s2);
    }

    static greaterThan(s1, s2) {
        return greaterThan(s1, s2);
    }

    static min(...specificities) {
        return min(...specificities);
    }

    static max(...specificities) {
        return max(...specificities);
    }

    static sortAsc(...specificities) {
        return sortAsc(...specificities);
    }

    static sortDesc(...specificities) {
        return sortDesc(...specificities);
    }
}

export default Specificity;
