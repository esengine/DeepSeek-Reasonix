import { useRef } from "react";

// 一条最近 n 秒的速度曲线。刻度不标：读的人要的是形状（在爬、在停、是不是
// 一顿一顿的），确切数字就在它旁边。
export function Spark({ points, w = 92, h = 18 }: { points: number[]; w?: number; h?: number }) {
  // 高度按整场见过的最高速度归一，不按窗口内的。否则窗口一滚动曲线就重新缩放，
  // 一段没有变化的数据会看着像在起伏 —— 那是图在撒谎。
  const peak = useRef(1);
  if (points.length < 2) peak.current = 1;
  peak.current = Math.max(peak.current, ...points, 1);

  if (points.length < 2) return null;
  const step = w / (points.length - 1);
  const y = (v: number) => h - 1.5 - (v / peak.current) * (h - 3);
  const line = points.map((v, i) => `${(i * step).toFixed(1)},${y(v).toFixed(1)}`).join(" ");
  const last = points[points.length - 1];
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
      <polygon className="fill" points={`0,${h} ${line} ${w},${h}`} />
      <polyline className="line" points={line} vectorEffect="non-scaling-stroke" />
      <circle className="tip" cx={w - 0.8} cy={y(last)} r={1.7} />
    </svg>
  );
}
