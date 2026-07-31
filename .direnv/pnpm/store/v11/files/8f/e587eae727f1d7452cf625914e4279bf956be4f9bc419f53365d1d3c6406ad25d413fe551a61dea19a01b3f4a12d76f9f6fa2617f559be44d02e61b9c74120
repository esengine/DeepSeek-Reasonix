const compare = (s1, s2) => {
    if (s1.a === s2.a) {
        if (s1.b === s2.b) {
            return s1.c - s2.c;
        }
        return s1.b - s2.b;
    }
    return s1.a - s2.a;
};

const equals = (s1, s2) => {
    return compare(s1, s2) === 0;
};

const greaterThan = (s1, s2) => {
    return compare(s1, s2) > 0;
};

const lessThan = (s1, s2) => {
    return compare(s1, s2) < 0;
};

export { compare, equals, greaterThan, lessThan };
