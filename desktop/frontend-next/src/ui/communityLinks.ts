const REPO = "https://github.com/esengine/DeepSeek-Reasonix";

export const COMMUNITY = {
  qqName: "DeepSeek-Reasonix官方群",
  qqNumber: "1093562660",
  qqJoin: "https://qm.qq.com/q/i59b0z2R8s",
  douyinName: "做游戏的小鱼",
  douyinId: "22703872788",
  author: "esengine",
  authorUrl: "https://github.com/esengine",
  discord: "https://discord.gg/XF78rEME2D",
  paypal: "https://paypal.me/yuhuahui",
  issues: `${REPO}/issues`,
  contributors: `${REPO}/graphs/contributors`,
} as const;

// GitHub's own rule: alphanumerics and single hyphens, no hyphen at either end, 39 at most.
const LOGIN = /^[A-Za-z0-9](?:[A-Za-z0-9]|-(?=[A-Za-z0-9])){0,38}$/;

export function profileUrl(login: string): string | null {
  return LOGIN.test(login) ? `https://github.com/${login}` : null;
}
