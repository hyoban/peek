// bumpp raises the version, commits "chore: release v…", tags and pushes
// it, which the release workflow builds and publishes. With no
// package.json, the version is mygo.config.ts's, read from it here.
import mygo from "./mygo.config";

export default {
  currentVersion: mygo.version,
  files: ["mygo.config.ts"],
  commit: "chore: release {tag}",
};
