import Logo from "./Logo";
import "./SiteRoot.css";

// SiteRoot is the app outside every Organisation, when the server leaves the
// base path at the root: the splash at /, and "Page not found" anywhere else —
// including an unknown Organisation's address, which must look exactly like
// any other missing page. It fetches nothing: there is no Organisation here to
// ask about, and no session worth checking.
export default function SiteRoot({ pathname }: { pathname: string }) {
  return pathname === "/" ? <Splash /> : <NotFound />;
}

// Splash names, lists and links to no Organisation: an Organisation's address
// is the only way in (ADR 0011).
function Splash() {
  return (
    <main className="site-root">
      <Logo size={64} />
      <h1>Rota Wise</h1>
      <p>This site runs the volunteer rotas for drop-ins.</p>
      <p>
        If you volunteer or organise for one, use the link your drop-in gave
        you.
      </p>
    </main>
  );
}

function NotFound() {
  return (
    <main className="site-root">
      <Logo size={48} />
      <h1>Page not found</h1>
      <p>
        <a href="/">Go to the Rota Wise home page</a>
      </p>
    </main>
  );
}
