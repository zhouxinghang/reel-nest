import { useId } from "react";

/** Small local vector marks: no third-party image requests on a private library. */
export function PlayerLogo({
  name,
}: {
  name: "pot" | "vlc" | "iina" | "copy";
}) {
  const gradient = useId();
  return (
    <svg
      className="player-logo"
      viewBox="0 0 40 40"
      aria-hidden="true"
      focusable="false"
    >
      {name === "pot" && (
        <>
          <circle cx="20" cy="20" r="19" fill="#ffe900" />
          <path d="m15 10 16 10-16 10Z" fill="white" />
        </>
      )}
      {name === "vlc" && (
        <>
          <path d="M16 4Q20 0 24 4L33 31H7Z" fill="#ff9400" />
          <path d="m14 11 12 0 2 6H12Zm-4 12h20l2 6H8Z" fill="#fff" />
          <path d="M7 30h26l5 8H2Z" fill="#ff9400" />
          <path d="M11 30h18l3 3H8Z" fill="#ffc456" />
        </>
      )}
      {name === "iina" && (
        <>
          <defs>
            <linearGradient id={gradient} x2="1" y2="1">
              <stop stopColor="#367bff" />
              <stop offset="1" stopColor="#0bdfe9" />
            </linearGradient>
          </defs>
          <rect width="40" height="40" rx="9" fill="#27272b" />
          <rect x="5" y="16" width="4" height="9" rx="2" fill="#6246f5" />
          <rect x="11" y="12" width="5" height="17" rx="2.5" fill="#465aff" />
          <path
            d="M18 11q0-3 3-1l13 8q3 2 0 4l-13 8q-3 2-3-1Z"
            fill={`url(#${gradient})`}
          />
        </>
      )}
      {name === "copy" && (
        <>
          <rect x="13" y="3" width="23" height="30" rx="4" fill="#768ac8" />
          <rect x="4" y="10" width="25" height="30" rx="4" fill="#d1dcff" />
          <path
            d="M12 24h9m-4.5-4.5v9"
            stroke="white"
            strokeWidth="3"
            strokeLinecap="round"
          />
        </>
      )}
    </svg>
  );
}
