import { compare } from './compare.js';

const sort = (specificities, order = 'ASC') => {
    const sorted = specificities.sort(compare);

    if (order === 'DESC') {
        return sorted.reverse();
    }

    return sorted;
};

const sortAsc = (...specificities) => {
    return sort(specificities, 'ASC');
};

const sortDesc = (...specificities) => {
    return sort(specificities, 'DESC');
};

export { sortAsc, sortDesc };
