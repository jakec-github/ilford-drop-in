import { Link } from "wouter";
import { useSetup } from "../hooks/useSetup";
import type { AccessLevel } from "../auth-context";
import { BASE_PATH } from "../basePath";
import "./SetupNeeded.css";

// SetupNeeded says what is missing before a rota can be made — Roles, the Rota,
// or both — and sends the Organiser to Config, where both are set.
export function SetupNeeded({ lead }: { lead: string }) {
  const { roles, rota } = useSetup();
  const missing = [
    roles === false && "the Roles",
    rota === false && "the Rota",
  ].filter(Boolean);
  return (
    <div className="setup-needed">
      <p>{lead}</p>
      <p>
        First set {missing.join(" and ") || "the Roles and the Rota"} on{" "}
        <Link href="/organiser/config">Organiser &rarr; Config</Link>.
      </p>
    </div>
  );
}

// NoShifts is the rota page with nothing on it. Who is reading decides what
// they are told: a visitor who may be an Organiser is asked to log in, an
// Organiser is sent to whatever comes next, and anyone else is told to wait.
export function NoShifts({ level }: { level: AccessLevel | null }) {
  if (level === "organiser") return <NoShiftsOrganiser />;
  return (
    <div className="setup-needed">
      <h2>No shifts yet</h2>
      {level === null ? (
        <p>
          Organisers: <a href={`${BASE_PATH}/auth/login`}>log in</a> to set up
          the rota.
        </p>
      ) : (
        <p>Shifts appear here once an organiser has made the rota.</p>
      )}
    </div>
  );
}

function NoShiftsOrganiser() {
  const { done } = useSetup();
  if (done === null) return null;
  if (!done) return <SetupNeeded lead="No shifts yet." />;
  return (
    <div className="setup-needed">
      <h2>No shifts yet</h2>
      <p>
        Define the first rota on{" "}
        <Link href="/organiser/allocation">Organiser &rarr; Allocation</Link>.
      </p>
    </div>
  );
}
