import type { ComponentPropsWithRef } from "react";
import "./Button.css";

// The shared button. Defaults to type="button" so it never submits a form by
// accident; pass type="submit" explicitly where that is wanted. ref is included
// because a caller sometimes has to move focus onto it — React 19 passes it
// through as an ordinary prop, so no forwardRef is needed.
type ButtonProps = ComponentPropsWithRef<"button"> & {
  size?: "small";
};

export default function Button({ size, className, ...rest }: ButtonProps) {
  const classes = ["button", size === "small" && "button--small", className]
    .filter(Boolean)
    .join(" ");

  return <button type="button" {...rest} className={classes} />;
}
