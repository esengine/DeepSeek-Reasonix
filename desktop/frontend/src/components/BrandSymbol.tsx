import { useEffect, useId, useRef } from "react";

// BrandSymbol 内联渲染 Reasonix 图标（logo-symbol.svg 的图形），彩虹渐变 +
// 流动动画，与 BrandLogo 效果一致。不能用 <img> 加载 SVG（渐变引用与动画
// 不可靠），内联 DOM 后由 rAF 直接驱动 gradientTransform。
const FLOW_DURATION_MS = 2800;
const FLOW_DISTANCE = 235; // 与 viewBox 宽一致：平移一个周期无缝循环

export function BrandSymbol({ className, alt = "Reasonix" }: { className?: string; alt?: string }) {
  const gradId = "symbol-" + useId().replace(/[^a-zA-Z0-9]/g, "");
  const fill = `url(#${gradId})`;
  const gradRef = useRef<SVGLinearGradientElement | null>(null);

  useEffect(() => {
    const grad = gradRef.current;
    if (!grad) return;
    let raf = 0;
    const step = (t: number) => {
      const phase = (t % FLOW_DURATION_MS) / FLOW_DURATION_MS;
      const x = phase * FLOW_DISTANCE;
      grad.setAttribute("gradientTransform", `translate(-${x} 0)`);
      raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, []);

  return (
    <svg viewBox="245 70 235 245" className={className} role="img" aria-label={alt}>
      <defs>
        <linearGradient ref={gradRef} id={gradId} x1="0" y1="0" x2="470" y2="0" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#ff2f70" />
          <stop offset="14.2857%" stopColor="#ff9f1c" />
          <stop offset="28.5714%" stopColor="#f7f24a" />
          <stop offset="42.8571%" stopColor="#25f36b" />
          <stop offset="57.1429%" stopColor="#18d9ff" />
          <stop offset="71.4286%" stopColor="#7a5cff" />
          <stop offset="85.7143%" stopColor="#ff4fe3" />
          <stop offset="100%" stopColor="#ff2f70" />
        </linearGradient>
      </defs>
      <g>
        <path fill={fill} d="M369.75,185.3c2.09,1.76,4.98,2.41,7.95,1.06,1.18-.54,1.96-1.91,1.47-3.25-3.92-10.71-14.97-18.54-19.38-12.48-.54.74-.52,1.7-.11,2.29.32.47.97.57,1.82.4,2.68-.56,6.02,1.31,5.7,4.3-.33,3.12.3,5.79,2.56,7.69Z" />
        <path fill={fill} d="M434.28,204.78c10.12-11.32,16.46-25.01,18.67-40.07,5.04-32.93-11.31-63.01-42.11-75.75-10.83-4.35-21.95-6.2-33.76-6.09l-114.5.03v219.42s56.29-.04,56.29-.04v-77.17c-1.86-.69-3.15-1.27-3.15-1.27-12.24-5.59-21.6-15.29-26.74-27.44-6.56-15.51-4.54-32.9,5.78-46.12,10.28-12.84,24.31-16.92,40.41-14.67,5.95.83,12.3-6.15,24.4-4.81.81.09,1.63.7,1.71,1.18.28,1.65-4.4,2.54-4.4,7.05,0,1.9.85,4.1,2.65,5.36,6.17,4.33,11.51,9.19,17.1,14.22,2.72,2.44,11.93,9.31,14.27,3.72,1.37-3.26,2.27-6.68,3.18-10.15.45-1.72-.5-2.74-1.89-3.56-9.5-5.63-13.08-17.35-9.37-27.74.36-1,1.29-1.49,2.08-1.52,3.19-.14,1.49,5.78,9.38,8.21,7.49,2.31,7.71,8.53,11.35,6.05,8.48-5.78,11.54-1.05,19.41-8.46.81-.76,2.2-.82,3.04-.23.52.36.97,1.29.93,2.38-.23,6.26-2.67,12.24-6.95,16.84-8.09,8.69-14.89,4.53-15.33,10.62-1.29,17.86-7.13,35.69-19.81,48.66-.41.42-.65,1.01-.58,1.41.07.42.57.83,1.12,1.01l10.68,3.63c1.51.51,2.6,1.87,2.41,3.31-.17,1.26-1.17,2.37-2.61,2.85-7.84,2.59-16.26,2.14-24.24-.27-10.56,8.89-23.54,13.48-37.68,12.93l20.53,28.34,33.49,45.73,66.85-.09-56.68-76.53c13.19-3.89,24.78-10.87,34.07-20.94Z" />
        <circle fill={fill} cx="362.14" cy="177.19" r="1.58" />
        <path fill={fill} d="M300.31,167.85c-3.53.43-5.65,2.96-5.31,6.74,2.39,26.35,26.7,47.92,53.94,42.72,5.8-1.11,11.08-3.41,15.36-7.74-9.27-7.98-13.79-16.12-21.41-24.13-10.91-11.47-26.43-19.54-42.58-17.59Z" />
      </g>
    </svg>
  );
}
