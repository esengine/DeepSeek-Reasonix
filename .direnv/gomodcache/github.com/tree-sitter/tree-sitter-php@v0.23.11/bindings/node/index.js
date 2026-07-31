const root = require("path").join(__dirname, "..", "..");

module.exports =
  typeof process.versions.bun === "string"
    // Support `bun build --compile` by being statically analyzable enough to find the .node file at build-time
    ? require(`../../prebuilds/${process.platform}-${process.arch}/tree-sitter-php.node`)
    : require("node-gyp-build")(root);

try {
  module.exports.php.nodeTypeInfo = require("../../php/src/node-types.json");
  module.exports.php_only.nodeTypeInfo = require("../../php_only/src/node-types.json");
} catch (_) { }
