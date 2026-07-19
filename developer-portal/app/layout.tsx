import type { Metadata, Viewport } from "next";
import { headers } from "next/headers";
import type { ReactNode } from "react";

import "./globals.css";

export async function generateMetadata(): Promise<Metadata> {
  const requestHeaders = await headers();
  const host = requestHeaders.get("x-forwarded-host") ?? requestHeaders.get("host") ?? "localhost:3000";
  const protocol = requestHeaders.get("x-forwarded-proto") ?? (host.startsWith("localhost") ? "http" : "https");
  const metadataBase = new URL(`${protocol}://${host}`);

  return {
    metadataBase,
    title: {
      default: "Reasonix Developer Atlas",
      template: "%s · Reasonix Developer Atlas",
    },
    description: "面向 Reasonix 新参与者的可跳转开发地图：架构、运行时、安全边界、桌面端、扩展、生态与上手路径。",
    applicationName: "Reasonix Developer Atlas",
    keywords: ["Reasonix", "developer onboarding", "architecture", "Go", "Wails", "reasoning agent"],
    openGraph: {
      type: "website",
      locale: "zh_CN",
      title: "Reasonix Developer Atlas",
      description: "从一次用户回合，理解并接手整个项目。",
      images: [{ url: "/og.png", width: 1792, height: 917, alt: "Reasonix 开发架构抽象地图" }],
    },
    twitter: {
      card: "summary_large_image",
      title: "Reasonix Developer Atlas",
      description: "从一次用户回合，理解并接手整个项目。",
      images: ["/og.png"],
    },
    icons: { icon: "/favicon.svg" },
  };
}

export const viewport: Viewport = {
  colorScheme: "light",
  themeColor: "#f7f9ff",
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
