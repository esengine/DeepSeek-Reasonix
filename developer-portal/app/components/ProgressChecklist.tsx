"use client";

import { useEffect, useState } from "react";

const items = [
  "能画出 Reasonix 的五层架构与依赖方向",
  "能从入口跟踪一次完整用户回合",
  "能解释 sessionPath 与 tabId 的适用边界",
  "能区分内置 Tool、MCP、Skill、Plugin Agent 与插件包",
  "能区分 session checkpoint 与 Guard / Safe Mode 应用恢复",
  "能独立运行与改动范围匹配的验证命令",
  "完成一个边界清晰、带验证说明的小型 PR",
];

const storageKey = "reasonix-developer-atlas-progress-v2";

export function ProgressChecklist() {
  const [checked, setChecked] = useState<boolean[]>(() => items.map(() => false));
  const [ready, setReady] = useState(false);

  useEffect(() => {
    try {
      const stored = window.localStorage.getItem(storageKey);
      if (stored) {
        const parsed = JSON.parse(stored) as boolean[];
        if (Array.isArray(parsed) && parsed.length === items.length) setChecked(parsed);
      }
    } finally {
      setReady(true);
    }
  }, []);

  function toggle(index: number) {
    const next = checked.map((value, itemIndex) => (itemIndex === index ? !value : value));
    setChecked(next);
    window.localStorage.setItem(storageKey, JSON.stringify(next));
  }

  const done = checked.filter(Boolean).length;

  return (
    <section className="progress-card" aria-label="接手进度">
      <div className="progress-heading">
        <div>
          <span className="kicker">LOCAL PROGRESS</span>
          <h3>我的接手进度</h3>
        </div>
        <strong>{ready ? `${done} / ${items.length}` : "—"}</strong>
      </div>
      <div className="progress-track" aria-hidden="true">
        <span style={{ width: `${(done / items.length) * 100}%` }} />
      </div>
      <div className="progress-items">
        {items.map((item, index) => (
          <label key={item}>
            <input checked={checked[index]} onChange={() => toggle(index)} type="checkbox" />
            <span aria-hidden="true" />
            {item}
          </label>
        ))}
      </div>
      <p>进度只保存在当前浏览器，不会写入仓库或上传。</p>
    </section>
  );
}
