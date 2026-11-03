import type { ButtonHTMLAttributes, ReactNode } from "react";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "link";

export function Button({ variant = "secondary", size = "sm", iconOnly, children, onClick, ...rest }: {
  variant?: ButtonVariant;
  size?: "sm" | "md";
  iconOnly?: boolean;
  children?: ReactNode;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      {...rest}
      onClick={onClick}
      className={rest.className ? `kbtn ${rest.className}` : "kbtn"}
      data-variant={variant}
      data-size={size}
      data-icon-only={iconOnly || undefined}
    >
      {children}
    </button>
  );
}
