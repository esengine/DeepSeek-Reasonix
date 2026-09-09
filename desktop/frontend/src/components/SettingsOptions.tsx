import { Children, cloneElement, isValidElement, type ButtonHTMLAttributes, type HTMLAttributes } from "react";

if (import.meta.env?.MODE) void import("./SettingsOptions.css");

/** Shared segmented choices; callers retain their labels, state and save handlers. */
export function SettingsOptions({
  children,
  className = "",
  layout = "content",
  onKeyDown,
  role = "group",
  ...props
}: HTMLAttributes<HTMLDivElement> & { layout?: "content" | "field" | "fill" }) {
  return (
    <div
      {...props}
      role={role}
      className={`settings-options settings-options--${layout} ${className}`}
      onKeyDown={(event) => {
        onKeyDown?.(event);
        if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey) return;
        const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>(":scope > button:not(:disabled)"));
        const index = buttons.indexOf(event.target as HTMLButtonElement);
        if (index < 0 || !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
        event.preventDefault();
        const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1
          : (index + (event.key === "ArrowRight" ? 1 : -1) + buttons.length) % buttons.length;
        buttons[next].focus();
        buttons[next].click();
      }}
    >
      {Children.map(children, (child) => {
        if (!isValidElement<ButtonHTMLAttributes<HTMLButtonElement>>(child) || child.type !== "button") return child;
        const selected = child.props["aria-checked"] ?? child.props["aria-selected"] ?? child.props["aria-pressed"]
          ?? /(?:set-seg__btn--on|provider-add-segmented__item--active)/.test(child.props.className ?? "");
        return cloneElement(child, {
          type: child.props.type ?? "button",
          title: child.props.title ?? (typeof child.props.children === "string" ? child.props.children : undefined),
          "aria-pressed": child.props.role === "radio" || child.props.role === "tab" ? undefined : selected,
          "data-selected": selected ? "true" : "false",
        } as ButtonHTMLAttributes<HTMLButtonElement>);
      })}
    </div>
  );
}
