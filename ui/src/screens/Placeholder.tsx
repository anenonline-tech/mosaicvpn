/**
 * Placeholder shown for screens that subsequent Phase 3 PRs will bring
 * online: Routing register, Pool gazetteer, Folio of preferences, Tray.
 */
export function Placeholder({
  title,
  subtitle,
}: {
  title: string;
  subtitle: string;
}): JSX.Element {
  return (
    <div className="placeholder">
      <h2>{title}</h2>
      <p>{subtitle}</p>
    </div>
  );
}
