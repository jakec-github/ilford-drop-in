import { useEffect, useState } from "react";
import RotaViewer from "./components/RotaViewer";
import { fetchRota } from "./api";
import type { RotaShift } from "./types";

function App() {
  const [rota, setRota] = useState<RotaShift[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchRota()
      .then(setRota)
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "Failed to load rota");
      });
  }, []);

  if (error) return <p className="app-status">Could not load the rota: {error}</p>;
  if (rota === null) return <p className="app-status">Loading rota…</p>;

  return <RotaViewer rotaShifts={rota} />;
}

export default App;
