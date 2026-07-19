import type { ButtonHTMLAttributes, ReactNode } from "react";

export function IconButton({
  children,
  className = "",
  neutral,
  label,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  children: ReactNode;
  neutral?: boolean;
  label: string;
}) {
  return (
    <button
      type="button"
      className={`icon-btn${neutral ? " neutral" : ""} ${className}`.trim()}
      aria-label={label}
      {...rest}
    >
      {children}
    </button>
  );
}
