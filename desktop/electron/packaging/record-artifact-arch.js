"use strict";
const { Arch } = require("electron-builder");
const { record } = require("./arch");

// artifactBuildCompleted: the one place the architecture is a value rather than
// a substring of a filename.
module.exports = async function recordArtifactArch(artifact) {
  if (artifact.arch !== undefined && artifact.arch !== null) {
    record(artifact.file, Arch[artifact.arch]);
  }
};
