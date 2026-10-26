import type { ReactNode } from "react";
import "./Loading.css";

// Loading is a wait, said in words with the Rota Wise mark moving beside them:
// the logo's four columns fill and drain in a wave, each brightening as it
// fills, so at any moment it is still a rota of shifts filled to different
// levels. The columns hang from one line, as the logo's do, so the spinner and
// the mark are the same drawing — Logo.tsx and web/favicon.svg are the still
// version; change the three together.
//
// The words are the signal and the mark is company for them: they sit in a
// status region, and a reader who has asked for less movement gets the logo's
// own pose with only its brightness travelling.
//
// Two forms, for two different waits:
//
// - The default is for a screen with nothing on it yet — the first load of a
//   page. The whole mark, centred, and only after a moment, so a load that is
//   over at once does not flash it.
// - `inline` is for a wait beside content that is already showing, such as a
//   draft re-solving. The columns alone, in the colour of the words beside
//   them. It never stands in for that content: what is on screen stays where it
//   is, and the page does not jump.
export default function Loading({
  children,
  inline = false,
}: {
  children: ReactNode;
  inline?: boolean;
}) {
  if (inline) {
    return (
      <span className="loading loading--inline" role="status">
        <svg width="16" height="9" viewBox="5 14 23 13" aria-hidden="true">
          <Columns fill="currentColor" />
        </svg>
        {children}
      </span>
    );
  }

  return (
    <div className="loading" role="status">
      <svg width="56" height="56" viewBox="0 0 32 32" aria-hidden="true">
        <rect x="2" y="2" width="28" height="28" rx="7" fill="#2f6f5e" />
        <rect x="2" y="2" width="28" height="9" rx="7" fill="#1f4f42" />
        <rect x="2" y="7" width="28" height="4" fill="#1f4f42" />
        <Columns fill="#fff" />
      </svg>
      <p>{children}</p>
    </div>
  );
}

// The logo's four columns, each drawn at full height; Loading.css scales them.
function Columns({ fill }: { fill: string }) {
  return (
    <g fill={fill}>
      {[6, 11.5, 17, 22.5].map((x) => (
        <rect
          key={x}
          className="loading-column"
          x={x}
          y="15"
          width="4"
          height="11"
          rx="1.5"
        />
      ))}
    </g>
  );
}
