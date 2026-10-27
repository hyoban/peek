// PEEK_DEV=1 builds Peek Dev, which installs beside Peek, with an orange
// icon, keeps its own state and never updates itself from the releases.
const dev = process.env.PEEK_DEV === "1";

export default {
  name: dev ? "Peek Dev" : "Peek",
  identifier: dev ? "cc.hyoban.peek.dev" : "cc.hyoban.peek",
  icon: dev ? "icons/dev.png" : "resources/icon.png",
  version: "0.1.0",
  copyright: "Copyright © 2026 Stephen Zhou",
  out: "build",
  macos: {
    infoPlist: {
      LSApplicationCategoryType: "public.app-category.developer-tools",
    },
  },
  updates: dev
    ? undefined
    : {
        publicKey: "pAa+54YyaPOMHDUMB27iOb95RL+l5HJWCF0YOfTL0ww=",
        github: "hyoban/peek",
      },
};
