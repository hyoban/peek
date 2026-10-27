// bumpp raises the version, commits "chore: release v…", tags and pushes
// it, which the release workflow builds and publishes. With no
// package.json, the version is mygo.config.ts's, read from it here. The
// commits since the last tag pick the version: a breaking change raises
// the major, a feat the minor, anything else the patch.
import mygo from "./mygo.config";

export default {
  currentVersion: mygo.version,
  files: ["mygo.config.ts"],
  release: "conventional",
  commit: "chore: release {tag}",
};
