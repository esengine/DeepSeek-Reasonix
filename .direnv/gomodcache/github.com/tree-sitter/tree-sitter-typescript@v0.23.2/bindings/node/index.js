const root = require("path").join(__dirname, "..", "..");

module.exports =
  typeof process.versions.bun === "string"
    // Support `bun build --compile` by being statically analyzable enough to find the .node file at build-time
    ? require(`../../prebuilds/${process.platform}-${process.arch}/tree-sitter-typescript.node`)
    : require("node-gyp-build")(root);

try {
  module.exports.typescript.nodeTypeInfo = require("../../typescript/src/node-types.json");
  module.exports.tsx.nodeTypeInfo = require("../../tsx/src/node-types.json");
} catch (_) { }
