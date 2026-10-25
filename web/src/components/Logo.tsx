// Logo is the Rota Wise mark: a calendar whose columns are shifts, filled to
// different levels. web/favicon.svg is the same drawing; change both together.
export default function Logo({ size = 32 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      role="img"
      aria-label="Rota Wise"
    >
      <rect x="2" y="2" width="28" height="28" rx="7" fill="#2f6f5e" />
      <rect x="2" y="2" width="28" height="9" rx="7" fill="#1f4f42" />
      <rect x="2" y="7" width="28" height="4" fill="#1f4f42" />
      <g fill="#fff">
        <rect x="6" y="15" width="4" height="11" rx="1.5" />
        <rect x="11.5" y="15" width="4" height="7" rx="1.5" opacity=".4" />
        <rect x="17" y="15" width="4" height="11" rx="1.5" />
        <rect x="22.5" y="15" width="4" height="4" rx="1.5" opacity=".4" />
      </g>
    </svg>
  );
}
