import type { State } from "../api/types";

/** A small copper square at 45° rotation; color tracks the connection
 * state — leaf when up, copper while connecting, ink-mute when down.
 * Identical to the dot rendered in the tray icon. */
export function StatusSquare({ state }: { state: State }): JSX.Element {
  const cls =
    state === "connected"
      ? "up"
      : state === "connecting"
        ? "connecting"
        : "down";
  return <span className={`status-square ${cls}`} />;
}
