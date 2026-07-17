import type { ButtonHTMLAttributes } from "react";
import "./Button.css";

// The shared button. Defaults to type="button" so it never submits a form by
// accident; pass type="submit" explicitly where that is wanted.
type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  size?: "small";
};

export default function Button({ size, className, ...rest }: ButtonProps) {
  const classes = ["button", size === "small" && "button--small", className]
    .filter(Boolean)
    .join(" ");

  return <button type="button" {...rest} className={classes} />;
}
