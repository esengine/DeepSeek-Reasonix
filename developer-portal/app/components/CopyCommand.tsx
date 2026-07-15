"use client";

import { useState } from "react";

export function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    await navigator.clipboard.writeText(command);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  }

  return (
    <div className="command-box">
      <code>{command}</code>
      <button type="button" onClick={copy} aria-label={`复制命令：${command}`}>
        {copied ? "已复制" : "复制"}
      </button>
    </div>
  );
}
