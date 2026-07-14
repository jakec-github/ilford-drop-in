import RotaViewer from "./components/RotaViewer";
import { mockRota } from "./mockData";

function App() {
  return <RotaViewer rotaShifts={mockRota} />;
}

export default App;
