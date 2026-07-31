#!/usr/bin/env node
import Specificity from '../dist/index.js';

if (!process.argv[2]) {
    console.error('❌ Missing selector argument');
    process.exit(1);
}

try {
    const specificities = Specificity.calculate(process.argv[2]);
    console.log(specificities.map((specificity) => `${specificity}`).join('\n'));
} catch (e) {
    console.error(`❌ ${e.message}`);
}
