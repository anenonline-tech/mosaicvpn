import type { Status } from "../api/types";

/** Atlas-style binding rules (top + bottom) and corner marginalia. */
export function Marginalia({
  status,
  plate,
  plateSubtitle,
}: {
  status: Status | null;
  plate: string;
  plateSubtitle: string;
}): JSX.Element {
  const issue = new Date().toLocaleDateString("en-GB", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
  return (
    <>
      <div className="binding-rule top" />
      <div className="binding-rule top thin" />
      <div className="binding-rule bot" />
      <div className="binding-rule bot thin" />

      <div className="marg tl">
        <b>Mosaic</b> · An atlas of secure routes
      </div>
      <div className="marg tr">Vol. I · {issue}</div>
      <div className="marg bl">
        Daemon{" "}
        {status?.state === "connected"
          ? "active"
          : status?.state === "connecting"
            ? "connecting"
            : "idle"}
        {status?.daemon_pid ? ` · pid ${status.daemon_pid}` : null}
      </div>
      <div className="marg br">
        Plate {plate} · {plateSubtitle}
      </div>
    </>
  );
}
