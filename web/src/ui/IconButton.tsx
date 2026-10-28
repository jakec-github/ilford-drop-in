import type { ComponentPropsWithRef } from "react";
import "./IconButton.css";

// The shapes an IconButton can draw. Pencil is every "edit this"; plus is every
// "make a new one" — one glyph each across the app, so an Organiser learns them
// once.
const ICONS = {
  pencil: (
    <path d="M13.5 3.5l3 3L7 16H4v-3z M11.5 5.5l3 3" strokeLinejoin="round" />
  ),
  plus: <path d="M10 4v12M4 10h12" />,
} as const;

export type IconName = keyof typeof ICONS;

// IconButton is a button that is only a glyph. It has no words of its own, so
// aria-label is required rather than optional: it is the button's name, and
// title repeats it for a pointer that hovers.
//
// "filled" is the one prominent action of a section; "plain" sits inside a row
// beside the thing it acts on and should not compete with it.
type IconButtonProps = Omit<ComponentPropsWithRef<"button">, "children"> & {
  icon: IconName;
  "aria-label": string;
  variant?: "filled" | "plain";
};

export default function IconButton({
  icon,
  variant = "plain",
  className,
  title,
  ...rest
}: IconButtonProps) {
  const classes = ["icon-button", `icon-button--${variant}`, className]
    .filter(Boolean)
    .join(" ");
  return (
    <button
      type="button"
      title={title ?? rest["aria-label"]}
      {...rest}
      className={classes}
    >
      <svg viewBox="0 0 20 20" aria-hidden="true">
        {ICONS[icon]}
      </svg>
    </button>
  );
}
